package main

import (
	"strings"
	"testing"
	"time"

	thingscloud "github.com/arthursoares/things-cloud-sdk"
)

// Valid Base58 UUIDs for tests (format-check passes; existence varies by fake).
const (
	testTaskUUID    = "AbCdEfGhJkMnPqRsTuVwXy"
	testProjectUUID = "BbCdEfGhJkMnPqRsTuVwXy"
	testAreaUUID    = "CbCdEfGhJkMnPqRsTuVwXy"
	testTagUUID     = "DbCdEfGhJkMnPqRsTuVwXy"
	testItemUUID    = "EbCdEfGhJkMnPqRsTuVwXy"
	missingUUID     = "ZbCdEfGhJkMnPqRsTuVwXy"
)

type fakeStore struct {
	tasks map[string]*thingscloud.Task
	areas map[string]*thingscloud.Area
	tags  map[string]*thingscloud.Tag
	items map[string]*thingscloud.CheckListItem
}

func (f fakeStore) Task(uuid string) (*thingscloud.Task, error)               { return f.tasks[uuid], nil }
func (f fakeStore) Area(uuid string) (*thingscloud.Area, error)               { return f.areas[uuid], nil }
func (f fakeStore) Tag(uuid string) (*thingscloud.Tag, error)                 { return f.tags[uuid], nil }
func (f fakeStore) ChecklistItem(uuid string) (*thingscloud.CheckListItem, error) {
	return f.items[uuid], nil
}

func populatedStore() fakeStore {
	return fakeStore{
		tasks: map[string]*thingscloud.Task{
			testTaskUUID:    {UUID: testTaskUUID, Type: thingscloud.TaskTypeTask, Schedule: thingscloud.TaskScheduleAnytime},
			testProjectUUID: {UUID: testProjectUUID, Type: thingscloud.TaskTypeProject},
		},
		areas: map[string]*thingscloud.Area{testAreaUUID: {UUID: testAreaUUID}},
		tags:  map[string]*thingscloud.Tag{testTagUUID: {UUID: testTagUUID}},
		items: map[string]*thingscloud.CheckListItem{testItemUUID: {UUID: testItemUUID}},
	}
}

func withFakeStore(t *testing.T, store fakeStore) {
	t.Helper()
	orig := validationState
	validationState = func() entityStore { return store }
	t.Cleanup(func() { validationState = orig })
}

func expectNotFound(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error for nonexistent target, got success")
	}
	if !isInvalidInput(err) {
		t.Fatalf("expected invalid-input error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' in error, got: %v", err)
	}
}

func TestWriteOpsRejectUnknownTargets(t *testing.T) {
	withFakeStore(t, populatedStore())

	t.Run("complete unknown task", func(t *testing.T) { expectNotFound(t, completeTask(missingUUID)) })
	t.Run("uncomplete unknown task", func(t *testing.T) { expectNotFound(t, uncompleteTask(missingUUID)) })
	t.Run("trash unknown task", func(t *testing.T) { expectNotFound(t, trashTask(missingUUID)) })
	t.Run("untrash unknown task", func(t *testing.T) { expectNotFound(t, untrashTask(missingUUID)) })
	t.Run("move unknown task to today", func(t *testing.T) { expectNotFound(t, moveTaskToToday(missingUUID, "")) })
	t.Run("move unknown task to anytime", func(t *testing.T) { expectNotFound(t, moveTaskToAnytime(missingUUID)) })
	t.Run("move unknown task to someday", func(t *testing.T) { expectNotFound(t, moveTaskToSomeday(missingUUID)) })
	t.Run("move unknown task to inbox", func(t *testing.T) { expectNotFound(t, moveTaskToInbox(missingUUID)) })
	t.Run("edit unknown task", func(t *testing.T) {
		expectNotFound(t, editTask(EditTaskRequest{UUID: missingUUID, Title: "x"}))
	})
	t.Run("edit with unknown project", func(t *testing.T) {
		expectNotFound(t, editTask(EditTaskRequest{UUID: testTaskUUID, Project: missingUUID}))
	})
	t.Run("edit with unknown area", func(t *testing.T) {
		expectNotFound(t, editTask(EditTaskRequest{UUID: testTaskUUID, Area: missingUUID}))
	})
	t.Run("edit with unknown tag", func(t *testing.T) {
		expectNotFound(t, editTask(EditTaskRequest{UUID: testTaskUUID, Tags: missingUUID}))
	})
	t.Run("create task in unknown project", func(t *testing.T) {
		_, err := createTask(CreateTaskRequest{Title: "x", Project: missingUUID})
		expectNotFound(t, err)
	})
	t.Run("create task under unknown parent", func(t *testing.T) {
		_, err := createTask(CreateTaskRequest{Title: "x", ParentTask: missingUUID})
		expectNotFound(t, err)
	})
	t.Run("create task with unknown tag", func(t *testing.T) {
		_, err := createTask(CreateTaskRequest{Title: "x", Tags: missingUUID})
		expectNotFound(t, err)
	})
	t.Run("create area with unknown tag", func(t *testing.T) {
		_, err := createArea("x", []string{missingUUID})
		expectNotFound(t, err)
	})
	t.Run("create tag with unknown parent", func(t *testing.T) {
		_, err := createTag("x", "", missingUUID)
		expectNotFound(t, err)
	})
	t.Run("create heading in unknown project", func(t *testing.T) {
		_, err := createHeading("x", missingUUID)
		expectNotFound(t, err)
	})
	t.Run("create project in unknown area", func(t *testing.T) {
		_, err := createProject("x", "", "", "", missingUUID, "")
		expectNotFound(t, err)
	})
	t.Run("create checklist item on unknown task", func(t *testing.T) {
		_, err := createChecklistItem("x", missingUUID)
		expectNotFound(t, err)
	})
	t.Run("complete unknown checklist item", func(t *testing.T) {
		expectNotFound(t, completeChecklistItem(missingUUID))
	})
	t.Run("uncomplete unknown checklist item", func(t *testing.T) {
		expectNotFound(t, uncompleteChecklistItem(missingUUID))
	})
	t.Run("delete unknown checklist item", func(t *testing.T) {
		expectNotFound(t, deleteChecklistItem(missingUUID))
	})
}

func TestWriteOpsRejectWrongEntityType(t *testing.T) {
	withFakeStore(t, populatedStore())

	t.Run("plain task used as project", func(t *testing.T) {
		_, err := createTask(CreateTaskRequest{Title: "x", Project: testTaskUUID})
		if err == nil || !strings.Contains(err.Error(), "not a project") {
			t.Fatalf("expected 'not a project' error, got: %v", err)
		}
	})
	t.Run("checklist item on a project", func(t *testing.T) {
		_, err := createChecklistItem("x", testProjectUUID)
		if err == nil || !strings.Contains(err.Error(), "checklist items can only be added to tasks") {
			t.Fatalf("expected checklist parent-type error, got: %v", err)
		}
	})
}

func TestBuildEditUpdateSchedulePreservation(t *testing.T) {
	scheduled := &thingscloud.Task{UUID: testTaskUUID, Schedule: thingscloud.TaskScheduleSomeday}
	inbox := &thingscloud.Task{UUID: testTaskUUID, Schedule: thingscloud.TaskScheduleInbox}

	t.Run("project move keeps existing schedule", func(t *testing.T) {
		fields, err := buildEditUpdate(EditTaskRequest{UUID: testTaskUUID, Project: testProjectUUID}, scheduled, time.UTC)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := fields["st"]; ok {
			t.Fatalf("project move must not touch schedule, but update sets st: %v", fields)
		}
		if _, ok := fields["sr"]; ok {
			t.Fatalf("project move must not touch scheduled date, but update sets sr: %v", fields)
		}
	})

	t.Run("project move out of inbox forces anytime", func(t *testing.T) {
		fields, err := buildEditUpdate(EditTaskRequest{UUID: testTaskUUID, Project: testProjectUUID}, inbox, time.UTC)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if st, ok := fields["st"]; !ok || st != 1 {
			t.Fatalf("inbox task moved to project should get st=1, got: %v", fields)
		}
	})

	t.Run("explicit when wins over preservation", func(t *testing.T) {
		fields, err := buildEditUpdate(EditTaskRequest{UUID: testTaskUUID, Project: testProjectUUID, When: "someday"}, scheduled, time.UTC)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if st, ok := fields["st"]; !ok || st != 2 {
			t.Fatalf("explicit when:someday should set st=2, got: %v", fields)
		}
	})

	t.Run("project none clears project", func(t *testing.T) {
		fields, err := buildEditUpdate(EditTaskRequest{UUID: testTaskUUID, Project: "none"}, scheduled, time.UTC)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		pr, ok := fields["pr"].([]string)
		if !ok || len(pr) != 0 {
			t.Fatalf("project 'none' should clear pr, got: %v", fields["pr"])
		}
		if _, ok := fields["st"]; ok {
			t.Fatalf("clearing project must not touch schedule, got: %v", fields)
		}
	})

	t.Run("repeat on inbox task without when forces anytime", func(t *testing.T) {
		fields, err := buildEditUpdate(EditTaskRequest{UUID: testTaskUUID, Repeat: "daily"}, inbox, time.UTC)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if st, ok := fields["st"]; !ok || st != 1 {
			t.Fatalf("repeat on inbox task should force st=1, got: %v", fields)
		}
	})

	t.Run("repeat on scheduled task keeps schedule", func(t *testing.T) {
		fields, err := buildEditUpdate(EditTaskRequest{UUID: testTaskUUID, Repeat: "daily"}, scheduled, time.UTC)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := fields["st"]; ok {
			t.Fatalf("repeat on non-inbox task must not touch schedule, got: %v", fields)
		}
	})
}

func TestSyncThrottle(t *testing.T) {
	origDoSync := doSync
	t.Cleanup(func() {
		doSync = origDoSync
		syncThrottleMu.Lock()
		lastSyncAt = time.Time{}
		syncThrottleMu.Unlock()
	})

	calls := 0
	doSync = func() error { calls++; return nil }
	syncThrottleMu.Lock()
	lastSyncAt = time.Time{}
	syncThrottleMu.Unlock()

	t.Setenv("SYNC_MIN_INTERVAL", "60")

	for i := 0; i < 3; i++ {
		if err := syncForRead(); err != nil {
			t.Fatalf("syncForRead failed: %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("expected 1 sync for 3 throttled reads, got %d", calls)
	}

	syncAfterWrite()
	if calls != 2 {
		t.Fatalf("syncAfterWrite must bypass the throttle, got %d calls", calls)
	}

	t.Setenv("SYNC_MIN_INTERVAL", "0")
	if err := syncForRead(); err != nil {
		t.Fatalf("syncForRead failed: %v", err)
	}
	if calls != 3 {
		t.Fatalf("interval 0 should sync on every read, got %d calls", calls)
	}
}

func TestSyncThrottleRetriesAfterFailure(t *testing.T) {
	origDoSync := doSync
	t.Cleanup(func() {
		doSync = origDoSync
		syncThrottleMu.Lock()
		lastSyncAt = time.Time{}
		syncThrottleMu.Unlock()
	})

	calls := 0
	doSync = func() error {
		calls++
		return errTestSyncFailed
	}
	syncThrottleMu.Lock()
	lastSyncAt = time.Time{}
	syncThrottleMu.Unlock()

	t.Setenv("SYNC_MIN_INTERVAL", "60")

	if err := syncForRead(); err == nil {
		t.Fatal("expected sync error to propagate")
	}
	// A failed sync must not start the throttle window.
	if err := syncForRead(); err == nil {
		t.Fatal("expected second sync attempt (and error), got throttled success")
	}
	if calls != 2 {
		t.Fatalf("failed syncs should not be throttled, got %d calls", calls)
	}
}

var errTestSyncFailed = &thingscloud.HTTPStatusError{StatusCode: 500, Status: "500 boom"}

// withCapturedWrites stubs out the network so write ops succeed locally and
// their envelopes can be inspected.
func withCapturedWrites(t *testing.T) *[]writeEnvelope {
	t.Helper()
	origWrite, origSync := writeToHistory, doSync
	var envs []writeEnvelope
	writeToHistory = func(env writeEnvelope) error {
		envs = append(envs, env)
		return nil
	}
	doSync = func() error { return nil }
	t.Cleanup(func() { writeToHistory, doSync = origWrite, origSync })
	return &envs
}

func lastEnvelope(t *testing.T, envs *[]writeEnvelope) writeEnvelope {
	t.Helper()
	if len(*envs) == 0 {
		t.Fatal("no write envelope captured")
	}
	return (*envs)[len(*envs)-1]
}

func TestCreateOpsTreatNoneAsUnset(t *testing.T) {
	withFakeStore(t, populatedStore())
	envs := withCapturedWrites(t)

	t.Run("tag parent none", func(t *testing.T) {
		if _, err := createTag("t", "", "none"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		payload := lastEnvelope(t, envs).payload.(map[string]any)
		if pn := payload["pn"].([]string); len(pn) != 0 {
			t.Fatalf("parent 'none' must not be written as a reference, got pn=%v", pn)
		}
	})

	t.Run("heading project none", func(t *testing.T) {
		if _, err := createHeading("h", "none"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		payload := lastEnvelope(t, envs).payload.(taskCreatePayload)
		if len(payload.Pr) != 0 {
			t.Fatalf("project 'none' must not be written as a reference, got pr=%v", payload.Pr)
		}
	})

	t.Run("project area none", func(t *testing.T) {
		if _, err := createProject("p", "", "", "", "none", ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		payload := lastEnvelope(t, envs).payload.(taskCreatePayload)
		if len(payload.Ar) != 0 {
			t.Fatalf("area 'none' must not be written as a reference, got ar=%v", payload.Ar)
		}
	})

	t.Run("task project none", func(t *testing.T) {
		if _, err := createTask(CreateTaskRequest{Title: "x", Project: "none"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		payload := lastEnvelope(t, envs).payload.(taskCreatePayload)
		if len(payload.Pr) != 0 {
			t.Fatalf("project 'none' must not be written as a reference, got pr=%v", payload.Pr)
		}
	})

	t.Run("task parent none", func(t *testing.T) {
		if _, err := createTask(CreateTaskRequest{Title: "x", ParentTask: "none"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		payload := lastEnvelope(t, envs).payload.(taskCreatePayload)
		if len(payload.Pr) != 0 {
			t.Fatalf("parent_task 'none' must not be written as a reference, got pr=%v", payload.Pr)
		}
	})
}

func TestCompleteTaskWritesUpdateEnvelope(t *testing.T) {
	withFakeStore(t, populatedStore())
	envs := withCapturedWrites(t)

	if err := completeTask(testTaskUUID); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	env := lastEnvelope(t, envs)
	if env.id != testTaskUUID || env.action != 1 || env.kind != "Task6" {
		t.Fatalf("unexpected envelope: id=%s action=%d kind=%s", env.id, env.action, env.kind)
	}
	fields := env.payload.(map[string]any)
	if fields["ss"] != 3 {
		t.Fatalf("complete should set ss=3, got %v", fields["ss"])
	}
}

func TestGenerateUUIDCanonicalLength(t *testing.T) {
	t.Parallel()
	for i := 0; i < 1000; i++ {
		id := generateUUID()
		if len(id) != 22 {
			t.Fatalf("generateUUID must emit exactly 22 chars, got %d: %q", len(id), id)
		}
		if !isBase58UUID(id) {
			t.Fatalf("generateUUID emitted an id our own validator rejects: %q", id)
		}
		if id[0] == '1' {
			t.Fatalf("generateUUID must never emit a leading-'1' (zero-byte) id — the app's Base58 decoder overflows on them: %q", id)
		}
	}
}

func TestBase58UUIDRejectsAppIndecodableLengths(t *testing.T) {
	t.Parallel()
	// The Things app's Base58 decoder hard-crashes on identifiers longer than
	// 22 chars; the validator must never let them through.
	if isBase58UUID(strings.Repeat("A", 23)) {
		t.Fatal("23-char identifier must be rejected")
	}
	if isBase58UUID(strings.Repeat("A", 32)) {
		t.Fatal("32-char identifier must be rejected")
	}
	if !isBase58UUID(strings.Repeat("A", 22)) {
		t.Fatal("22-char identifier must be accepted")
	}
}

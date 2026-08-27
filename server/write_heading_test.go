package main

import (
	"testing"

	thingscloud "github.com/arthursoares/things-cloud-sdk"
)

const testHeadingUUID = "GbCdEfGhJkMnPqRsTuVwXy"

// storeWithHeading returns the standard fixture store plus a heading.
func storeWithHeading() fakeStore {
	store := populatedStore()
	store.tasks[testHeadingUUID] = &thingscloud.Task{
		UUID:          testHeadingUUID,
		Type:          thingscloud.TaskTypeHeading,
		ParentTaskIDs: []string{testProjectUUID},
	}
	return store
}

func TestCreateTaskHeading(t *testing.T) {
	withFakeStore(t, storeWithHeading())
	envs := withCapturedWrites(t)

	t.Run("sets agr and leaves inbox", func(t *testing.T) {
		if _, err := createTask(CreateTaskRequest{Title: "x", Heading: testHeadingUUID}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		payload := lastEnvelope(t, envs).payload.(taskCreatePayload)
		if len(payload.Agr) != 1 || payload.Agr[0] != testHeadingUUID {
			t.Fatalf("Agr = %v, want [%s]", payload.Agr, testHeadingUUID)
		}
		if payload.St != 1 {
			t.Fatalf("St = %d, want 1 (headed tasks are structural, not inbox)", payload.St)
		}
	})

	t.Run("explicit when wins over structural default", func(t *testing.T) {
		if _, err := createTask(CreateTaskRequest{Title: "x", Heading: testHeadingUUID, When: "someday"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		payload := lastEnvelope(t, envs).payload.(taskCreatePayload)
		if payload.St != 2 {
			t.Fatalf("St = %d, want 2 (explicit someday)", payload.St)
		}
	})

	t.Run("non-heading target rejected", func(t *testing.T) {
		if _, err := createTask(CreateTaskRequest{Title: "x", Heading: testTaskUUID}); err == nil || !isInvalidInput(err) {
			t.Fatalf("expected invalid-input error, got %v", err)
		}
	})

	t.Run("unknown heading rejected", func(t *testing.T) {
		expectNotFound(t, func() error {
			_, err := createTask(CreateTaskRequest{Title: "x", Heading: missingUUID})
			return err
		}())
	})

	t.Run("none treated as unset", func(t *testing.T) {
		if _, err := createTask(CreateTaskRequest{Title: "x", Heading: "none"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		payload := lastEnvelope(t, envs).payload.(taskCreatePayload)
		if len(payload.Agr) != 0 {
			t.Fatalf("Agr = %v, want empty", payload.Agr)
		}
	})
}

func TestEditTaskHeading(t *testing.T) {
	withFakeStore(t, storeWithHeading())
	envs := withCapturedWrites(t)

	t.Run("set writes agr", func(t *testing.T) {
		if err := editTask(EditTaskRequest{UUID: testTaskUUID, Heading: testHeadingUUID}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		fields := lastEnvelope(t, envs).payload.(map[string]any)
		agr, ok := fields["agr"].([]string)
		if !ok || len(agr) != 1 || agr[0] != testHeadingUUID {
			t.Fatalf("agr = %v, want [%s]", fields["agr"], testHeadingUUID)
		}
	})

	t.Run("none clears agr", func(t *testing.T) {
		if err := editTask(EditTaskRequest{UUID: testTaskUUID, Heading: "none"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		fields := lastEnvelope(t, envs).payload.(map[string]any)
		agr, ok := fields["agr"].([]string)
		if !ok || len(agr) != 0 {
			t.Fatalf("agr = %v, want []", fields["agr"])
		}
	})

	t.Run("inbox task gets bumped to anytime", func(t *testing.T) {
		store := storeWithHeading()
		store.tasks[testTaskUUID] = &thingscloud.Task{
			UUID:     testTaskUUID,
			Type:     thingscloud.TaskTypeTask,
			Schedule: thingscloud.TaskScheduleInbox,
		}
		withFakeStore(t, store)
		if err := editTask(EditTaskRequest{UUID: testTaskUUID, Heading: testHeadingUUID}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		fields := lastEnvelope(t, envs).payload.(map[string]any)
		if st, ok := fields["st"]; !ok || st != 1 {
			t.Fatalf("st = %v (present=%v), want 1", st, ok)
		}
	})

	t.Run("non-heading target rejected", func(t *testing.T) {
		withFakeStore(t, storeWithHeading())
		err := editTask(EditTaskRequest{UUID: testTaskUUID, Heading: testProjectUUID})
		if err == nil || !isInvalidInput(err) {
			t.Fatalf("expected invalid-input error, got %v", err)
		}
	})
}

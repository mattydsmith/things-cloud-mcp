package main

import (
	"testing"
	"time"

	thingscloud "github.com/arthursoares/things-cloud-sdk"
)

const testScheduledTaskUUID = "FbCdEfGhJkMnPqRsTuVwXy"

// storeWithScheduledTask returns the standard fixture store plus one task
// that already carries a scheduled date (required to attach reminders).
func storeWithScheduledTask() fakeStore {
	store := populatedStore()
	sr := time.Now().UTC().Add(24 * time.Hour)
	store.tasks[testScheduledTaskUUID] = &thingscloud.Task{
		UUID:          testScheduledTaskUUID,
		Type:          thingscloud.TaskTypeTask,
		Schedule:      thingscloud.TaskScheduleSomeday,
		ScheduledDate: &sr,
	}
	return store
}

func TestParseReminder(t *testing.T) {
	valid := map[string]int{
		"00:00": 0,
		"09:05": 9*3600 + 5*60,
		"17:00": 61200,
		"23:59": 23*3600 + 59*60,
	}
	for in, want := range valid {
		got, err := parseReminder(in)
		if err != nil {
			t.Errorf("parseReminder(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseReminder(%q) = %d, want %d", in, got, want)
		}
	}

	for _, in := range []string{"25:00", "12:60", "9am", "17", "17:0", "-1:00", "170:00"} {
		if _, err := parseReminder(in); err == nil {
			t.Errorf("parseReminder(%q) succeeded, want error", in)
		}
	}
}

func TestCreateTaskReminder(t *testing.T) {
	withFakeStore(t, populatedStore())
	envs := withCapturedWrites(t)

	t.Run("with today date sets ato", func(t *testing.T) {
		if _, err := createTask(CreateTaskRequest{Title: "x", When: "today", Reminder: "17:00"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		payload := lastEnvelope(t, envs).payload.(taskCreatePayload)
		if payload.Ato == nil || *payload.Ato != 61200 {
			t.Fatalf("Ato = %v, want 61200", payload.Ato)
		}
	})

	t.Run("with explicit date sets ato", func(t *testing.T) {
		date := time.Now().UTC().Add(72 * time.Hour).Format("2006-01-02")
		if _, err := createTask(CreateTaskRequest{Title: "x", When: date, Reminder: "09:30"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		payload := lastEnvelope(t, envs).payload.(taskCreatePayload)
		if payload.Ato == nil || *payload.Ato != 9*3600+30*60 {
			t.Fatalf("Ato = %v, want 34200", payload.Ato)
		}
	})

	t.Run("without date rejected", func(t *testing.T) {
		for _, when := range []string{"", "anytime", "someday", "inbox"} {
			if _, err := createTask(CreateTaskRequest{Title: "x", When: when, Reminder: "17:00"}); err == nil {
				t.Errorf("when=%q with reminder succeeded, want error", when)
			} else if !isInvalidInput(err) {
				t.Errorf("when=%q: expected invalid-input error, got %v", when, err)
			}
		}
	})

	t.Run("invalid format rejected", func(t *testing.T) {
		if _, err := createTask(CreateTaskRequest{Title: "x", When: "today", Reminder: "9am"}); err == nil || !isInvalidInput(err) {
			t.Fatalf("expected invalid-input error, got %v", err)
		}
	})

	t.Run("none treated as unset", func(t *testing.T) {
		if _, err := createTask(CreateTaskRequest{Title: "x", Reminder: "none"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		payload := lastEnvelope(t, envs).payload.(taskCreatePayload)
		if payload.Ato != nil {
			t.Fatalf("Ato = %v, want nil", *payload.Ato)
		}
	})
}

func TestEditTaskReminder(t *testing.T) {
	withFakeStore(t, storeWithScheduledTask())
	envs := withCapturedWrites(t)

	fieldsOf := func(t *testing.T) map[string]any {
		t.Helper()
		return lastEnvelope(t, envs).payload.(map[string]any)
	}

	t.Run("set on task with existing date", func(t *testing.T) {
		if err := editTask(EditTaskRequest{UUID: testScheduledTaskUUID, Reminder: "17:00"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got, ok := fieldsOf(t)["ato"]; !ok || got != 61200 {
			t.Fatalf("ato = %v (present=%v), want 61200", got, ok)
		}
	})

	t.Run("set together with when", func(t *testing.T) {
		if err := editTask(EditTaskRequest{UUID: testTaskUUID, When: "today", Reminder: "08:15"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got, ok := fieldsOf(t)["ato"]; !ok || got != 8*3600+15*60 {
			t.Fatalf("ato = %v (present=%v), want 29700", got, ok)
		}
	})

	t.Run("rejected without any date", func(t *testing.T) {
		// testTaskUUID is Anytime with no scheduled date.
		err := editTask(EditTaskRequest{UUID: testTaskUUID, Reminder: "17:00"})
		if err == nil || !isInvalidInput(err) {
			t.Fatalf("expected invalid-input error, got %v", err)
		}
	})

	t.Run("rejected when new when drops the date", func(t *testing.T) {
		err := editTask(EditTaskRequest{UUID: testScheduledTaskUUID, When: "anytime", Reminder: "17:00"})
		if err == nil || !isInvalidInput(err) {
			t.Fatalf("expected invalid-input error, got %v", err)
		}
	})

	t.Run("none clears", func(t *testing.T) {
		if err := editTask(EditTaskRequest{UUID: testScheduledTaskUUID, Reminder: "none"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got, ok := fieldsOf(t)["ato"]; !ok || got != nil {
			t.Fatalf("ato = %v (present=%v), want explicit nil", got, ok)
		}
	})

	t.Run("removing the date clears the reminder", func(t *testing.T) {
		for _, when := range []string{"anytime", "someday", "inbox", "none"} {
			if err := editTask(EditTaskRequest{UUID: testScheduledTaskUUID, When: when}); err != nil {
				t.Fatalf("when=%q: unexpected error: %v", when, err)
			}
			if got, ok := fieldsOf(t)["ato"]; !ok || got != nil {
				t.Fatalf("when=%q: ato = %v (present=%v), want explicit nil", when, got, ok)
			}
		}
	})

	t.Run("dated when keeps reminder untouched", func(t *testing.T) {
		if err := editTask(EditTaskRequest{UUID: testScheduledTaskUUID, When: "today"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := fieldsOf(t)["ato"]; ok {
			t.Fatal("ato present on dated reschedule without reminder param, want absent")
		}
	})
}

func TestMovesClearReminder(t *testing.T) {
	withFakeStore(t, populatedStore())
	envs := withCapturedWrites(t)

	moves := map[string]func(string) error{
		"anytime": moveTaskToAnytime,
		"someday": moveTaskToSomeday,
		"inbox":   moveTaskToInbox,
	}
	for name, move := range moves {
		if err := move(testTaskUUID); err != nil {
			t.Fatalf("move to %s: %v", name, err)
		}
		fields := lastEnvelope(t, envs).payload.(map[string]any)
		if got, ok := fields["ato"]; !ok || got != nil {
			t.Errorf("move to %s: ato = %v (present=%v), want explicit nil", name, got, ok)
		}
	}

	if err := moveTaskToToday(testTaskUUID, ""); err != nil {
		t.Fatalf("move to today: %v", err)
	}
	fields := lastEnvelope(t, envs).payload.(map[string]any)
	if _, ok := fields["ato"]; ok {
		t.Error("move to today: ato present, want absent (date remains)")
	}
}

func TestFormatTaskReminder(t *testing.T) {
	offset := 61200
	out := formatTask(&thingscloud.Task{UUID: "u", Title: "t", AlarmTimeOffset: &offset})
	if out.Reminder != "17:00" {
		t.Errorf("Reminder = %q, want %q", out.Reminder, "17:00")
	}

	out = formatTask(&thingscloud.Task{UUID: "u", Title: "t"})
	if out.Reminder != "" {
		t.Errorf("Reminder = %q for nil offset, want empty", out.Reminder)
	}
}

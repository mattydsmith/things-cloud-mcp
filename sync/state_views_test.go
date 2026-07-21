package sync

import (
	"path/filepath"
	"testing"
	"time"

	things "github.com/arthursoares/things-cloud-sdk"
)

// newViewTestSyncer opens an empty syncer backed by a temp database.
func newViewTestSyncer(t *testing.T) *Syncer {
	t.Helper()
	syncer, err := Open(filepath.Join(t.TempDir(), "views.db"), nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { syncer.Close() })
	return syncer
}

// saveScheduled stores a schedule=1 task whose scheduled date is `dayOffset`
// days from the current UTC day (negative values are in the past).
func saveScheduled(t *testing.T, syncer *Syncer, uuid, title string, status things.TaskStatus, dayOffset int) {
	t.Helper()
	nowUTC := time.Now().UTC()
	day := time.Date(nowUTC.Year(), nowUTC.Month(), nowUTC.Day(), 0, 0, 0, 0, time.UTC).
		AddDate(0, 0, dayOffset)
	task := &things.Task{
		UUID:          uuid,
		Title:         title,
		Status:        status,
		Schedule:      things.TaskScheduleAnytime,
		Type:          things.TaskTypeTask,
		ScheduledDate: &day,
		CreationDate:  nowUTC,
	}
	if err := syncer.saveTask(task); err != nil {
		t.Fatalf("saveTask(%s) failed: %v", uuid, err)
	}
}

func titleSet(tasks []*things.Task) map[string]bool {
	got := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		got[task.Title] = true
	}
	return got
}

// Things rolls overdue tasks forward into Today instead of hiding them. A
// same-day-only window silently returned an empty Today for anyone whose tasks
// were all scheduled on an earlier date.
func TestTasksInTodayIncludesRolledOverTasks(t *testing.T) {
	t.Parallel()
	syncer := newViewTestSyncer(t)

	saveScheduled(t, syncer, "today-1", "Scheduled today", things.TaskStatusPending, 0)
	saveScheduled(t, syncer, "overdue-1", "Scheduled yesterday", things.TaskStatusPending, -1)
	saveScheduled(t, syncer, "overdue-2", "Scheduled last year", things.TaskStatusPending, -300)
	saveScheduled(t, syncer, "future-1", "Scheduled tomorrow", things.TaskStatusPending, 1)

	tasks, err := syncer.State().TasksInToday(QueryOpts{})
	if err != nil {
		t.Fatalf("TasksInToday failed: %v", err)
	}
	got := titleSet(tasks)

	for _, want := range []string{"Scheduled today", "Scheduled yesterday", "Scheduled last year"} {
		if !got[want] {
			t.Errorf("TasksInToday missing %q; Today must include rolled-over tasks", want)
		}
	}
	if got["Scheduled tomorrow"] {
		t.Error("TasksInToday returned a future-dated task")
	}
}

// Today and Anytime are complements: a task belongs to exactly one of them.
func TestTodayAndAnytimeArePartitioned(t *testing.T) {
	t.Parallel()
	syncer := newViewTestSyncer(t)

	saveScheduled(t, syncer, "today-1", "Scheduled today", things.TaskStatusPending, 0)
	saveScheduled(t, syncer, "overdue-1", "Scheduled yesterday", things.TaskStatusPending, -1)
	saveScheduled(t, syncer, "future-1", "Scheduled tomorrow", things.TaskStatusPending, 1)

	state := syncer.State()
	today, err := state.TasksInToday(QueryOpts{})
	if err != nil {
		t.Fatalf("TasksInToday failed: %v", err)
	}
	anytime, err := state.TasksInAnytime(QueryOpts{})
	if err != nil {
		t.Fatalf("TasksInAnytime failed: %v", err)
	}

	inToday, inAnytime := titleSet(today), titleSet(anytime)
	for title := range inToday {
		if inAnytime[title] {
			t.Errorf("task %q appears in both Today and Anytime", title)
		}
	}
	if len(today)+len(anytime) != 3 {
		t.Errorf("Today(%d) + Anytime(%d) = %d, want 3 tasks total",
			len(today), len(anytime), len(today)+len(anytime))
	}
}

// Things hides canceled and completed tasks alike. Filtering on completion
// alone left canceled tasks in every view.
func TestViewsExcludeCanceledTasks(t *testing.T) {
	t.Parallel()
	syncer := newViewTestSyncer(t)

	inbox := &things.Task{
		UUID: "inbox-open", Title: "Open inbox task",
		Status: things.TaskStatusPending, Schedule: things.TaskScheduleInbox,
		Type: things.TaskTypeTask, CreationDate: time.Now().UTC(),
	}
	canceled := &things.Task{
		UUID: "inbox-canceled", Title: "Canceled inbox task",
		Status: things.TaskStatusCanceled, Schedule: things.TaskScheduleInbox,
		Type: things.TaskTypeTask, CreationDate: time.Now().UTC(),
	}
	completed := &things.Task{
		UUID: "inbox-completed", Title: "Completed inbox task",
		Status: things.TaskStatusCompleted, Schedule: things.TaskScheduleInbox,
		Type: things.TaskTypeTask, CreationDate: time.Now().UTC(),
	}
	for _, task := range []*things.Task{inbox, canceled, completed} {
		if err := syncer.saveTask(task); err != nil {
			t.Fatalf("saveTask(%s) failed: %v", task.UUID, err)
		}
	}

	tasks, err := syncer.State().TasksInInbox(QueryOpts{})
	if err != nil {
		t.Fatalf("TasksInInbox failed: %v", err)
	}
	got := titleSet(tasks)
	if !got["Open inbox task"] {
		t.Error("TasksInInbox dropped the open task")
	}
	if got["Canceled inbox task"] {
		t.Error("TasksInInbox returned a canceled task")
	}
	if got["Completed inbox task"] {
		t.Error("TasksInInbox returned a completed task")
	}

	// Canceled tasks in Today were hidden by the old same-day window; they
	// surface once rolled-over tasks are included, so cover that path too.
	saveScheduled(t, syncer, "today-canceled", "Canceled overdue task", things.TaskStatusCanceled, -2)
	todayTasks, err := syncer.State().TasksInToday(QueryOpts{})
	if err != nil {
		t.Fatalf("TasksInToday failed: %v", err)
	}
	if titleSet(todayTasks)["Canceled overdue task"] {
		t.Error("TasksInToday returned a canceled task")
	}
}

// IncludeCompleted remains an opt-in escape hatch for both done statuses.
func TestIncludeCompletedReturnsCanceledAndCompleted(t *testing.T) {
	t.Parallel()
	syncer := newViewTestSyncer(t)

	for _, task := range []*things.Task{
		{UUID: "a", Title: "Open", Status: things.TaskStatusPending,
			Schedule: things.TaskScheduleInbox, Type: things.TaskTypeTask, CreationDate: time.Now().UTC()},
		{UUID: "b", Title: "Canceled", Status: things.TaskStatusCanceled,
			Schedule: things.TaskScheduleInbox, Type: things.TaskTypeTask, CreationDate: time.Now().UTC()},
		{UUID: "c", Title: "Completed", Status: things.TaskStatusCompleted,
			Schedule: things.TaskScheduleInbox, Type: things.TaskTypeTask, CreationDate: time.Now().UTC()},
	} {
		if err := syncer.saveTask(task); err != nil {
			t.Fatalf("saveTask(%s) failed: %v", task.UUID, err)
		}
	}

	tasks, err := syncer.State().TasksInInbox(QueryOpts{IncludeCompleted: true})
	if err != nil {
		t.Fatalf("TasksInInbox failed: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("IncludeCompleted returned %d tasks, want 3", len(tasks))
	}
}

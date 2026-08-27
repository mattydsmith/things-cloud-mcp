package sync

import (
	"path/filepath"
	"testing"
	"time"

	things "github.com/arthursoares/things-cloud-sdk"
)

func TestTasksWithTag(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	syncer, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer syncer.Close()

	now := time.Now().Truncate(time.Second)
	if err := syncer.saveTag(&things.Tag{UUID: "tag-1", Title: "errand"}); err != nil {
		t.Fatalf("saveTag failed: %v", err)
	}

	tasks := []*things.Task{
		{UUID: "task-tagged", Title: "Tagged", Type: things.TaskTypeTask, Schedule: things.TaskScheduleAnytime, CreationDate: now, TagIDs: []string{"tag-1"}},
		{UUID: "task-untagged", Title: "Untagged", Type: things.TaskTypeTask, Schedule: things.TaskScheduleAnytime, CreationDate: now},
		{UUID: "task-tagged-done", Title: "Tagged done", Type: things.TaskTypeTask, Schedule: things.TaskScheduleAnytime, CreationDate: now, TagIDs: []string{"tag-1"}, Status: things.TaskStatusCompleted},
	}
	for _, task := range tasks {
		if err := syncer.saveTask(task); err != nil {
			t.Fatalf("saveTask(%s) failed: %v", task.UUID, err)
		}
	}

	state := syncer.State()

	got, err := state.TasksWithTag("tag-1", QueryOpts{})
	if err != nil {
		t.Fatalf("TasksWithTag failed: %v", err)
	}
	if len(got) != 1 || got[0].UUID != "task-tagged" {
		t.Fatalf("TasksWithTag = %d tasks (%v), want just task-tagged", len(got), uuidsOf(got))
	}

	got, err = state.TasksWithTag("tag-1", QueryOpts{IncludeCompleted: true})
	if err != nil {
		t.Fatalf("TasksWithTag (completed) failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("TasksWithTag incl. completed = %d tasks (%v), want 2", len(got), uuidsOf(got))
	}

	got, err = state.TasksWithTag("no-such-tag", QueryOpts{})
	if err != nil {
		t.Fatalf("TasksWithTag (unknown) failed: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("TasksWithTag unknown tag = %d tasks, want 0", len(got))
	}
}

func uuidsOf(tasks []*things.Task) []string {
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.UUID
	}
	return ids
}

package sync

import (
	"path/filepath"
	"testing"

	things "github.com/arthursoares/things-cloud-sdk"
)

func TestStateChecklistItem(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	syncer, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer syncer.Close()

	item := &things.CheckListItem{
		UUID:    "check-state-1",
		Title:   "Lookup me",
		TaskIDs: []string{"task-state-1"},
		Status:  things.TaskStatusPending,
	}
	if err := syncer.saveChecklistItem(item); err != nil {
		t.Fatalf("saveChecklistItem failed: %v", err)
	}

	state := syncer.State()

	t.Run("existing item", func(t *testing.T) {
		got, err := state.ChecklistItem("check-state-1")
		if err != nil {
			t.Fatalf("ChecklistItem failed: %v", err)
		}
		if got == nil || got.Title != "Lookup me" {
			t.Fatalf("unexpected item: %+v", got)
		}
	})

	t.Run("missing item returns nil", func(t *testing.T) {
		got, err := state.ChecklistItem("no-such-item")
		if err != nil {
			t.Fatalf("ChecklistItem failed: %v", err)
		}
		if got != nil {
			t.Fatalf("expected nil for missing item, got: %+v", got)
		}
	})
}

package sync

import (
	"encoding/json"
	"path/filepath"
	"testing"

	things "github.com/arthursoares/things-cloud-sdk"
)

// Things 3.23+ clients write Task7 items (field-identical to Task6). The
// engine previously dropped them as unknown kinds, silently losing every
// app-originated change after the client upgrade.
func TestProcessTask7Items(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	syncer, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer syncer.Close()

	title := "Written by Things 3.23"
	tp := things.TaskTypeTask
	payload := things.TaskActionItemPayload{}
	payload.Title = &title
	payload.Type = &tp
	payloadBytes, _ := json.Marshal(payload)

	changes, err := syncer.processItems([]things.Item{{
		UUID:   "task7-001",
		Kind:   things.ItemKindTask7,
		Action: things.ItemActionCreated,
		P:      payloadBytes,
	}}, 0)
	if err != nil {
		t.Fatalf("processItems failed: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change for a Task7 create, got %d", len(changes))
	}
	if _, ok := changes[0].(TaskCreated); !ok {
		t.Fatalf("expected TaskCreated, got %T", changes[0])
	}

	task, err := syncer.State().Task("task7-001")
	if err != nil {
		t.Fatalf("Task lookup failed: %v", err)
	}
	if task == nil || task.Title != title {
		t.Fatalf("Task7 item not persisted correctly: %+v", task)
	}

	// Completion via a Task7 update must apply too.
	ss := things.TaskStatusCompleted
	upd := things.TaskActionItemPayload{}
	upd.Status = &ss
	updBytes, _ := json.Marshal(upd)
	if _, err := syncer.processItems([]things.Item{{
		UUID:   "task7-001",
		Kind:   things.ItemKindTask7,
		Action: things.ItemActionModified,
		P:      updBytes,
	}}, 1); err != nil {
		t.Fatalf("processItems update failed: %v", err)
	}
	task, _ = syncer.State().Task("task7-001")
	if task == nil || task.Status != things.TaskStatusCompleted {
		t.Fatalf("Task7 completion not applied: %+v", task)
	}
}

func TestResetLocalStateWipesMirrorAndCursor(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	syncer, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer syncer.Close()

	if err := syncer.saveTask(&things.Task{UUID: "stale-1", Title: "from old stream"}); err != nil {
		t.Fatalf("saveTask failed: %v", err)
	}
	if err := syncer.saveSyncState("old-history-key", 42); err != nil {
		t.Fatalf("saveSyncState failed: %v", err)
	}

	if err := syncer.resetLocalState(); err != nil {
		t.Fatalf("resetLocalState failed: %v", err)
	}

	task, err := syncer.State().Task("stale-1")
	if err != nil {
		t.Fatalf("Task lookup failed: %v", err)
	}
	if task != nil {
		t.Fatalf("expected mirror to be empty after reset, found: %+v", task)
	}
	key, idx, err := syncer.getSyncState()
	if err != nil {
		t.Fatalf("getSyncState failed: %v", err)
	}
	if key != "" || idx != 0 {
		t.Fatalf("expected cleared sync cursor, got key=%q idx=%d", key, idx)
	}
}

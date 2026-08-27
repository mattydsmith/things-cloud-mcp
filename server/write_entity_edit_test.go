package main

import "testing"

func TestEditArea(t *testing.T) {
	withFakeStore(t, populatedStore())
	envs := withCapturedWrites(t)

	t.Run("renames", func(t *testing.T) {
		if err := editArea(testAreaUUID, "New Name"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		env := lastEnvelope(t, envs)
		if env.action != 1 || env.kind != "Area3" || env.id != testAreaUUID {
			t.Fatalf("envelope = action %d kind %s id %s, want modify Area3 %s", env.action, env.kind, env.id, testAreaUUID)
		}
		fields := env.payload.(map[string]any)
		if fields["tt"] != "New Name" {
			t.Fatalf("tt = %v, want New Name", fields["tt"])
		}
	})

	t.Run("empty title rejected", func(t *testing.T) {
		if err := editArea(testAreaUUID, ""); err == nil || !isInvalidInput(err) {
			t.Fatalf("expected invalid-input error, got %v", err)
		}
	})

	t.Run("unknown area rejected", func(t *testing.T) {
		expectNotFound(t, editArea(missingUUID, "x"))
	})
}

func TestEditTag(t *testing.T) {
	withFakeStore(t, populatedStore())
	envs := withCapturedWrites(t)

	t.Run("renames title", func(t *testing.T) {
		if err := editTag(testTagUUID, "Renamed", ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		env := lastEnvelope(t, envs)
		if env.action != 1 || env.kind != "Tag4" {
			t.Fatalf("envelope = action %d kind %s, want modify Tag4", env.action, env.kind)
		}
		fields := env.payload.(map[string]any)
		if fields["tt"] != "Renamed" {
			t.Fatalf("tt = %v, want Renamed", fields["tt"])
		}
		if _, ok := fields["sh"]; ok {
			t.Fatal("sh present without shorthand param, want absent")
		}
	})

	t.Run("sets shorthand", func(t *testing.T) {
		if err := editTag(testTagUUID, "", "R"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		fields := lastEnvelope(t, envs).payload.(map[string]any)
		if fields["sh"] != "R" {
			t.Fatalf("sh = %v, want R", fields["sh"])
		}
		if _, ok := fields["tt"]; ok {
			t.Fatal("tt present without title param, want absent")
		}
	})

	t.Run("shorthand none clears", func(t *testing.T) {
		if err := editTag(testTagUUID, "", "none"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		fields := lastEnvelope(t, envs).payload.(map[string]any)
		if sh, ok := fields["sh"]; !ok || sh != nil {
			t.Fatalf("sh = %v (present=%v), want explicit nil", sh, ok)
		}
	})

	t.Run("no changes rejected", func(t *testing.T) {
		if err := editTag(testTagUUID, "", ""); err == nil || !isInvalidInput(err) {
			t.Fatalf("expected invalid-input error, got %v", err)
		}
	})

	t.Run("unknown tag rejected", func(t *testing.T) {
		expectNotFound(t, editTag(missingUUID, "x", ""))
	})
}

func TestEditChecklistItem(t *testing.T) {
	withFakeStore(t, populatedStore())
	envs := withCapturedWrites(t)

	t.Run("renames", func(t *testing.T) {
		if err := editChecklistItem(testItemUUID, "Buy milk"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		env := lastEnvelope(t, envs)
		if env.action != 1 || env.kind != "ChecklistItem3" {
			t.Fatalf("envelope = action %d kind %s, want modify ChecklistItem3", env.action, env.kind)
		}
		fields := env.payload.(map[string]any)
		if fields["tt"] != "Buy milk" {
			t.Fatalf("tt = %v, want Buy milk", fields["tt"])
		}
		if _, ok := fields["md"]; !ok {
			t.Fatal("md missing, want modification timestamp")
		}
	})

	t.Run("empty title rejected", func(t *testing.T) {
		if err := editChecklistItem(testItemUUID, ""); err == nil || !isInvalidInput(err) {
			t.Fatalf("expected invalid-input error, got %v", err)
		}
	})

	t.Run("unknown item rejected", func(t *testing.T) {
		expectNotFound(t, editChecklistItem(missingUUID, "x"))
	})
}

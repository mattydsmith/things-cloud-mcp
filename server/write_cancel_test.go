package main

import "testing"

func TestCancelTask(t *testing.T) {
	withFakeStore(t, populatedStore())
	envs := withCapturedWrites(t)

	t.Run("writes canceled status with stop date", func(t *testing.T) {
		if err := cancelTask(testTaskUUID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		env := lastEnvelope(t, envs)
		if env.action != 1 || env.kind != "Task6" {
			t.Fatalf("envelope = action %d kind %s, want modify Task6", env.action, env.kind)
		}
		fields := env.payload.(map[string]any)
		if ss, ok := fields["ss"]; !ok || ss != 2 {
			t.Fatalf("ss = %v (present=%v), want 2 (canceled)", ss, ok)
		}
		if sp, ok := fields["sp"]; !ok || sp == nil {
			t.Fatalf("sp = %v (present=%v), want a stop timestamp", sp, ok)
		}
	})

	t.Run("unknown task rejected", func(t *testing.T) {
		expectNotFound(t, cancelTask(missingUUID))
	})

	t.Run("malformed uuid rejected", func(t *testing.T) {
		if err := cancelTask("not-a-uuid"); err == nil || !isInvalidInput(err) {
			t.Fatalf("expected invalid-input error, got %v", err)
		}
	})
}

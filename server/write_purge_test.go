package main

import "testing"

func TestPurgeTask(t *testing.T) {
	withFakeStore(t, populatedStore())
	envs := withCapturedWrites(t)

	t.Run("writes tombstone", func(t *testing.T) {
		if err := purgeTask(testTaskUUID); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		env := lastEnvelope(t, envs)
		if env.action != 0 || env.kind != "Tombstone2" {
			t.Fatalf("envelope = action %d kind %s, want create Tombstone2", env.action, env.kind)
		}
		if env.id == testTaskUUID {
			t.Fatal("tombstone must get its own UUID, not reuse the task's")
		}
		fields := env.payload.(map[string]any)
		if fields["dloid"] != testTaskUUID {
			t.Fatalf("dloid = %v, want %s", fields["dloid"], testTaskUUID)
		}
		if _, ok := fields["dld"]; !ok {
			t.Fatal("dld missing, want deletion timestamp")
		}
	})

	t.Run("unknown task rejected", func(t *testing.T) {
		expectNotFound(t, purgeTask(missingUUID))
	})
}

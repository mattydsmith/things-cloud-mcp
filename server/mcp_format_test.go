package main

import (
	"encoding/json"
	"testing"
	"time"

	things "github.com/arthursoares/things-cloud-sdk"
)

func TestFormatTaskJSONShape(t *testing.T) {
	t.Parallel()

	t.Run("includes completed_at and today_index_ref", func(t *testing.T) {
		t.Parallel()

		completedAt := time.Date(2026, 3, 5, 10, 30, 0, 0, time.FixedZone("UTC+1", 3600))
		todayRef := time.Date(2026, 3, 6, 12, 0, 0, 0, time.FixedZone("UTC-5", -5*3600))
		task := &things.Task{
			UUID:                "task-1",
			Title:               "Completed task",
			Status:              things.TaskStatusCompleted,
			Type:                things.TaskTypeTask,
			CompletionDate:      &completedAt,
			TodayIndexReference: &todayRef,
		}

		b, err := json.Marshal(formatTask(task))
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}

		var got map[string]any
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}

		if got["completed_at"] != "2026-03-05T09:30:00Z" {
			t.Fatalf("expected completed_at to be normalized to UTC, got %v", got["completed_at"])
		}
		if got["today_index_ref"] != "2026-03-06" {
			t.Fatalf("expected today_index_ref date string, got %v", got["today_index_ref"])
		}
	})

	t.Run("omits completed_at and today_index_ref when absent", func(t *testing.T) {
		t.Parallel()

		task := &things.Task{
			UUID:   "task-2",
			Title:  "Open task",
			Status: things.TaskStatusPending,
			Type:   things.TaskTypeTask,
		}

		b, err := json.Marshal(formatTask(task))
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}

		var got map[string]any
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}

		if _, ok := got["completed_at"]; ok {
			t.Fatal("did not expect completed_at in output")
		}
		if _, ok := got["today_index_ref"]; ok {
			t.Fatal("did not expect today_index_ref in output")
		}
	})

	t.Run("normalizes the never-ending sentinel deadline to absent", func(t *testing.T) {
		t.Parallel()

		sentinel := time.Date(things.NeverendingRepeatYear, time.January, 1, 0, 0, 0, 0, time.UTC)
		task := &things.Task{
			UUID:         "task-3",
			Title:        "Task with sentinel deadline",
			Status:       things.TaskStatusPending,
			Type:         things.TaskTypeTask,
			DeadlineDate: &sentinel,
		}

		b, err := json.Marshal(formatTask(task))
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}

		var got map[string]any
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}

		if _, ok := got["deadline"]; ok {
			t.Fatalf("did not expect deadline in output for sentinel date, got %v", got["deadline"])
		}
	})

	t.Run("includes a real deadline", func(t *testing.T) {
		t.Parallel()

		deadline := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
		task := &things.Task{
			UUID:         "task-4",
			Title:        "Task with real deadline",
			Status:       things.TaskStatusPending,
			Type:         things.TaskTypeTask,
			DeadlineDate: &deadline,
		}

		b, err := json.Marshal(formatTask(task))
		if err != nil {
			t.Fatalf("marshal failed: %v", err)
		}

		var got map[string]any
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}

		if got["deadline"] != "2026-03-10" {
			t.Fatalf("expected deadline 2026-03-10, got %v", got["deadline"])
		}
	})
}

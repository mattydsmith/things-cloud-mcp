package sync

import (
	"path/filepath"
	"testing"
	"time"

	things "github.com/arthursoares/things-cloud-sdk"
)

func TestTaskStorageAlarmAndRepeater(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	syncer, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer syncer.Close()

	offset := 61200
	task := &things.Task{
		UUID:            "task-alarm",
		Title:           "Remind me",
		Status:          things.TaskStatusPending,
		Schedule:        things.TaskScheduleAnytime,
		Type:            things.TaskTypeTask,
		CreationDate:    time.Now().Truncate(time.Second),
		AlarmTimeOffset: &offset,
		Repeater: &things.RepeaterConfiguration{
			FrequencyUnit:      things.FrequencyUnitWeekly,
			FrequencyAmplitude: 2,
		},
	}

	if err := syncer.saveTask(task); err != nil {
		t.Fatalf("saveTask failed: %v", err)
	}

	got, err := syncer.getTask("task-alarm")
	if err != nil {
		t.Fatalf("getTask failed: %v", err)
	}
	if got == nil {
		t.Fatal("task not found")
	}
	if got.AlarmTimeOffset == nil || *got.AlarmTimeOffset != 61200 {
		t.Errorf("AlarmTimeOffset = %v, want 61200", got.AlarmTimeOffset)
	}
	if got.Repeater == nil {
		t.Fatal("Repeater = nil, want persisted configuration")
	}
	if got.Repeater.FrequencyUnit != things.FrequencyUnitWeekly {
		t.Errorf("FrequencyUnit = %v, want weekly (256)", got.Repeater.FrequencyUnit)
	}
	if got.Repeater.FrequencyAmplitude != 2 {
		t.Errorf("FrequencyAmplitude = %d, want 2", got.Repeater.FrequencyAmplitude)
	}

	// Saving without a repeater must clear the stored rule.
	task.Repeater = nil
	task.AlarmTimeOffset = nil
	if err := syncer.saveTask(task); err != nil {
		t.Fatalf("saveTask (clear) failed: %v", err)
	}
	got, err = syncer.getTask("task-alarm")
	if err != nil {
		t.Fatalf("getTask (clear) failed: %v", err)
	}
	if got.AlarmTimeOffset != nil {
		t.Errorf("AlarmTimeOffset = %v after clear, want nil", *got.AlarmTimeOffset)
	}
	if got.Repeater != nil {
		t.Errorf("Repeater = %+v after clear, want nil", got.Repeater)
	}
}

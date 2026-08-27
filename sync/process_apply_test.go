package sync

import (
	"encoding/json"
	"testing"

	things "github.com/arthursoares/things-cloud-sdk"
)

func mustUnmarshalPayload(t *testing.T, raw string) things.TaskActionItemPayload {
	t.Helper()
	var p things.TaskActionItemPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return p
}

func TestApplyTaskPayloadAlarmAndRepeater(t *testing.T) {
	t.Parallel()

	offset := 3600
	old := &things.Task{
		UUID:            "task-1",
		Title:           "with reminder",
		AlarmTimeOffset: &offset,
		Repeater: &things.RepeaterConfiguration{
			FrequencyUnit:      things.FrequencyUnitDaily,
			FrequencyAmplitude: 1,
		},
	}

	t.Run("absent keys preserve values", func(t *testing.T) {
		p := mustUnmarshalPayload(t, `{"tt":"renamed"}`)
		got := applyTaskPayload(old, "task-1", p)
		if got.AlarmTimeOffset == nil || *got.AlarmTimeOffset != 3600 {
			t.Errorf("AlarmTimeOffset = %v, want preserved 3600", got.AlarmTimeOffset)
		}
		if got.Repeater == nil {
			t.Error("Repeater = nil, want preserved configuration")
		}
	})

	t.Run("explicit nulls clear values", func(t *testing.T) {
		p := mustUnmarshalPayload(t, `{"ato":null,"rr":null}`)
		got := applyTaskPayload(old, "task-1", p)
		if got.AlarmTimeOffset != nil {
			t.Errorf("AlarmTimeOffset = %v, want cleared", *got.AlarmTimeOffset)
		}
		if got.Repeater != nil {
			t.Errorf("Repeater = %+v, want cleared", got.Repeater)
		}
	})

	t.Run("explicit values overwrite", func(t *testing.T) {
		p := mustUnmarshalPayload(t, `{"ato":61200,"rr":{"fu":256,"fa":2,"of":[]}}`)
		got := applyTaskPayload(old, "task-1", p)
		if got.AlarmTimeOffset == nil || *got.AlarmTimeOffset != 61200 {
			t.Errorf("AlarmTimeOffset = %v, want 61200", got.AlarmTimeOffset)
		}
		if got.Repeater == nil || got.Repeater.FrequencyUnit != things.FrequencyUnitWeekly || got.Repeater.FrequencyAmplitude != 2 {
			t.Errorf("Repeater = %+v, want weekly/2", got.Repeater)
		}
	})
}

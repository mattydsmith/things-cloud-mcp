package thingscloud

import (
	"encoding/json"
	"testing"
)

func TestPayloadPresenceAlarmTimeOffset(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		json    string
		has     bool
		wantVal *int
	}{
		{name: "explicit null clears", json: `{"ato":null}`, has: true, wantVal: nil},
		{name: "explicit value sets", json: `{"ato":3600}`, has: true, wantVal: intPointer(3600)},
		{name: "absent leaves unchanged", json: `{}`, has: false, wantVal: nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var p TaskActionItemPayload
			if err := json.Unmarshal([]byte(tc.json), &p); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}
			if got := p.HasAlarmTimeOffset(); got != tc.has {
				t.Errorf("HasAlarmTimeOffset() = %v, want %v", got, tc.has)
			}
			if tc.wantVal == nil && p.AlarmTimeOffset != nil {
				t.Errorf("AlarmTimeOffset = %v, want nil", *p.AlarmTimeOffset)
			}
			if tc.wantVal != nil && (p.AlarmTimeOffset == nil || *p.AlarmTimeOffset != *tc.wantVal) {
				t.Errorf("AlarmTimeOffset = %v, want %d", p.AlarmTimeOffset, *tc.wantVal)
			}
		})
	}
}

func TestPayloadPresenceRepeater(t *testing.T) {
	t.Parallel()

	t.Run("explicit null clears", func(t *testing.T) {
		var p TaskActionItemPayload
		if err := json.Unmarshal([]byte(`{"rr":null}`), &p); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if !p.HasRepeater() {
			t.Error("HasRepeater() = false for explicit null, want true")
		}
		if p.Repeater != nil {
			t.Errorf("Repeater = %+v, want nil", p.Repeater)
		}
	})

	t.Run("explicit value sets", func(t *testing.T) {
		var p TaskActionItemPayload
		if err := json.Unmarshal([]byte(`{"rr":{"fu":16,"fa":1,"of":[]}}`), &p); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if !p.HasRepeater() {
			t.Error("HasRepeater() = false for explicit value, want true")
		}
		if p.Repeater == nil {
			t.Fatal("Repeater = nil, want parsed configuration")
		}
		if p.Repeater.FrequencyUnit != FrequencyUnitDaily {
			t.Errorf("FrequencyUnit = %v, want daily (16)", p.Repeater.FrequencyUnit)
		}
	})

	t.Run("absent leaves unchanged", func(t *testing.T) {
		var p TaskActionItemPayload
		if err := json.Unmarshal([]byte(`{}`), &p); err != nil {
			t.Fatalf("unmarshal failed: %v", err)
		}
		if p.HasRepeater() {
			t.Error("HasRepeater() = true for absent key, want false")
		}
	})
}

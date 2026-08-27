package main

import (
	"testing"
	"time"

	thingscloud "github.com/arthursoares/things-cloud-sdk"
)

func TestDescribeRepeat(t *testing.T) {
	ts := func(y int, m time.Month, d int) *thingscloud.Timestamp {
		v := thingscloud.Timestamp(time.Date(y, m, d, 0, 0, 0, 0, time.UTC))
		return &v
	}

	cases := []struct {
		name string
		rc   *thingscloud.RepeaterConfiguration
		want string
	}{
		{name: "nil", rc: nil, want: ""},
		{
			name: "daily",
			rc:   &thingscloud.RepeaterConfiguration{FrequencyUnit: thingscloud.FrequencyUnitDaily, FrequencyAmplitude: 1},
			want: "every day",
		},
		{
			name: "every 2 weeks",
			rc:   &thingscloud.RepeaterConfiguration{FrequencyUnit: thingscloud.FrequencyUnitWeekly, FrequencyAmplitude: 2},
			want: "every 2 weeks",
		},
		{
			name: "monthly after completion",
			rc:   &thingscloud.RepeaterConfiguration{FrequencyUnit: thingscloud.FrequencyUnitMonthly, FrequencyAmplitude: 1, Type: 1},
			want: "every month after completion",
		},
		{
			name: "yearly with end date",
			rc: &thingscloud.RepeaterConfiguration{
				FrequencyUnit:      thingscloud.FrequencyUnitYearly,
				FrequencyAmplitude: 1,
				LastScheduledAt:    ts(2027, time.March, 1),
			},
			want: "every year until 2027-03-01",
		},
		{
			name: "neverending end marker omitted",
			rc: &thingscloud.RepeaterConfiguration{
				FrequencyUnit:      thingscloud.FrequencyUnitDaily,
				FrequencyAmplitude: 3,
				LastScheduledAt:    ts(4001, time.January, 1),
			},
			want: "every 3 days",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := describeRepeat(tc.rc); got != tc.want {
				t.Errorf("describeRepeat() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatTaskRepeat(t *testing.T) {
	task := &thingscloud.Task{
		UUID:  "u",
		Title: "t",
		Repeater: &thingscloud.RepeaterConfiguration{
			FrequencyUnit:      thingscloud.FrequencyUnitWeekly,
			FrequencyAmplitude: 1,
		},
	}
	if got := formatTask(task).Repeat; got != "every week" {
		t.Errorf("Repeat = %q, want %q", got, "every week")
	}
	if got := formatTask(&thingscloud.Task{UUID: "u", Title: "t"}).Repeat; got != "" {
		t.Errorf("Repeat = %q for non-repeating task, want empty", got)
	}
}

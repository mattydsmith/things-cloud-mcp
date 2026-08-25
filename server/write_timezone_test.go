package main

import (
	"testing"
	"time"
)

func withFrozenClock(t *testing.T, instant time.Time) {
	t.Helper()
	orig := timeNow
	timeNow = func() time.Time { return instant }
	t.Cleanup(func() { timeNow = orig })
}

func dayUTC(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestLocalCalendarDayUTC(t *testing.T) {
	t.Parallel()

	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load America/New_York: %v", err)
	}
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load Asia/Shanghai: %v", err)
	}

	cases := []struct {
		name    string
		instant time.Time
		loc     *time.Location
		want    time.Time
	}{
		// The incident case: 21:08 EDT on Aug 24 is already Aug 25 in UTC.
		{"late evening west of UTC", time.Date(2026, 8, 25, 1, 8, 0, 0, time.UTC), ny, dayUTC(2026, 8, 24)},
		// Mirror: 06:00 CST on Aug 25 is still Aug 24 in UTC.
		{"early morning east of UTC", time.Date(2026, 8, 24, 22, 0, 0, 0, time.UTC), shanghai, dayUTC(2026, 8, 25)},
		{"utc is identity", time.Date(2026, 8, 24, 22, 0, 0, 0, time.UTC), time.UTC, dayUTC(2026, 8, 24)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := localCalendarDayUTC(tc.instant, tc.loc)
			if !got.Equal(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseWhenTodayResolvesLocalDay(t *testing.T) {
	t.Run("America/New_York at 2026-08-25T01:08Z is Aug 24", func(t *testing.T) {
		t.Setenv("THINGS_TIMEZONE", "America/New_York")
		withFrozenClock(t, time.Date(2026, 8, 25, 1, 8, 0, 0, time.UTC))

		st, sr, tir, ok := parseWhen("today")
		if !ok || st != 1 {
			t.Fatalf("parseWhen(today) = st=%d ok=%v", st, ok)
		}
		want := dayUTC(2026, 8, 24).Unix()
		if sr == nil || *sr != want {
			t.Fatalf("sr = %v, want %d (2026-08-24)", sr, want)
		}
		if tir == nil || *tir != *sr {
			t.Fatalf("today_index_ref must track scheduled date: tir=%v sr=%v", tir, sr)
		}
	})

	t.Run("Asia/Shanghai at 2026-08-24T22:00Z is Aug 25", func(t *testing.T) {
		t.Setenv("THINGS_TIMEZONE", "Asia/Shanghai")
		withFrozenClock(t, time.Date(2026, 8, 24, 22, 0, 0, 0, time.UTC))

		_, sr, tir, ok := parseWhen("today")
		if !ok || sr == nil {
			t.Fatal("parseWhen(today) failed")
		}
		want := dayUTC(2026, 8, 25).Unix()
		if *sr != want || *tir != want {
			t.Fatalf("sr=%d tir=%d, want %d (2026-08-25)", *sr, *tir, want)
		}
	})

	t.Run("unset timezone falls back to UTC day", func(t *testing.T) {
		t.Setenv("THINGS_TIMEZONE", "")
		withFrozenClock(t, time.Date(2026, 8, 25, 1, 8, 0, 0, time.UTC))

		_, sr, _, ok := parseWhen("today")
		if !ok || sr == nil || *sr != dayUTC(2026, 8, 25).Unix() {
			t.Fatalf("expected UTC day 2026-08-25, got %v", sr)
		}
	})

	t.Run("explicit local today is Today, not past", func(t *testing.T) {
		t.Setenv("THINGS_TIMEZONE", "America/New_York")
		withFrozenClock(t, time.Date(2026, 8, 25, 1, 8, 0, 0, time.UTC))

		st, sr, tir, ok := parseWhen("2026-08-24")
		if !ok || st != 1 {
			t.Fatalf("parseWhen(2026-08-24) = st=%d ok=%v, want Today", st, ok)
		}
		want := dayUTC(2026, 8, 24).Unix()
		if *sr != want || *tir != want {
			t.Fatalf("sr=%d tir=%d, want %d", *sr, *tir, want)
		}
	})
}

func TestThingsLocationInvalidFallsBackToUTC(t *testing.T) {
	t.Setenv("THINGS_TIMEZONE", "Not/AZone")
	if loc := thingsLocation(); loc != time.UTC {
		t.Fatalf("invalid timezone should fall back to UTC, got %v", loc)
	}
}

func TestDeadlineTodayLocalNotRejectedAsPast(t *testing.T) {
	withFakeStore(t, populatedStore())
	envs := withCapturedWrites(t)
	t.Setenv("THINGS_TIMEZONE", "America/New_York")
	withFrozenClock(t, time.Date(2026, 8, 25, 1, 8, 0, 0, time.UTC))

	// Local day is Aug 24; a deadline of Aug 24 is today, not the past.
	if _, err := createTask(CreateTaskRequest{Title: "x", Deadline: "2026-08-24"}); err != nil {
		t.Fatalf("deadline on the local today must be accepted, got: %v", err)
	}
	payload := lastEnvelope(t, envs).payload.(taskCreatePayload)
	if payload.Dd == nil || *payload.Dd != dayUTC(2026, 8, 24).Unix() {
		t.Fatalf("unexpected deadline payload: %v", payload.Dd)
	}
}

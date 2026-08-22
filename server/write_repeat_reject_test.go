package main

import (
	"strings"
	"testing"
)

// repeat is currently rejected on both createTask and editTask; see
// docs/known-issues/repeat-disabled.md. These tests confirm the guard fires
// for every real recurrence value, leaves "" and "none" untouched, and never
// reaches the network write path: `history` (server/main.go) is left nil in
// this test binary, so a call that reached historyWrite() would panic on a
// nil dereference rather than return an error. A clean error return with no
// panic is itself proof no write was attempted.
//
// Full end-to-end verification that repeat-free requests still succeed, and
// that repeat:"none" still clears an existing rule, is blocked by the
// pre-existing absence of a `history` test double in this repo and is out of
// scope for this hotfix.

func TestRepeatValueRequestsRule(t *testing.T) {
	t.Parallel()

	cases := []struct {
		repeat string
		want   bool
	}{
		{"", false},
		{"none", false},
		{"None", true}, // case-sensitive, matches buildRepeatRule's existing semantics
		{"daily", true},
		{"every 2 days", true},
		{"daily until 2026-02-24 after completion", true},
		{"garbage", true},
	}

	for _, tc := range cases {
		t.Run(tc.repeat, func(t *testing.T) {
			t.Parallel()
			if got := repeatValueRequestsRule(tc.repeat); got != tc.want {
				t.Fatalf("repeatValueRequestsRule(%q) = %v, want %v", tc.repeat, got, tc.want)
			}
		})
	}
}

func TestCreateTask_RejectsRepeat(t *testing.T) {
	t.Parallel()

	repeats := []string{
		"daily",
		"every 3 weeks",
		"daily until 2026-02-24 after completion",
		"garbage-value",
	}

	for _, repeat := range repeats {
		t.Run(repeat, func(t *testing.T) {
			t.Parallel()

			uuid, err := createTask(CreateTaskRequest{Title: "Test task", Repeat: repeat})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !isInvalidInput(err) {
				t.Fatalf("expected invalid-input error, got: %v", err)
			}
			msg := strings.ToLower(err.Error())
			if !strings.Contains(msg, "repeat") || !strings.Contains(msg, "unsupported") {
				t.Fatalf("expected error mentioning repeat/unsupported, got: %v", err)
			}
			if uuid != "" {
				t.Fatalf("expected empty uuid on rejection, got %q", uuid)
			}
		})
	}
}

func TestEditTask_RejectsRepeat(t *testing.T) {
	t.Parallel()

	repeats := []string{
		"daily",
		"every 3 weeks",
		"daily until 2026-02-24 after completion",
		"garbage-value",
	}

	for _, repeat := range repeats {
		t.Run(repeat, func(t *testing.T) {
			t.Parallel()

			err := editTask(EditTaskRequest{UUID: generateUUID(), Repeat: repeat})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !isInvalidInput(err) {
				t.Fatalf("expected invalid-input error, got: %v", err)
			}
			msg := strings.ToLower(err.Error())
			if !strings.Contains(msg, "repeat") || !strings.Contains(msg, "unsupported") {
				t.Fatalf("expected error mentioning repeat/unsupported, got: %v", err)
			}
		})
	}
}

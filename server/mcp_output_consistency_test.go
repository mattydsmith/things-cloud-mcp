package main

import (
	"testing"

	thingscloud "github.com/arthursoares/things-cloud-sdk"
)

// Every attribute a write tool accepts must be visible in read output, so
// agents can verify their writes landed. These tests cover the read side.

func TestFormatTaskHeadingID(t *testing.T) {
	out := formatTask(&thingscloud.Task{
		UUID:           "u",
		Title:          "t",
		ParentTaskIDs:  []string{"project-uuid"},
		ActionGroupIDs: []string{"heading-uuid"},
	})
	if out.HeadingID != "heading-uuid" {
		t.Errorf("HeadingID = %q, want %q", out.HeadingID, "heading-uuid")
	}
	if out.ProjectID != "project-uuid" {
		t.Errorf("ProjectID = %q, want %q", out.ProjectID, "project-uuid")
	}

	if got := formatTask(&thingscloud.Task{UUID: "u", Title: "t"}).HeadingID; got != "" {
		t.Errorf("HeadingID = %q for headingless task, want empty", got)
	}
}

func TestFormatTaskTrashed(t *testing.T) {
	if !formatTask(&thingscloud.Task{UUID: "u", Title: "t", InTrash: true}).Trashed {
		t.Error("Trashed = false for in-trash task, want true")
	}
	if formatTask(&thingscloud.Task{UUID: "u", Title: "t"}).Trashed {
		t.Error("Trashed = true for live task, want false")
	}
}

func TestFormatTagParent(t *testing.T) {
	out := formatTag(&thingscloud.Tag{
		UUID:         "u",
		Title:        "child",
		ShortHand:    "c",
		ParentTagIDs: []string{"parent-uuid"},
	})
	if out.ParentID != "parent-uuid" {
		t.Errorf("ParentID = %q, want %q", out.ParentID, "parent-uuid")
	}
	if out.Shorthand != "c" {
		t.Errorf("Shorthand = %q, want %q", out.Shorthand, "c")
	}

	if got := formatTag(&thingscloud.Tag{UUID: "u", Title: "root"}).ParentID; got != "" {
		t.Errorf("ParentID = %q for root tag, want empty", got)
	}
}

func TestFormatAreaTags(t *testing.T) {
	out := formatArea(&thingscloud.Area{
		UUID:   "u",
		Title:  "Work",
		TagIDs: []string{"tag-1", "tag-2"},
	})
	if len(out.Tags) != 2 || out.Tags[0] != "tag-1" || out.Tags[1] != "tag-2" {
		t.Errorf("Tags = %v, want [tag-1 tag-2]", out.Tags)
	}
}

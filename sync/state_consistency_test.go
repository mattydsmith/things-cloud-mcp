package sync

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	things "github.com/arthursoares/things-cloud-sdk"
)

func openTestSyncer(t *testing.T) *Syncer {
	t.Helper()
	syncer, err := Open(filepath.Join(t.TempDir(), "test.db"), nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { syncer.Close() })
	return syncer
}

// Headings must appear in project listings — it's the only way agents can
// discover a heading's UUID and verify tasks landed under it.
func TestTasksInProjectIncludesHeadings(t *testing.T) {
	t.Parallel()
	syncer := openTestSyncer(t)

	now := time.Now().UTC().Truncate(time.Second)
	entities := []*things.Task{
		{UUID: "proj-1", Title: "Project", Type: things.TaskTypeProject, Schedule: things.TaskScheduleAnytime, CreationDate: now},
		{UUID: "head-1", Title: "Phase 1", Type: things.TaskTypeHeading, Schedule: things.TaskScheduleAnytime, CreationDate: now, ParentTaskIDs: []string{"proj-1"}},
		{UUID: "task-1", Title: "Do it", Type: things.TaskTypeTask, Schedule: things.TaskScheduleAnytime, CreationDate: now, ParentTaskIDs: []string{"proj-1"}, ActionGroupIDs: []string{"head-1"}},
	}
	for _, e := range entities {
		if err := syncer.saveTask(e); err != nil {
			t.Fatalf("saveTask(%s): %v", e.UUID, err)
		}
	}

	got, err := syncer.State().TasksInProject("proj-1", QueryOpts{})
	if err != nil {
		t.Fatalf("TasksInProject failed: %v", err)
	}
	byUUID := map[string]*things.Task{}
	for _, task := range got {
		byUUID[task.UUID] = task
	}
	if byUUID["head-1"] == nil {
		t.Fatalf("heading missing from project listing, got %v", uuidsOf(got))
	}
	if byUUID["task-1"] == nil {
		t.Fatalf("task missing from project listing, got %v", uuidsOf(got))
	}
	if ids := byUUID["task-1"].ActionGroupIDs; len(ids) != 1 || ids[0] != "head-1" {
		t.Errorf("task ActionGroupIDs = %v, want [head-1]", ids)
	}
}

// Tag parents must survive the list query, not just single-tag lookup.
func TestAllTagsIncludeParent(t *testing.T) {
	t.Parallel()
	syncer := openTestSyncer(t)

	if err := syncer.saveTag(&things.Tag{UUID: "tag-parent", Title: "errands"}); err != nil {
		t.Fatalf("saveTag: %v", err)
	}
	if err := syncer.saveTag(&things.Tag{UUID: "tag-child", Title: "groceries", ParentTagIDs: []string{"tag-parent"}}); err != nil {
		t.Fatalf("saveTag: %v", err)
	}

	tags, err := syncer.State().AllTags()
	if err != nil {
		t.Fatalf("AllTags failed: %v", err)
	}
	var child *things.Tag
	for _, tag := range tags {
		if tag.UUID == "tag-child" {
			child = tag
		}
	}
	if child == nil {
		t.Fatal("tag-child missing from AllTags")
	}
	if len(child.ParentTagIDs) != 1 || child.ParentTagIDs[0] != "tag-parent" {
		t.Errorf("ParentTagIDs = %v, want [tag-parent]", child.ParentTagIDs)
	}
}

// Area tags must round-trip through item processing and storage so agents
// can verify tags passed to things_create_area actually stuck.
func TestAreaTagsRoundTrip(t *testing.T) {
	t.Parallel()
	syncer := openTestSyncer(t)

	title := "Work"
	payload, _ := json.Marshal(map[string]any{"tt": title, "tg": []string{"tag-1", "tag-2"}, "ix": 0})
	item := things.Item{
		UUID:   "area-tags",
		Kind:   things.ItemKindArea3,
		Action: things.ItemActionCreated,
		P:      payload,
	}
	if _, err := syncer.processItems([]things.Item{item}, 0); err != nil {
		t.Fatalf("processItems failed: %v", err)
	}

	area, err := syncer.State().Area("area-tags")
	if err != nil {
		t.Fatalf("Area failed: %v", err)
	}
	if area == nil {
		t.Fatal("area not found")
	}
	if len(area.TagIDs) != 2 {
		t.Fatalf("TagIDs = %v, want [tag-1 tag-2]", area.TagIDs)
	}

	// A modify without tg must preserve tags; retitle only.
	newTitle := "Work (renamed)"
	modPayload, _ := json.Marshal(map[string]any{"tt": newTitle})
	mod := things.Item{UUID: "area-tags", Kind: things.ItemKindArea3, Action: things.ItemActionModified, P: modPayload}
	if _, err := syncer.processItems([]things.Item{mod}, 1); err != nil {
		t.Fatalf("processItems (modify) failed: %v", err)
	}
	area, err = syncer.State().Area("area-tags")
	if err != nil {
		t.Fatalf("Area (after modify) failed: %v", err)
	}
	if area.Title != newTitle {
		t.Errorf("Title = %q, want %q", area.Title, newTitle)
	}
	if len(area.TagIDs) != 2 {
		t.Errorf("TagIDs after retitle = %v, want preserved [tag-1 tag-2]", area.TagIDs)
	}

	// AllAreas must carry tags too.
	areas, err := syncer.State().AllAreas()
	if err != nil {
		t.Fatalf("AllAreas failed: %v", err)
	}
	if len(areas) != 1 || len(areas[0].TagIDs) != 2 {
		t.Errorf("AllAreas tags = %v, want 1 area with 2 tags", areas)
	}
}

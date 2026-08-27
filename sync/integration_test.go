package sync

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	things "github.com/arthursoares/things-cloud-sdk"
)

func TestIntegration(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	syncer, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer syncer.Close()

	t.Run("process task creation", func(t *testing.T) {
		payload := things.TaskActionItemPayload{}
		title := "Buy groceries"
		payload.Title = &title
		tp := things.TaskTypeTask
		payload.Type = &tp

		payloadBytes, _ := json.Marshal(payload)
		item := things.Item{
			UUID:   "task-001",
			Kind:   things.ItemKindTask,
			Action: things.ItemActionCreated,
			P:      payloadBytes,
		}

		changes, err := syncer.processItems([]things.Item{item}, 0)
		if err != nil {
			t.Fatalf("processItems failed: %v", err)
		}

		if len(changes) != 1 {
			t.Fatalf("expected 1 change, got %d", len(changes))
		}

		created, ok := changes[0].(TaskCreated)
		if !ok {
			t.Fatalf("expected TaskCreated, got %T", changes[0])
		}
		if created.Task.Title != "Buy groceries" {
			t.Errorf("expected title 'Buy groceries', got %q", created.Task.Title)
		}

		// Verify task was persisted
		state := syncer.State()
		task, err := state.Task("task-001")
		if err != nil {
			t.Fatalf("Task lookup failed: %v", err)
		}
		if task == nil {
			t.Fatal("task not persisted")
		}
	})

	t.Run("process task completion", func(t *testing.T) {
		payload := things.TaskActionItemPayload{}
		status := things.TaskStatusCompleted
		payload.Status = &status

		payloadBytes, _ := json.Marshal(payload)
		item := things.Item{
			UUID:   "task-001",
			Kind:   things.ItemKindTask,
			Action: things.ItemActionModified,
			P:      payloadBytes,
		}

		changes, err := syncer.processItems([]things.Item{item}, 1)
		if err != nil {
			t.Fatalf("processItems failed: %v", err)
		}

		if len(changes) != 1 {
			t.Fatalf("expected 1 change, got %d", len(changes))
		}

		_, ok := changes[0].(TaskCompleted)
		if !ok {
			t.Fatalf("expected TaskCompleted, got %T", changes[0])
		}
	})

	t.Run("process task with tir stores TodayIndexReference separately", func(t *testing.T) {
		// Dates must be UTC midnights: that's what production writes, and
		// change detection classifies "today" in UTC. Local midnights make
		// this test fail whenever the local date is ahead of the UTC date.
		now := time.Now().UTC()
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		yesterday := today.Add(-24 * time.Hour)

		payload := things.TaskActionItemPayload{}
		title := "TIR test task"
		payload.Title = &title
		tp := things.TaskTypeTask
		payload.Type = &tp
		sched := things.TaskScheduleAnytime
		payload.Schedule = &sched
		srDate := things.Timestamp(yesterday)
		payload.ScheduledDate = &srDate

		payloadBytes, _ := json.Marshal(payload)
		item := things.Item{
			UUID:   "task-tir",
			Kind:   things.ItemKindTask,
			Action: things.ItemActionCreated,
			P:      payloadBytes,
		}

		if _, err := syncer.processItems([]things.Item{item}, 10); err != nil {
			t.Fatalf("processItems (create) failed: %v", err)
		}

		modPayload := things.TaskActionItemPayload{}
		tirDate := things.Timestamp(today)
		modPayload.TaskIR = &tirDate

		modBytes, _ := json.Marshal(modPayload)
		modItem := things.Item{
			UUID:   "task-tir",
			Kind:   things.ItemKindTask,
			Action: things.ItemActionModified,
			P:      modBytes,
		}

		if _, err := syncer.processItems([]things.Item{modItem}, 11); err != nil {
			t.Fatalf("processItems (modify tir) failed: %v", err)
		}

		task, err := syncer.State().Task("task-tir")
		if err != nil {
			t.Fatalf("Task lookup failed: %v", err)
		}
		if task == nil {
			t.Fatal("task not found")
		}
		if task.ScheduledDate == nil {
			t.Fatal("ScheduledDate should still be set")
		}
		if task.ScheduledDate.Unix() != yesterday.Unix() {
			t.Errorf("ScheduledDate should remain %v, got %v", yesterday, task.ScheduledDate)
		}
		if task.TodayIndexReference == nil {
			t.Fatal("TodayIndexReference should be set")
		}
		if task.TodayIndexReference.Unix() != today.Unix() {
			t.Errorf("TodayIndexReference should be %v, got %v", today, task.TodayIndexReference)
		}
	})

	t.Run("process area creation", func(t *testing.T) {
		payload := things.AreaActionItemPayload{}
		title := "Work"
		payload.Title = &title

		payloadBytes, _ := json.Marshal(payload)
		item := things.Item{
			UUID:   "area-001",
			Kind:   things.ItemKindArea3,
			Action: things.ItemActionCreated,
			P:      payloadBytes,
		}

		changes, err := syncer.processItems([]things.Item{item}, 2)
		if err != nil {
			t.Fatalf("processItems failed: %v", err)
		}

		if len(changes) != 1 {
			t.Fatalf("expected 1 change, got %d", len(changes))
		}

		_, ok := changes[0].(AreaCreated)
		if !ok {
			t.Fatalf("expected AreaCreated, got %T", changes[0])
		}

		state := syncer.State()
		areas, _ := state.AllAreas()
		if len(areas) != 1 {
			t.Errorf("expected 1 area, got %d", len(areas))
		}
	})

	t.Run("query change log", func(t *testing.T) {
		// ChangesSinceIndex(0) returns changes with server_index > 0
		// The test above adds two more task changes at indexes 10 and 11,
		// so this should include indexes 1, 2, 10, and 11.
		changes, err := syncer.ChangesSinceIndex(0)
		if err != nil {
			t.Fatalf("ChangesSinceIndex failed: %v", err)
		}

		if len(changes) != 4 {
			t.Errorf("expected 4 changes after index 0, got %d", len(changes))
		}

		// Test ChangesSinceIndex(-1) to get all changes
		allChanges, err := syncer.ChangesSinceIndex(-1)
		if err != nil {
			t.Fatalf("ChangesSinceIndex failed: %v", err)
		}

		if len(allChanges) != 5 {
			t.Errorf("expected 5 total changes, got %d", len(allChanges))
		}
	})
}

func TestTasksInTodayWithTIR(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	syncer, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer syncer.Close()

	nowUTC := time.Now().UTC()
	today := time.Date(nowUTC.Year(), nowUTC.Month(), nowUTC.Day(), 12, 0, 0, 0, time.UTC)
	yesterday := today.Add(-24 * time.Hour)

	syncer.saveTask(&things.Task{
		UUID:          "sr-only",
		Title:         "SR Only Today",
		Status:        things.TaskStatusPending,
		Schedule:      things.TaskScheduleAnytime,
		ScheduledDate: &today,
	})
	syncer.saveTask(&things.Task{
		UUID:                "tir-only",
		Title:               "TIR Only Today",
		Status:              things.TaskStatusPending,
		Schedule:            things.TaskScheduleAnytime,
		TodayIndexReference: &today,
	})
	syncer.saveTask(&things.Task{
		UUID:                "both-today",
		Title:               "Both Today",
		Status:              things.TaskStatusPending,
		Schedule:            things.TaskScheduleAnytime,
		ScheduledDate:       &today,
		TodayIndexReference: &today,
	})
	syncer.saveTask(&things.Task{
		UUID:                "sr-old-tir-today",
		Title:               "SR Old TIR Today",
		Status:              things.TaskStatusPending,
		Schedule:            things.TaskScheduleAnytime,
		ScheduledDate:       &yesterday,
		TodayIndexReference: &today,
	})
	syncer.saveTask(&things.Task{
		UUID:                "sr-today-tir-old",
		Title:               "SR Today TIR Old",
		Status:              things.TaskStatusPending,
		Schedule:            things.TaskScheduleAnytime,
		ScheduledDate:       &today,
		TodayIndexReference: &yesterday,
	})
	syncer.saveTask(&things.Task{
		UUID:                "both-old",
		Title:               "Both Old",
		Status:              things.TaskStatusPending,
		Schedule:            things.TaskScheduleAnytime,
		ScheduledDate:       &yesterday,
		TodayIndexReference: &yesterday,
	})
	syncer.saveTask(&things.Task{
		UUID:     "no-dates",
		Title:    "No Dates",
		Status:   things.TaskStatusPending,
		Schedule: things.TaskScheduleAnytime,
	})
	syncer.saveTask(&things.Task{
		UUID:                "inbox-with-tir",
		Title:               "Inbox TIR",
		Status:              things.TaskStatusPending,
		Schedule:            things.TaskScheduleInbox,
		TodayIndexReference: &today,
	})

	tasks, err := syncer.State().TasksInToday(QueryOpts{})
	if err != nil {
		t.Fatalf("TasksInToday failed: %v", err)
	}

	got := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		got[task.UUID] = true
	}

	expected := []string{"sr-only", "tir-only", "both-today", "sr-old-tir-today", "sr-today-tir-old"}
	for _, uuid := range expected {
		if !got[uuid] {
			t.Errorf("expected task %q in Today, but not found", uuid)
		}
	}

	notExpected := []string{"both-old", "no-dates", "inbox-with-tir"}
	for _, uuid := range notExpected {
		if got[uuid] {
			t.Errorf("task %q should NOT be in Today", uuid)
		}
	}

	if len(tasks) != len(expected) {
		t.Errorf("expected %d tasks in Today, got %d", len(expected), len(tasks))
	}
}

func TestProcessItemsUsesSourceServerIndex(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	syncer, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer syncer.Close()

	makeTaskItem := func(uuid, title string, serverIndex int) things.Item {
		payload := things.TaskActionItemPayload{}
		payload.Title = &title
		tp := things.TaskTypeTask
		payload.Type = &tp

		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}

		return things.Item{
			UUID:        uuid,
			Kind:        things.ItemKindTask,
			Action:      things.ItemActionCreated,
			P:           payloadBytes,
			ServerIndex: intPtr(serverIndex),
		}
	}

	items := []things.Item{
		makeTaskItem("task-shared-slot-1", "Shared slot 1", 100),
		makeTaskItem("task-shared-slot-2", "Shared slot 2", 100),
	}

	changes, err := syncer.processItems(items, 100)
	if err != nil {
		t.Fatalf("processItems failed: %v", err)
	}

	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}
	for _, change := range changes {
		if change.ServerIndex() != 100 {
			t.Fatalf("change server index = %d, want 100", change.ServerIndex())
		}
	}

	changesAfterSharedIndex, err := syncer.ChangesSinceIndex(100)
	if err != nil {
		t.Fatalf("ChangesSinceIndex(100) failed: %v", err)
	}
	if len(changesAfterSharedIndex) != 0 {
		t.Fatalf("expected no changes after shared server index, got %d", len(changesAfterSharedIndex))
	}

	changesBeforeSharedIndex, err := syncer.ChangesSinceIndex(99)
	if err != nil {
		t.Fatalf("ChangesSinceIndex(99) failed: %v", err)
	}
	if len(changesBeforeSharedIndex) != 2 {
		t.Fatalf("expected 2 changes after index 99, got %d", len(changesBeforeSharedIndex))
	}
	for _, change := range changesBeforeSharedIndex {
		if change.ServerIndex() != 100 {
			t.Fatalf("stored change server index = %d, want 100", change.ServerIndex())
		}
	}
}

func intPtr(v int) *int {
	return &v
}

func TestIntegration_TaskAssignmentChanges(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	syncer, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer syncer.Close()

	t.Run("assigning to project and area emits container changes", func(t *testing.T) {
		project := &things.Task{UUID: "project-1", Title: "Project", Type: things.TaskTypeProject}
		if err := syncer.saveTask(project); err != nil {
			t.Fatalf("saveTask(project) failed: %v", err)
		}
		area := &things.Area{UUID: "area-1", Title: "Work"}
		if err := syncer.saveArea(area); err != nil {
			t.Fatalf("saveArea failed: %v", err)
		}
		task := &things.Task{UUID: "task-assign", Title: "Task", Type: things.TaskTypeTask}
		if err := syncer.saveTask(task); err != nil {
			t.Fatalf("saveTask(task) failed: %v", err)
		}

		parentIDs := []string{"project-1"}
		areaIDs := []string{"area-1"}
		payload := things.TaskActionItemPayload{
			ParentTaskIDs: &parentIDs,
			AreaIDs:       &areaIDs,
		}
		payloadBytes, _ := json.Marshal(payload)
		item := things.Item{
			UUID:   "task-assign",
			Kind:   things.ItemKindTask,
			Action: things.ItemActionModified,
			P:      payloadBytes,
		}

		changes, err := syncer.processItems([]things.Item{item}, 20)
		if err != nil {
			t.Fatalf("processItems failed: %v", err)
		}

		var gotProject, gotArea bool
		for _, change := range changes {
			switch c := change.(type) {
			case TaskAssignedToProject:
				gotProject = true
				if c.Project == nil || c.Project.UUID != "project-1" {
					t.Fatalf("expected new project assignment to project-1, got %#v", c.Project)
				}
				if c.OldProject != nil {
					t.Fatalf("expected no old project, got %#v", c.OldProject)
				}
			case TaskAssignedToArea:
				gotArea = true
				if c.Area == nil || c.Area.UUID != "area-1" {
					t.Fatalf("expected new area assignment to area-1, got %#v", c.Area)
				}
				if c.OldArea != nil {
					t.Fatalf("expected no old area, got %#v", c.OldArea)
				}
			}
		}

		if !gotProject {
			t.Fatal("expected TaskAssignedToProject change")
		}
		if !gotArea {
			t.Fatal("expected TaskAssignedToArea change")
		}
	})

	t.Run("clearing project and area emits removal changes", func(t *testing.T) {
		project := &things.Task{UUID: "project-2", Title: "Project 2", Type: things.TaskTypeProject}
		if err := syncer.saveTask(project); err != nil {
			t.Fatalf("saveTask(project) failed: %v", err)
		}
		area := &things.Area{UUID: "area-2", Title: "Personal"}
		if err := syncer.saveArea(area); err != nil {
			t.Fatalf("saveArea failed: %v", err)
		}
		task := &things.Task{
			UUID:          "task-clear",
			Title:         "Task Clear",
			Type:          things.TaskTypeTask,
			ParentTaskIDs: []string{"project-2"},
			AreaIDs:       []string{"area-2"},
		}
		if err := syncer.saveTask(task); err != nil {
			t.Fatalf("saveTask(task) failed: %v", err)
		}

		parentIDs := []string{}
		areaIDs := []string{}
		payload := things.TaskActionItemPayload{
			ParentTaskIDs: &parentIDs,
			AreaIDs:       &areaIDs,
		}
		payloadBytes, _ := json.Marshal(payload)
		item := things.Item{
			UUID:   "task-clear",
			Kind:   things.ItemKindTask,
			Action: things.ItemActionModified,
			P:      payloadBytes,
		}

		changes, err := syncer.processItems([]things.Item{item}, 21)
		if err != nil {
			t.Fatalf("processItems failed: %v", err)
		}

		var gotProject, gotArea bool
		for _, change := range changes {
			switch c := change.(type) {
			case TaskAssignedToProject:
				gotProject = true
				if c.Project != nil {
					t.Fatalf("expected project removal, got new project %#v", c.Project)
				}
				if c.OldProject == nil || c.OldProject.UUID != "project-2" {
					t.Fatalf("expected old project project-2, got %#v", c.OldProject)
				}
			case TaskAssignedToArea:
				gotArea = true
				if c.Area != nil {
					t.Fatalf("expected area removal, got new area %#v", c.Area)
				}
				if c.OldArea == nil || c.OldArea.UUID != "area-2" {
					t.Fatalf("expected old area area-2, got %#v", c.OldArea)
				}
			}
		}

		if !gotProject {
			t.Fatal("expected TaskAssignedToProject change")
		}
		if !gotArea {
			t.Fatal("expected TaskAssignedToArea change")
		}
	})

	t.Run("assigning to parent task does not emit project assignment", func(t *testing.T) {
		parent := &things.Task{UUID: "parent-task", Title: "Parent", Type: things.TaskTypeTask}
		if err := syncer.saveTask(parent); err != nil {
			t.Fatalf("saveTask(parent) failed: %v", err)
		}
		child := &things.Task{UUID: "child-task", Title: "Child", Type: things.TaskTypeTask}
		if err := syncer.saveTask(child); err != nil {
			t.Fatalf("saveTask(child) failed: %v", err)
		}

		parentIDs := []string{"parent-task"}
		payload := things.TaskActionItemPayload{ParentTaskIDs: &parentIDs}
		payloadBytes, _ := json.Marshal(payload)
		item := things.Item{
			UUID:   "child-task",
			Kind:   things.ItemKindTask,
			Action: things.ItemActionModified,
			P:      payloadBytes,
		}

		changes, err := syncer.processItems([]things.Item{item}, 22)
		if err != nil {
			t.Fatalf("processItems failed: %v", err)
		}

		for _, change := range changes {
			if _, ok := change.(TaskAssignedToProject); ok {
				t.Fatalf("did not expect TaskAssignedToProject for subtask assignment: %#v", change)
			}
		}
	})
}

func TestStateQueries(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	syncer, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer syncer.Close()

	// Create test data directly
	completedAtRecent := time.Date(2026, 3, 5, 10, 30, 0, 0, time.UTC)
	completedAtOlder := time.Date(2026, 3, 4, 8, 15, 0, 0, time.UTC)
	todayRef := time.Now().UTC()
	futureRef := time.Now().UTC().Add(48 * time.Hour)
	pastRef := time.Now().UTC().Add(-48 * time.Hour)
	syncer.saveTask(&things.Task{UUID: "inbox-1", Title: "Inbox Task", Schedule: things.TaskScheduleInbox, Status: things.TaskStatusPending})
	syncer.saveTask(&things.Task{UUID: "anytime-1", Title: "Anytime Task", Schedule: things.TaskScheduleAnytime, Status: things.TaskStatusPending})
	syncer.saveTask(&things.Task{UUID: "today-ref-1", Title: "Today via tir", Schedule: things.TaskScheduleAnytime, Status: things.TaskStatusPending, TodayIndexReference: &todayRef})
	syncer.saveTask(&things.Task{UUID: "someday-1", Title: "Someday Task", Schedule: things.TaskScheduleSomeday, Status: things.TaskStatusPending})
	syncer.saveTask(&things.Task{UUID: "someday-past-1", Title: "Someday Past Task", Schedule: things.TaskScheduleSomeday, Status: things.TaskStatusPending, ScheduledDate: &pastRef})
	syncer.saveTask(&things.Task{UUID: "upcoming-1", Title: "Upcoming Task", Schedule: things.TaskScheduleSomeday, Status: things.TaskStatusPending, ScheduledDate: &futureRef})
	syncer.saveTask(&things.Task{UUID: "upcoming-ref-1", Title: "Upcoming via tir", Schedule: things.TaskScheduleSomeday, Status: things.TaskStatusPending, TodayIndexReference: &futureRef})
	syncer.saveTask(&things.Task{UUID: "completed-1", Title: "Completed Task", Schedule: things.TaskScheduleAnytime, Status: things.TaskStatusCompleted, CompletionDate: &completedAtRecent})
	syncer.saveTask(&things.Task{UUID: "completed-2", Title: "Older Completed Task", Schedule: things.TaskScheduleAnytime, Status: things.TaskStatusCompleted, CompletionDate: &completedAtOlder})
	syncer.saveTask(&things.Task{UUID: "trashed-1", Title: "Trashed Task", InTrash: true})
	syncer.saveTask(&things.Task{UUID: "project-1", Title: "Test Project", Type: things.TaskTypeProject})

	state := syncer.State()

	t.Run("TasksInInbox excludes completed by default", func(t *testing.T) {
		tasks, err := state.TasksInInbox(QueryOpts{})
		if err != nil {
			t.Fatalf("TasksInInbox failed: %v", err)
		}
		if len(tasks) != 1 {
			t.Errorf("expected 1 inbox task, got %d", len(tasks))
		}
	})

	t.Run("AllTasks excludes trashed by default", func(t *testing.T) {
		tasks, err := state.AllTasks(QueryOpts{})
		if err != nil {
			t.Fatalf("AllTasks failed: %v", err)
		}
		for _, task := range tasks {
			if task.InTrash {
				t.Error("trashed task should be excluded")
			}
		}
	})

	t.Run("AllTasks includes trashed when requested", func(t *testing.T) {
		tasks, err := state.AllTasks(QueryOpts{IncludeTrashed: true})
		if err != nil {
			t.Fatalf("AllTasks failed: %v", err)
		}
		found := false
		for _, task := range tasks {
			if task.UUID == "trashed-1" {
				found = true
				break
			}
		}
		if !found {
			t.Error("trashed task should be included")
		}
	})

	t.Run("AllProjects returns only projects", func(t *testing.T) {
		projects, err := state.AllProjects(QueryOpts{})
		if err != nil {
			t.Fatalf("AllProjects failed: %v", err)
		}
		if len(projects) != 1 {
			t.Errorf("expected 1 project, got %d", len(projects))
		}
		if projects[0].Type != things.TaskTypeProject {
			t.Error("returned task is not a project")
		}
	})

	t.Run("TasksInToday includes tir-only tasks", func(t *testing.T) {
		tasks, err := state.TasksInToday(QueryOpts{})
		if err != nil {
			t.Fatalf("TasksInToday failed: %v", err)
		}
		if len(tasks) != 1 {
			t.Fatalf("expected 1 today task, got %d", len(tasks))
		}
		if tasks[0].UUID != "today-ref-1" {
			t.Fatalf("expected today-ref-1, got %s", tasks[0].UUID)
		}
	})

	t.Run("TasksInAnytime returns undated anytime tasks", func(t *testing.T) {
		tasks, err := state.TasksInAnytime(QueryOpts{})
		if err != nil {
			t.Fatalf("TasksInAnytime failed: %v", err)
		}
		if len(tasks) != 1 {
			t.Fatalf("expected 1 anytime task, got %d", len(tasks))
		}
		if tasks[0].UUID != "anytime-1" {
			t.Fatalf("expected anytime-1, got %s", tasks[0].UUID)
		}
	})

	t.Run("TasksInSomeday returns deferred tasks that are not upcoming", func(t *testing.T) {
		tasks, err := state.TasksInSomeday(QueryOpts{})
		if err != nil {
			t.Fatalf("TasksInSomeday failed: %v", err)
		}
		if len(tasks) != 2 {
			t.Fatalf("expected 2 someday tasks, got %d", len(tasks))
		}
		if tasks[0].UUID != "someday-1" || tasks[1].UUID != "someday-past-1" {
			t.Fatalf("unexpected someday tasks: %s, %s", tasks[0].UUID, tasks[1].UUID)
		}
	})

	t.Run("TasksInUpcoming returns deferred future-dated tasks", func(t *testing.T) {
		tasks, err := state.TasksInUpcoming(QueryOpts{})
		if err != nil {
			t.Fatalf("TasksInUpcoming failed: %v", err)
		}
		if len(tasks) != 2 {
			t.Fatalf("expected 2 upcoming tasks, got %d", len(tasks))
		}
		if tasks[0].UUID != "upcoming-1" || tasks[1].UUID != "upcoming-ref-1" {
			t.Fatalf("unexpected upcoming tasks: %s, %s", tasks[0].UUID, tasks[1].UUID)
		}
	})

	t.Run("CompletedTasksInRange filters and orders by completion date", func(t *testing.T) {
		after := time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)
		before := time.Date(2026, 3, 6, 0, 0, 0, 0, time.UTC)

		tasks, err := state.CompletedTasksInRange(10, &after, &before)
		if err != nil {
			t.Fatalf("CompletedTasksInRange failed: %v", err)
		}
		if len(tasks) != 1 {
			t.Fatalf("expected 1 task in date range, got %d", len(tasks))
		}
		if tasks[0].UUID != "completed-1" {
			t.Fatalf("expected completed-1, got %s", tasks[0].UUID)
		}

		allCompleted, err := state.CompletedTasksInRange(10, nil, nil)
		if err != nil {
			t.Fatalf("CompletedTasksInRange(all) failed: %v", err)
		}
		if len(allCompleted) < 2 {
			t.Fatalf("expected at least 2 completed tasks, got %d", len(allCompleted))
		}
		if allCompleted[0].UUID != "completed-1" || allCompleted[1].UUID != "completed-2" {
			t.Fatalf("expected completed tasks ordered by completion date desc, got %s then %s", allCompleted[0].UUID, allCompleted[1].UUID)
		}
	})
}

func TestStateQueryPagination(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	syncer, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer syncer.Close()

	for i := 1; i <= 3; i++ {
		if err := syncer.saveTask(&things.Task{
			UUID:     fmt.Sprintf("task-%d", i),
			Title:    fmt.Sprintf("Task %d", i),
			Type:     things.TaskTypeTask,
			Status:   things.TaskStatusPending,
			Schedule: things.TaskScheduleAnytime,
			Index:    i,
		}); err != nil {
			t.Fatalf("saveTask %d failed: %v", i, err)
		}
	}
	if _, err := syncer.db.Exec(`INSERT INTO areas (uuid, title, "index") VALUES ('area-1', 'Area 1', 1), ('area-2', 'Area 2', 2), ('area-3', 'Area 3', 3)`); err != nil {
		t.Fatalf("insert areas failed: %v", err)
	}
	if _, err := syncer.db.Exec(`INSERT INTO tags (uuid, title, shortcut, "index") VALUES ('tag-1', 'Tag 1', '', 1), ('tag-2', 'Tag 2', '', 2), ('tag-3', 'Tag 3', '', 3)`); err != nil {
		t.Fatalf("insert tags failed: %v", err)
	}

	state := syncer.State()

	t.Run("AllTasks paginates with limit and offset", func(t *testing.T) {
		tasks, err := state.AllTasks(QueryOpts{Limit: 2, Offset: 1})
		if err != nil {
			t.Fatalf("AllTasks failed: %v", err)
		}
		if len(tasks) != 2 {
			t.Fatalf("expected 2 tasks, got %d", len(tasks))
		}
		if tasks[0].UUID != "task-2" || tasks[1].UUID != "task-3" {
			t.Fatalf("unexpected tasks: %s, %s", tasks[0].UUID, tasks[1].UUID)
		}
	})

	t.Run("AllAreas paginates with limit and offset", func(t *testing.T) {
		areas, err := state.AllAreasWithOpts(QueryOpts{Limit: 1, Offset: 1})
		if err != nil {
			t.Fatalf("AllAreasWithOpts failed: %v", err)
		}
		if len(areas) != 1 || areas[0].UUID != "area-2" {
			t.Fatalf("unexpected areas: %#v", areas)
		}
	})

	t.Run("AllTags paginates with offset only", func(t *testing.T) {
		tags, err := state.AllTagsWithOpts(QueryOpts{Offset: 2})
		if err != nil {
			t.Fatalf("AllTagsWithOpts failed: %v", err)
		}
		if len(tags) != 1 || tags[0].UUID != "tag-3" {
			t.Fatalf("unexpected tags: %#v", tags)
		}
	})
}

func TestProcessTaskClearsNullableDates(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	syncer, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer syncer.Close()

	now := time.Now().UTC().Truncate(time.Second)
	scheduled := now.Add(24 * time.Hour)
	deadline := now.Add(48 * time.Hour)
	completion := now.Add(72 * time.Hour)

	if err := syncer.saveTask(&things.Task{
		UUID:                "task-clear-dates",
		Title:               "Task",
		Schedule:            things.TaskScheduleAnytime,
		Type:                things.TaskTypeTask,
		CreationDate:        now,
		ScheduledDate:       &scheduled,
		TodayIndexReference: &scheduled,
		DeadlineDate:        &deadline,
		CompletionDate:      &completion,
	}); err != nil {
		t.Fatalf("saveTask failed: %v", err)
	}

	item := things.Item{
		UUID:   "task-clear-dates",
		Kind:   things.ItemKindTask,
		Action: things.ItemActionModified,
		P:      json.RawMessage(`{"sr":null,"tir":null,"dd":null,"sp":null}`),
	}

	if _, err := syncer.processItems([]things.Item{item}, 1); err != nil {
		t.Fatalf("processItems failed: %v", err)
	}

	task, err := syncer.State().Task("task-clear-dates")
	if err != nil {
		t.Fatalf("Task lookup failed: %v", err)
	}
	if task == nil {
		t.Fatal("task not found")
	}
	if task.ScheduledDate != nil {
		t.Error("expected ScheduledDate to be cleared")
	}
	if task.TodayIndexReference != nil {
		t.Error("expected TodayIndexReference to be cleared")
	}
	if task.DeadlineDate != nil {
		t.Error("expected DeadlineDate to be cleared")
	}
	if task.CompletionDate != nil {
		t.Error("expected CompletionDate to be cleared")
	}
}

func TestProcessTaskClearsStaleTodayIndexReference(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	syncer, err := Open(dbPath, nil)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer syncer.Close()

	now := time.Now().UTC().Truncate(time.Second)
	today := now
	future := now.Add(48 * time.Hour)

	t.Run("scheduled date update without tir clears stale today reference", func(t *testing.T) {
		taskUUID := "task-clear-stale-tir-sr"
		if err := syncer.saveTask(&things.Task{
			UUID:                taskUUID,
			Title:               "Task",
			Schedule:            things.TaskScheduleAnytime,
			Type:                things.TaskTypeTask,
			Status:              things.TaskStatusPending,
			CreationDate:        now,
			TodayIndexReference: &today,
		}); err != nil {
			t.Fatalf("saveTask failed: %v", err)
		}

		futureTS := things.Timestamp(future)
		payload := things.TaskActionItemPayload{ScheduledDate: &futureTS}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}

		item := things.Item{
			UUID:   taskUUID,
			Kind:   things.ItemKindTask,
			Action: things.ItemActionModified,
			P:      payloadBytes,
		}

		if _, err := syncer.processItems([]things.Item{item}, 1); err != nil {
			t.Fatalf("processItems failed: %v", err)
		}

		task, err := syncer.State().Task(taskUUID)
		if err != nil {
			t.Fatalf("Task lookup failed: %v", err)
		}
		if task == nil {
			t.Fatal("task not found")
		}
		if task.TodayIndexReference != nil {
			t.Fatal("expected TodayIndexReference to be cleared")
		}

		todayTasks, err := syncer.State().TasksInToday(QueryOpts{})
		if err != nil {
			t.Fatalf("TasksInToday failed: %v", err)
		}
		for _, todayTask := range todayTasks {
			if todayTask.UUID == taskUUID {
				t.Fatal("task should not remain in Today after sr-only reschedule")
			}
		}
	})

	t.Run("schedule update without tir clears stale today reference", func(t *testing.T) {
		taskUUID := "task-clear-stale-tir-schedule"
		if err := syncer.saveTask(&things.Task{
			UUID:                taskUUID,
			Title:               "Task",
			Schedule:            things.TaskScheduleSomeday,
			Type:                things.TaskTypeTask,
			Status:              things.TaskStatusPending,
			CreationDate:        now,
			TodayIndexReference: &today,
		}); err != nil {
			t.Fatalf("saveTask failed: %v", err)
		}

		schedule := things.TaskScheduleAnytime
		payload := things.TaskActionItemPayload{Schedule: &schedule}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}

		item := things.Item{
			UUID:   taskUUID,
			Kind:   things.ItemKindTask,
			Action: things.ItemActionModified,
			P:      payloadBytes,
		}

		if _, err := syncer.processItems([]things.Item{item}, 2); err != nil {
			t.Fatalf("processItems failed: %v", err)
		}

		task, err := syncer.State().Task(taskUUID)
		if err != nil {
			t.Fatalf("Task lookup failed: %v", err)
		}
		if task == nil {
			t.Fatal("task not found")
		}
		if task.TodayIndexReference != nil {
			t.Fatal("expected TodayIndexReference to be cleared")
		}
		if got := taskLocation(task); got != LocationAnytime {
			t.Fatalf("expected task location %v, got %v", LocationAnytime, got)
		}
	})
}

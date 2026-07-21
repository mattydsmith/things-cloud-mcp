package sync

import (
	"database/sql"
	"fmt"
	"time"

	things "github.com/arthursoares/things-cloud-sdk"
)

// State provides read-only access to the synced Things state
type State struct {
	syncer *Syncer
}

// State returns a read-only view of the current aggregated state
func (s *Syncer) State() *State {
	return &State{syncer: s}
}

func (st *State) executor() dbExecutor {
	return st.syncer.rawDB
}

// QueryOpts controls filtering for state queries
type QueryOpts struct {
	IncludeCompleted bool
	IncludeTrashed   bool
	Limit            int
	Offset           int
}

// doneStatusClause returns the predicate excluding tasks the user is finished
// with. Things treats canceled and completed as distinct statuses but hides
// both from its views, so filtering on completion alone leaks canceled tasks.
func doneStatusClause(opts QueryOpts) string {
	if opts.IncludeCompleted {
		return ""
	}
	return fmt.Sprintf(" AND status NOT IN (%d, %d)",
		things.TaskStatusCanceled, things.TaskStatusCompleted)
}

// Task retrieves a task by UUID
func (st *State) Task(uuid string) (*things.Task, error) {
	st.syncer.mu.RLock()
	defer st.syncer.mu.RUnlock()
	return st.syncer.readSyncer().getTask(uuid)
}

// Area retrieves an area by UUID
func (st *State) Area(uuid string) (*things.Area, error) {
	st.syncer.mu.RLock()
	defer st.syncer.mu.RUnlock()
	return st.syncer.readSyncer().getArea(uuid)
}

// Tag retrieves a tag by UUID
func (st *State) Tag(uuid string) (*things.Tag, error) {
	st.syncer.mu.RLock()
	defer st.syncer.mu.RUnlock()
	return st.syncer.readSyncer().getTag(uuid)
}

// AllTasks returns all tasks matching the query options
func (st *State) AllTasks(opts QueryOpts) ([]*things.Task, error) {
	query := `SELECT uuid FROM tasks WHERE type = 0 AND deleted = 0`
	args := []any{}
	query += doneStatusClause(opts)
	if !opts.IncludeTrashed {
		query += " AND in_trash = 0"
	}
	query += ` ORDER BY "index"`
	query, args = paginateQuery(query, args, opts)
	return st.queryTasks(query, args...)
}

// AllProjects returns all projects
func (st *State) AllProjects(opts QueryOpts) ([]*things.Task, error) {
	query := `SELECT uuid FROM tasks WHERE type = 1 AND deleted = 0`
	args := []any{}
	query += doneStatusClause(opts)
	if !opts.IncludeTrashed {
		query += " AND in_trash = 0"
	}
	query += ` ORDER BY "index"`
	query, args = paginateQuery(query, args, opts)
	return st.queryTasks(query, args...)
}

// AllAreas returns all areas
func (st *State) AllAreas() ([]*things.Area, error) {
	return st.AllAreasWithOpts(QueryOpts{})
}

// AllAreasWithOpts returns areas with optional pagination.
func (st *State) AllAreasWithOpts(opts QueryOpts) ([]*things.Area, error) {
	st.syncer.mu.RLock()
	defer st.syncer.mu.RUnlock()

	query := `SELECT uuid, title FROM areas WHERE deleted = 0 ORDER BY "index"`
	args := []any{}
	query, args = paginateQuery(query, args, opts)

	rows, err := st.executor().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var areas []*things.Area
	for rows.Next() {
		var a things.Area
		if err := rows.Scan(&a.UUID, &a.Title); err != nil {
			return nil, err
		}
		areas = append(areas, &a)
	}
	return areas, nil
}

// AllTags returns all tags
func (st *State) AllTags() ([]*things.Tag, error) {
	return st.AllTagsWithOpts(QueryOpts{})
}

// AllTagsWithOpts returns tags with optional pagination.
func (st *State) AllTagsWithOpts(opts QueryOpts) ([]*things.Tag, error) {
	st.syncer.mu.RLock()
	defer st.syncer.mu.RUnlock()

	query := `SELECT uuid, title, shortcut FROM tags WHERE deleted = 0 ORDER BY "index"`
	args := []any{}
	query, args = paginateQuery(query, args, opts)

	rows, err := st.executor().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []*things.Tag
	for rows.Next() {
		var t things.Tag
		if err := rows.Scan(&t.UUID, &t.Title, &t.ShortHand); err != nil {
			return nil, err
		}
		tags = append(tags, &t)
	}
	return tags, nil
}

// TasksInInbox returns tasks in the Inbox
func (st *State) TasksInInbox(opts QueryOpts) ([]*things.Task, error) {
	query := `SELECT uuid FROM tasks WHERE type = 0 AND schedule = 0 AND deleted = 0`
	args := []any{}
	query += doneStatusClause(opts)
	if !opts.IncludeTrashed {
		query += " AND in_trash = 0"
	}
	query += ` ORDER BY "index"`
	query, args = paginateQuery(query, args, opts)
	return st.queryTasks(query, args...)
}

// TasksInToday returns tasks in the Today view. A task appears in Today when
// schedule=1 (started/anytime) AND either sr (scheduled_date) or tir
// (today_index_ref) falls on or before today's date. Things rolls overdue
// tasks forward into Today rather than hiding them, so the bound is open at
// the start: matching only today's date would drop every rolled-over task.
func (st *State) TasksInToday(opts QueryOpts) ([]*things.Task, error) {
	_, tomorrowUnix := currentUTCDayBounds()

	query := `SELECT uuid FROM tasks WHERE type = 0 AND schedule = 1
		AND (
			(scheduled_date IS NOT NULL AND scheduled_date < ?)
			OR (today_index_ref IS NOT NULL AND today_index_ref < ?)
		) AND deleted = 0`
	args := []any{tomorrowUnix, tomorrowUnix}
	query += doneStatusClause(opts)
	if !opts.IncludeTrashed {
		query += " AND in_trash = 0"
	}
	query += ` ORDER BY today_index, "index"`
	query, args = paginateQuery(query, args, opts)

	st.syncer.mu.RLock()
	defer st.syncer.mu.RUnlock()

	rows, err := st.executor().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return st.scanTaskUUIDs(rows)
}

// TasksInAnytime returns tasks in the Anytime view. A task appears in Anytime
// when schedule=1 and it is not classified into Today for the current UTC day.
// This is the exact complement of TasksInToday and must stay in step with it,
// or a rolled-over task shows up in both views at once.
func (st *State) TasksInAnytime(opts QueryOpts) ([]*things.Task, error) {
	_, tomorrowUnix := currentUTCDayBounds()

	query := `SELECT uuid FROM tasks WHERE type = 0 AND schedule = 1
		AND NOT (
			(scheduled_date IS NOT NULL AND scheduled_date < ?)
			OR (today_index_ref IS NOT NULL AND today_index_ref < ?)
		) AND deleted = 0`
	args := []any{tomorrowUnix, tomorrowUnix}
	query += doneStatusClause(opts)
	if !opts.IncludeTrashed {
		query += " AND in_trash = 0"
	}
	query += ` ORDER BY "index"`
	query, args = paginateQuery(query, args, opts)
	return st.queryTasks(query, args...)
}

// TasksInSomeday returns tasks in the Someday view. A task appears in Someday
// when schedule=2 and it is not classified into Upcoming at the current time.
func (st *State) TasksInSomeday(opts QueryOpts) ([]*things.Task, error) {
	nowUnix := currentUTCUnix()

	query := `SELECT uuid FROM tasks WHERE type = 0 AND schedule = 2
		AND NOT (
			(scheduled_date IS NOT NULL AND scheduled_date > ?)
			OR (today_index_ref IS NOT NULL AND today_index_ref > ?)
		) AND deleted = 0`
	args := []any{nowUnix, nowUnix}
	query += doneStatusClause(opts)
	if !opts.IncludeTrashed {
		query += " AND in_trash = 0"
	}
	query += ` ORDER BY "index"`
	query, args = paginateQuery(query, args, opts)
	return st.queryTasks(query, args...)
}

// TasksInUpcoming returns tasks in the Upcoming view. A task appears in
// Upcoming when schedule=2 and either sr (scheduled_date) or tir
// (today_index_ref) is in the future.
func (st *State) TasksInUpcoming(opts QueryOpts) ([]*things.Task, error) {
	nowUnix := currentUTCUnix()

	query := `SELECT uuid FROM tasks WHERE type = 0 AND schedule = 2
		AND (
			(scheduled_date IS NOT NULL AND scheduled_date > ?)
			OR (today_index_ref IS NOT NULL AND today_index_ref > ?)
		) AND deleted = 0`
	args := []any{nowUnix, nowUnix}
	query += doneStatusClause(opts)
	if !opts.IncludeTrashed {
		query += " AND in_trash = 0"
	}
	query += ` ORDER BY COALESCE(scheduled_date, today_index_ref), "index"`
	query, args = paginateQuery(query, args, opts)
	return st.queryTasks(query, args...)
}

// TasksInProject returns tasks belonging to a project
func (st *State) TasksInProject(projectUUID string, opts QueryOpts) ([]*things.Task, error) {
	query := `SELECT uuid FROM tasks WHERE type = 0 AND project_uuid = ? AND deleted = 0`
	args := []any{projectUUID}
	query += doneStatusClause(opts)
	if !opts.IncludeTrashed {
		query += " AND in_trash = 0"
	}
	query += ` ORDER BY "index"`
	query, args = paginateQuery(query, args, opts)

	st.syncer.mu.RLock()
	defer st.syncer.mu.RUnlock()

	rows, err := st.executor().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return st.scanTaskUUIDs(rows)
}

// TasksInArea returns tasks belonging to an area
func (st *State) TasksInArea(areaUUID string, opts QueryOpts) ([]*things.Task, error) {
	query := `SELECT uuid FROM tasks WHERE type = 0 AND area_uuid = ? AND deleted = 0`
	args := []any{areaUUID}
	query += doneStatusClause(opts)
	if !opts.IncludeTrashed {
		query += " AND in_trash = 0"
	}
	query += ` ORDER BY "index"`
	query, args = paginateQuery(query, args, opts)

	st.syncer.mu.RLock()
	defer st.syncer.mu.RUnlock()

	rows, err := st.executor().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return st.scanTaskUUIDs(rows)
}

// CompletedTasks returns completed tasks, ordered by completion date (most recent first)
func (st *State) CompletedTasks(limit int) ([]*things.Task, error) {
	return st.CompletedTasksInRange(limit, nil, nil)
}

// CompletedTasksInRange returns completed tasks in an optional completion-date window.
// completedAfter is inclusive, completedBefore is exclusive.
func (st *State) CompletedTasksInRange(limit int, completedAfter, completedBefore *time.Time) ([]*things.Task, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `SELECT uuid FROM tasks WHERE type = 0 AND status = 3 AND deleted = 0 AND in_trash = 0
		AND completion_date IS NOT NULL`
	args := []any{}
	if completedAfter != nil {
		query += " AND completion_date >= ?"
		args = append(args, completedAfter.Unix())
	}
	if completedBefore != nil {
		query += " AND completion_date < ?"
		args = append(args, completedBefore.Unix())
	}
	query += ` ORDER BY completion_date DESC LIMIT ?`
	args = append(args, limit)

	st.syncer.mu.RLock()
	defer st.syncer.mu.RUnlock()

	rows, err := st.executor().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return st.scanTaskUUIDs(rows)
}

// ChecklistItems returns checklist items for a task
func (st *State) ChecklistItems(taskUUID string) ([]*things.CheckListItem, error) {
	st.syncer.mu.RLock()
	defer st.syncer.mu.RUnlock()

	rows, err := st.executor().Query(`
			SELECT uuid, title, status, "index"
			FROM checklist_items
			WHERE task_uuid = ? AND deleted = 0
		ORDER BY "index"
	`, taskUUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*things.CheckListItem
	for rows.Next() {
		var c things.CheckListItem
		var status int
		if err := rows.Scan(&c.UUID, &c.Title, &status, &c.Index); err != nil {
			return nil, err
		}
		c.Status = things.TaskStatus(status)
		c.TaskIDs = []string{taskUUID}
		items = append(items, &c)
	}
	return items, nil
}

// Helper methods

func currentUTCDayBounds() (int64, int64) {
	nowUTC := time.Now().UTC()
	today := time.Date(nowUTC.Year(), nowUTC.Month(), nowUTC.Day(), 0, 0, 0, 0, time.UTC)
	tomorrow := today.Add(24 * time.Hour)
	return today.Unix(), tomorrow.Unix()
}

func currentUTCUnix() int64 {
	return time.Now().UTC().Unix()
}

func paginateQuery(query string, args []any, opts QueryOpts) (string, []any) {
	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
		if opts.Offset > 0 {
			query += " OFFSET ?"
			args = append(args, opts.Offset)
		}
		return query, args
	}
	if opts.Offset > 0 {
		query += " LIMIT -1 OFFSET ?"
		args = append(args, opts.Offset)
	}
	return query, args
}

func (st *State) queryTasks(query string, args ...any) ([]*things.Task, error) {
	st.syncer.mu.RLock()
	defer st.syncer.mu.RUnlock()

	rows, err := st.executor().Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return st.scanTaskUUIDs(rows)
}

func (st *State) scanTaskUUIDs(rows *sql.Rows) ([]*things.Task, error) {
	var tasks []*things.Task
	syncer := st.syncer.readSyncer()

	for rows.Next() {
		var uuid string
		if err := rows.Scan(&uuid); err != nil {
			return nil, err
		}
		task, err := syncer.getTask(uuid)
		if err != nil {
			return nil, err
		}
		if task != nil {
			tasks = append(tasks, task)
		}
	}
	return tasks, nil
}

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"log"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	gosync "sync"
	"time"
	_ "time/tzdata" // THINGS_TIMEZONE must resolve inside the scratch container

	thingscloud "github.com/arthursoares/things-cloud-sdk"
	"github.com/google/uuid"
)

// historyMu serializes all write operations (history.Sync + history.Write)
// to prevent concurrent LatestServerIndex races.
var historyMu gosync.Mutex

// ---------------------------------------------------------------------------
// Wire-format types (no omitempty — Things expects all fields on creates)
// ---------------------------------------------------------------------------

type wireNote struct {
	TypeTag  string `json:"_t"`
	Checksum int64  `json:"ch"`
	Value    string `json:"v"`
	Type     int    `json:"t"`
}

type wireExtension struct {
	Sn      map[string]any `json:"sn"`
	TypeTag string         `json:"_t"`
}

type writeEnvelope struct {
	id      string
	action  int
	kind    string
	payload any
}

func (w writeEnvelope) UUID() string { return w.id }

func (w writeEnvelope) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		T int    `json:"t"`
		E string `json:"e"`
		P any    `json:"p"`
	}{w.action, w.kind, w.payload})
}

type taskCreatePayload struct {
	Tp   int              `json:"tp"`
	Sr   *int64           `json:"sr"`
	Dds  *int64           `json:"dds"`
	Rt   []string         `json:"rt"`
	Rmd  *int64           `json:"rmd"`
	Ss   int              `json:"ss"`
	Tr   bool             `json:"tr"`
	Dl   []string         `json:"dl"`
	Icp  bool             `json:"icp"`
	St   int              `json:"st"`
	Ar   []string         `json:"ar"`
	Tt   string           `json:"tt"`
	Do   int              `json:"do"`
	Lai  *int64           `json:"lai"`
	Tir  *int64           `json:"tir"`
	Tg   []string         `json:"tg"`
	Agr  []string         `json:"agr"`
	Ix   int              `json:"ix"`
	Cd   float64          `json:"cd"`
	Lt   bool             `json:"lt"`
	Icc  int              `json:"icc"`
	Md   *float64         `json:"md"`
	Ti   int              `json:"ti"`
	Dd   *int64           `json:"dd"`
	Ato  *int             `json:"ato"`
	Nt   wireNote         `json:"nt"`
	Icsd *int64           `json:"icsd"`
	Pr   []string         `json:"pr"`
	Rp   *string          `json:"rp"`
	Acrd *int64           `json:"acrd"`
	Sp   *float64         `json:"sp"`
	Sb   int              `json:"sb"`
	Rr   *json.RawMessage `json:"rr"`
	Xx   wireExtension    `json:"xx"`
}

var errInvalidInput = errors.New("invalid input")

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func emptyNote() wireNote {
	return wireNote{TypeTag: "tx", Checksum: 0, Value: "", Type: 1}
}

func noteChecksum(s string) int64 {
	return int64(crc32.ChecksumIEEE([]byte(s)))
}

func textNote(s string) wireNote {
	return wireNote{TypeTag: "tx", Checksum: noteChecksum(s), Value: s, Type: 1}
}

func defaultExtension() wireExtension {
	return wireExtension{Sn: map[string]any{}, TypeTag: "oo"}
}

func invalidInputf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errInvalidInput, fmt.Sprintf(format, args...))
}

func isInvalidInput(err error) bool {
	return errors.Is(err, errInvalidInput)
}

func isBase58UUID(id string) bool {
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	// 128-bit UUIDs encode to at most 22 Base58 characters; the Things app's
	// decoder hard-crashes (Swift precondition) on anything longer, so never
	// let a longer identifier reach the wire.
	if len(id) < 20 || len(id) > 22 {
		return false
	}
	for i := 0; i < len(id); i++ {
		if !strings.ContainsRune(alphabet, rune(id[i])) {
			return false
		}
	}
	return true
}

func validateUUID(name, id string) error {
	if !isBase58UUID(id) {
		return invalidInputf("%s must be a Things Base58 UUID", name)
	}
	return nil
}

func validateOptionalUUID(name, id string) error {
	if id == "" || id == "none" {
		return nil
	}
	return validateUUID(name, id)
}

// normalizeOptionalUUID maps the "none" sentinel (accepted by
// validateOptionalUUID) to empty, so payload builders never embed the
// literal string "none" as an entity reference.
func normalizeOptionalUUID(id string) string {
	if id == "none" {
		return ""
	}
	return id
}

func parseUUIDList(name, raw string) ([]string, error) {
	if raw == "" {
		return []string{}, nil
	}
	parts := strings.Split(raw, ",")
	ids := make([]string, 0, len(parts))
	for _, part := range parts {
		id := strings.TrimSpace(part)
		if id == "" {
			continue
		}
		if err := validateUUID(name, id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func validateUUIDSlice(name string, ids []string) ([]string, error) {
	if ids == nil {
		return []string{}, nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		if err := validateUUID(name, trimmed); err != nil {
			return nil, err
		}
		out = append(out, trimmed)
	}
	return out, nil
}

func generateUUID() string {
	u := uuid.New()
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	n := new(big.Int).SetBytes(u[:])
	base := big.NewInt(58)
	mod := new(big.Int)
	var encoded []byte
	for n.Sign() > 0 {
		n.DivMod(n, base, mod)
		encoded = append(encoded, alphabet[mod.Int64()])
	}
	for i, j := 0, len(encoded)-1; i < j; i, j = i+1, j-1 {
		encoded[i], encoded[j] = encoded[j], encoded[i]
	}
	// Left-pad with '1' (zero in Base58) to the canonical 22 characters:
	// UUIDs with leading zero bytes would otherwise encode shorter than the
	// fixed length Things uses.
	s := string(encoded)
	for len(s) < 22 {
		s = "1" + s
	}
	return s
}

const defaultSyncMinInterval = 2 * time.Second

var (
	// syncThrottleMu single-flights syncs against Things Cloud and guards
	// lastSyncAt, so bursts of tool calls don't trip their rate limiting.
	syncThrottleMu gosync.Mutex
	lastSyncAt     time.Time

	// doSync performs one sync against Things Cloud. Overridable in tests.
	doSync = func() error {
		_, err := syncer.Sync()
		return err
	}
)

// syncMinInterval reads SYNC_MIN_INTERVAL (whole seconds) on each call so
// tests can vary it; invalid or unset values fall back to the default.
func syncMinInterval() time.Duration {
	raw := os.Getenv("SYNC_MIN_INTERVAL")
	if raw == "" {
		return defaultSyncMinInterval
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return defaultSyncMinInterval
	}
	return time.Duration(n) * time.Second
}

// syncForRead refreshes local state before a read, skipping the round trip
// to Things Cloud when the last successful sync is recent enough.
// Returns the error so callers can optionally surface it.
func syncForRead() error {
	syncThrottleMu.Lock()
	defer syncThrottleMu.Unlock()
	if time.Since(lastSyncAt) < syncMinInterval() {
		return nil
	}
	if err := doSync(); err != nil {
		log.Printf("[SYNC] pre-read sync failed: %v", err)
		return err
	}
	lastSyncAt = time.Now()
	return nil
}

// syncAfterWrite syncs after a write to refresh local state. It bypasses the
// throttle so the write is immediately visible to reads.
// Errors are logged but not returned (best-effort refresh).
func syncAfterWrite() {
	syncThrottleMu.Lock()
	defer syncThrottleMu.Unlock()
	if err := doSync(); err != nil {
		log.Printf("[SYNC] post-write refresh failed: %v", err)
		return
	}
	lastSyncAt = time.Now()
}

// ---------------------------------------------------------------------------
// Existence validation
//
// UUID arguments are format-checked by validateUUID, but a well-formed UUID
// can still point at nothing. Writing an update for a nonexistent entity
// appends a permanent orphan event to the Things Cloud history and reports
// false success, so every write verifies its targets against synced local
// state first.
// ---------------------------------------------------------------------------

// entityStore is the subset of *sync.State used for existence checks.
type entityStore interface {
	Task(uuid string) (*thingscloud.Task, error)
	Area(uuid string) (*thingscloud.Area, error)
	Tag(uuid string) (*thingscloud.Tag, error)
	ChecklistItem(uuid string) (*thingscloud.CheckListItem, error)
}

// validationState returns entity lookups backed by local state, refreshed
// best-effort (stale state still validates better than none). Overridable
// in tests.
var validationState = func() entityStore {
	_ = syncForRead()
	return syncer.State()
}

func requireTask(st entityStore, name, uuid string) (*thingscloud.Task, error) {
	task, err := st.Task(uuid)
	if err != nil {
		return nil, fmt.Errorf("%s lookup failed: %w", name, err)
	}
	if task == nil {
		return nil, invalidInputf("%s not found in synced state: %s", name, uuid)
	}
	return task, nil
}

func requireProject(st entityStore, name, uuid string) error {
	task, err := requireTask(st, name, uuid)
	if err != nil {
		return err
	}
	if task.Type != thingscloud.TaskTypeProject {
		return invalidInputf("%s is not a project: %s", name, uuid)
	}
	return nil
}

func requireHeading(st entityStore, name, uuid string) error {
	task, err := requireTask(st, name, uuid)
	if err != nil {
		return err
	}
	if task.Type != thingscloud.TaskTypeHeading {
		return invalidInputf("%s is not a heading: %s", name, uuid)
	}
	return nil
}

func requireArea(st entityStore, name, uuid string) error {
	area, err := st.Area(uuid)
	if err != nil {
		return fmt.Errorf("%s lookup failed: %w", name, err)
	}
	if area == nil {
		return invalidInputf("%s not found in synced state: %s", name, uuid)
	}
	return nil
}

func requireTag(st entityStore, name, uuid string) error {
	tag, err := st.Tag(uuid)
	if err != nil {
		return fmt.Errorf("%s lookup failed: %w", name, err)
	}
	if tag == nil {
		return invalidInputf("%s not found in synced state: %s", name, uuid)
	}
	return nil
}

func requireTags(st entityStore, name string, uuids []string) error {
	for _, id := range uuids {
		if err := requireTag(st, name, id); err != nil {
			return err
		}
	}
	return nil
}

func requireChecklistItem(st entityStore, uuid string) error {
	item, err := st.ChecklistItem(uuid)
	if err != nil {
		return fmt.Errorf("checklist item lookup failed: %w", err)
	}
	if item == nil {
		return invalidInputf("checklist item not found in synced state: %s", uuid)
	}
	return nil
}

func nowTs() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

// timeNow is the clock used for calendar-day resolution. Overridable in
// tests to freeze the boundary cases.
var timeNow = time.Now

// tzWarnOnce keeps an invalid THINGS_TIMEZONE from spamming the log on
// every write.
var tzWarnOnce gosync.Once

// thingsLocation returns the timezone used to resolve calendar days like
// "today". Set THINGS_TIMEZONE to an IANA name (e.g. "America/New_York") —
// the server usually runs in UTC, which is not the user's day for hours at
// a stretch. Unset or invalid values fall back to UTC.
func thingsLocation() *time.Location {
	name := os.Getenv("THINGS_TIMEZONE")
	if name == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		tzWarnOnce.Do(func() {
			log.Printf("[CONFIG] invalid THINGS_TIMEZONE %q (%v); falling back to UTC", name, err)
		})
		return time.UTC
	}
	return loc
}

// localCalendarDayUTC returns the calendar day of t as observed in loc,
// encoded as UTC midnight — the encoding Things uses for all dates.
func localCalendarDayUTC(t time.Time, loc *time.Location) time.Time {
	lt := t.In(loc)
	return time.Date(lt.Year(), lt.Month(), lt.Day(), 0, 0, 0, 0, time.UTC)
}

// resolveLocation resolves a caller-supplied IANA timezone name for one
// request. Empty means "use the server default" (THINGS_TIMEZONE, then UTC);
// an unknown name is an input error rather than a silent fallback, because a
// caller who names a timezone is trusting us to use exactly that one.
func resolveLocation(name string) (*time.Location, error) {
	if name == "" {
		return thingsLocation(), nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, invalidInputf("invalid timezone %q (use an IANA name like America/New_York)", name)
	}
	return loc, nil
}

// todayMidnightIn returns today's date — today as observed in loc — encoded
// as UTC midnight.
func todayMidnightIn(loc *time.Location) int64 {
	return localCalendarDayUTC(timeNow(), loc).Unix()
}

// ---------------------------------------------------------------------------
// Fluent update builder
// ---------------------------------------------------------------------------

type taskUpdate struct {
	fields map[string]any
}

func newTaskUpdate() *taskUpdate {
	return &taskUpdate{fields: map[string]any{
		"md": nowTs(),
	}}
}

func (u *taskUpdate) title(s string) *taskUpdate {
	u.fields["tt"] = s
	return u
}

func (u *taskUpdate) note(text string) *taskUpdate {
	u.fields["nt"] = textNote(text)
	return u
}

func (u *taskUpdate) clearNote() *taskUpdate {
	u.fields["nt"] = emptyNote()
	return u
}

func (u *taskUpdate) status(ss int) *taskUpdate {
	u.fields["ss"] = ss
	return u
}

func (u *taskUpdate) stopDate(ts float64) *taskUpdate {
	u.fields["sp"] = ts
	return u
}

func (u *taskUpdate) trash(b bool) *taskUpdate {
	u.fields["tr"] = b
	return u
}

func (u *taskUpdate) schedule(st int, sr, tir any) *taskUpdate {
	u.fields["st"] = st
	u.fields["sr"] = sr
	u.fields["tir"] = tir
	return u
}

func (u *taskUpdate) reminder(seconds int) *taskUpdate {
	u.fields["ato"] = seconds
	return u
}

func (u *taskUpdate) clearReminder() *taskUpdate {
	u.fields["ato"] = nil
	return u
}

func (u *taskUpdate) deadline(dd int64) *taskUpdate {
	u.fields["dd"] = dd
	return u
}

func (u *taskUpdate) clearDeadline() *taskUpdate {
	u.fields["dd"] = nil
	return u
}

func (u *taskUpdate) project(uuid string) *taskUpdate {
	u.fields["pr"] = []string{uuid}
	return u
}

func (u *taskUpdate) heading(uuid string) *taskUpdate {
	u.fields["agr"] = []string{uuid}
	return u
}

func (u *taskUpdate) clearHeading() *taskUpdate {
	u.fields["agr"] = []string{}
	return u
}

func (u *taskUpdate) area(uuid string) *taskUpdate {
	u.fields["ar"] = []string{uuid}
	return u
}

func (u *taskUpdate) clearArea() *taskUpdate {
	u.fields["ar"] = []string{}
	return u
}

func (u *taskUpdate) tags(uuids []string) *taskUpdate {
	u.fields["tg"] = uuids
	return u
}

func (u *taskUpdate) build() map[string]any {
	return u.fields
}

// ---------------------------------------------------------------------------
// API request types
// ---------------------------------------------------------------------------

// CreateTaskRequest is the JSON body for POST /api/tasks/create.
type CreateTaskRequest struct {
	Title      string `json:"title"`
	Note       string `json:"note,omitempty"`
	When       string `json:"when,omitempty"`        // today, anytime, someday, inbox, YYYY-MM-DD
	Deadline   string `json:"deadline,omitempty"`    // YYYY-MM-DD
	Project    string `json:"project,omitempty"`     // project UUID
	ParentTask string `json:"parent_task,omitempty"` // parent task UUID (for subtasks)
	Heading    string `json:"heading,omitempty"`     // heading UUID (agr) to place the task under
	Tags       string `json:"tags,omitempty"`        // comma-separated tag UUIDs
	Repeat     string `json:"repeat,omitempty"`      // daily, weekly, monthly, yearly, every N days/weeks/months/years, optional "until YYYY-MM-DD"
	Reminder   string `json:"reminder,omitempty"`    // HH:MM (24h); requires a dated when (today or YYYY-MM-DD)
	Timezone   string `json:"timezone,omitempty"`    // IANA name resolving "today" for this call; defaults to THINGS_TIMEZONE
}

// EditTaskRequest is the JSON body for POST /api/tasks/edit.
type EditTaskRequest struct {
	UUID       string `json:"uuid"`
	Title      string `json:"title,omitempty"`
	Note       string `json:"note,omitempty"`
	When       string `json:"when,omitempty"`
	Deadline   string `json:"deadline,omitempty"`
	Project    string `json:"project,omitempty"`
	ParentTask string `json:"parent_task,omitempty"`
	Heading    string `json:"heading,omitempty"` // heading UUID (agr), or "none" to detach
	Area       string `json:"area,omitempty"`
	Tags       string `json:"tags,omitempty"`
	Repeat     string `json:"repeat,omitempty"`   // daily, weekly, monthly, yearly, every N days/weeks/months/years, optional "until YYYY-MM-DD", none
	Reminder   string `json:"reminder,omitempty"` // HH:MM (24h) or "none" to clear; requires the task to keep a dated when
	Timezone   string `json:"timezone,omitempty"` // IANA name resolving "today" for this call; defaults to THINGS_TIMEZONE
}

// UUIDRequest is the JSON body for complete/trash endpoints.
type UUIDRequest struct {
	UUID string `json:"uuid"`
}

// ---------------------------------------------------------------------------
// Repeat rule builder
// ---------------------------------------------------------------------------

// buildRepeatRule builds a RepeaterConfiguration JSON from a repeat string.
// Formats: "daily", "weekly", "monthly", "yearly", "every N days/weeks/months/years"
// Optional end date: append "until YYYY-MM-DD".
// Append " after completion" for repeat-after-completion mode (tp=1).
// Returns nil if repeat is empty.
func buildRepeatRule(repeat string, refDate time.Time) (*json.RawMessage, error) {
	if repeat == "" || repeat == "none" {
		return nil, nil
	}

	s := strings.ToLower(strings.TrimSpace(repeat))

	afterCompletion := 0
	var endTs *int64
	for {
		changed := false

		if strings.HasSuffix(s, " after completion") {
			afterCompletion = 1
			s = strings.TrimSpace(strings.TrimSuffix(s, " after completion"))
			changed = true
		}

		if idx := strings.LastIndex(s, " until "); idx != -1 {
			dateStr := strings.TrimSpace(s[idx+len(" until "):])
			endDate, err := time.Parse("2006-01-02", dateStr)
			if err != nil {
				return nil, fmt.Errorf("invalid repeat end date: %s (use YYYY-MM-DD)", dateStr)
			}
			ts := endDate.UTC().Unix()
			endTs = &ts
			s = strings.TrimSpace(s[:idx])
			changed = true
		}

		if !changed {
			break
		}
	}
	if s == "" {
		return nil, fmt.Errorf("invalid repeat: missing base recurrence format")
	}

	var fu int64
	var fa int64 = 1

	switch s {
	case "daily", "every day":
		fu = 16
	case "weekly", "every week":
		fu = 256
	case "monthly", "every month":
		fu = 8
	case "yearly", "every year":
		fu = 4
	default:
		// Try "every N unit(s)" pattern
		var n int
		var unit string
		if _, err := fmt.Sscanf(s, "every %d %s", &n, &unit); err == nil && n > 0 {
			fa = int64(n)
			unit = strings.TrimSuffix(unit, "s")
			switch unit {
			case "day":
				fu = 16
			case "week":
				fu = 256
			case "month":
				fu = 8
			case "year":
				fu = 4
			default:
				return nil, fmt.Errorf("unknown repeat unit: %s", unit)
			}
		} else {
			return nil, fmt.Errorf("unrecognized repeat format: %s", repeat)
		}
	}

	ref := time.Date(refDate.Year(), refDate.Month(), refDate.Day(), 0, 0, 0, 0, time.UTC)
	srTs := ref.Unix()
	edTs := time.Date(thingscloud.NeverendingRepeatYear, time.January, 1, 0, 0, 0, 0, time.UTC).Unix()
	if endTs != nil {
		if *endTs < srTs {
			return nil, fmt.Errorf("repeat end date must be on or after start date")
		}
		edTs = *endTs
	}

	// Build detail config based on frequency
	var of []map[string]any
	switch fu {
	case 16: // daily
		of = []map[string]any{{"dy": 0}}
	case 256: // weekly — repeat on ref date's weekday
		of = []map[string]any{{"wd": int(ref.Weekday())}}
	case 8: // monthly — repeat on ref date's day of month (0-indexed)
		of = []map[string]any{{"dy": ref.Day() - 1}}
	case 4: // yearly — repeat on ref date's month+day (0-indexed)
		of = []map[string]any{{"dy": ref.Day() - 1, "mo": int(ref.Month()) - 1}}
	}

	config := map[string]any{
		"ia":  srTs,
		"rrv": 4,
		"tp":  afterCompletion,
		"of":  of,
		"fu":  fu,
		"sr":  srTs,
		"fa":  fa,
		"rc":  0,
		"ts":  0,
		"ed":  edTs,
	}

	b, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal repeat config: %w", err)
	}
	raw := json.RawMessage(b)
	return &raw, nil
}

// ---------------------------------------------------------------------------
// Core write operations (used by both HTTP handlers and MCP tools)
// ---------------------------------------------------------------------------

// historyWrite syncs the history to get the latest ancestor index, then writes.
// If the write still fails with 409 (race with Things app), it retries once.
func historyWrite(env writeEnvelope) error {
	historyMu.Lock()
	defer historyMu.Unlock()

	if client != nil && client.Debug {
		bs, _ := json.MarshalIndent(env, "", "  ")
		log.Printf("[WRITE] uuid=%s action=%d kind=%s payload=%s", env.id, env.action, env.kind, string(bs))
	} else {
		log.Printf("[WRITE] uuid=%s action=%d kind=%s", env.id, env.action, env.kind)
	}
	if err := history.Sync(); err != nil {
		return fmt.Errorf("history sync failed: %w", err)
	}
	err := history.Write(env)
	if isConflictError(err) {
		log.Printf("[WRITE] 409 conflict, retrying...")
		if err2 := history.Sync(); err2 != nil {
			return fmt.Errorf("history re-sync failed: %w", err2)
		}
		err = history.Write(env)
	}
	if err != nil {
		log.Printf("[WRITE] FAILED: %v", err)
		return fmt.Errorf("write failed: %w", err)
	}
	log.Printf("[WRITE] OK — new server index: %d", history.LatestServerIndex)
	return nil
}

// writeToHistory is the seam through which every mutation reaches Things
// Cloud. Overridable in tests to capture envelopes without a network.
var writeToHistory = historyWrite

func isConflictError(err error) bool {
	var statusErr *thingscloud.HTTPStatusError
	return errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusConflict
}

// parseReminder parses a 24h "HH:MM" reminder time into seconds after
// midnight of the task's scheduled day (the wire ato field).
func parseReminder(s string) (int, error) {
	t, err := time.Parse("15:04", s)
	if err != nil {
		return 0, invalidInputf("reminder must be 24h HH:MM format, got: %s", s)
	}
	return t.Hour()*3600 + t.Minute()*60, nil
}

// editKeepsDate reports whether the task will still carry a scheduled day
// after the edit — the anchor a reminder needs.
func editKeepsDate(req EditTaskRequest, task *thingscloud.Task, loc *time.Location) bool {
	if req.When == "none" {
		return false
	}
	if req.When != "" {
		_, sr, tir, ok := parseWhen(req.When, loc)
		return ok && (sr != nil || tir != nil)
	}
	return task.ScheduledDate != nil || task.TodayIndexReference != nil
}

// parseWhen interprets the when parameter. Returns (st, sr, tir, handled).
// For named values (today/anytime/someday/inbox/none) and YYYY-MM-DD dates.
// A future date goes to Upcoming (st=2), today's date goes to Today (st=1).
// Relative days resolve against the calendar day currently observed in loc.
func parseWhen(when string, loc *time.Location) (st int, sr, tir *int64, handled bool) {
	switch when {
	case "today":
		today := todayMidnightIn(loc)
		return 1, &today, &today, true
	case "anytime":
		return 1, nil, nil, true
	case "someday":
		return 2, nil, nil, true
	case "inbox":
		return 0, nil, nil, true
	case "none", "":
		return -1, nil, nil, false
	default:
		// Try parsing as YYYY-MM-DD
		if t, err := time.Parse("2006-01-02", when); err == nil {
			ts := t.UTC().Unix()
			today := todayMidnightIn(loc)
			if ts < today {
				// Past date → treat as Today
				return 1, &today, &today, true
			}
			if ts == today {
				return 1, &ts, &ts, true
			}
			// Future → Upcoming view (st=2 with date)
			return 2, &ts, nil, true
		}
		return -1, nil, nil, false
	}
}

func createTask(req CreateTaskRequest) (string, error) {
	if err := validateOptionalUUID("project", req.Project); err != nil {
		return "", err
	}
	if err := validateOptionalUUID("parent_task", req.ParentTask); err != nil {
		return "", err
	}
	if err := validateOptionalUUID("heading", req.Heading); err != nil {
		return "", err
	}
	req.Project = normalizeOptionalUUID(req.Project)
	req.ParentTask = normalizeOptionalUUID(req.ParentTask)
	req.Heading = normalizeOptionalUUID(req.Heading)
	tg, err := parseUUIDList("tags", req.Tags)
	if err != nil {
		return "", err
	}

	state := validationState()
	if req.Project != "" {
		if err := requireProject(state, "project", req.Project); err != nil {
			return "", err
		}
	}
	if req.ParentTask != "" {
		if _, err := requireTask(state, "parent_task", req.ParentTask); err != nil {
			return "", err
		}
	}
	if req.Heading != "" {
		if err := requireHeading(state, "heading", req.Heading); err != nil {
			return "", err
		}
	}
	if err := requireTags(state, "tags", tg); err != nil {
		return "", err
	}

	loc, err := resolveLocation(req.Timezone)
	if err != nil {
		return "", err
	}

	taskUUID := generateUUID()
	now := nowTs()

	var st int
	var sr, tir *int64
	var dd *int64

	if req.When != "" {
		s, r, t, ok := parseWhen(req.When, loc)
		if !ok {
			return "", invalidInputf("invalid when value: %s (use today, anytime, someday, inbox, or YYYY-MM-DD)", req.When)
		}
		st, sr, tir = s, r, t
	} else {
		st = 0 // inbox
	}

	// Repeating tasks must be triaged; Things behaves inconsistently when repeat+inbox is sent.
	if req.Repeat != "" {
		if req.When == "inbox" {
			return "", invalidInputf("repeat tasks cannot be in inbox; use when:anytime, today, someday, or YYYY-MM-DD")
		}
		if req.When == "" {
			st = 1 // default to Anytime when repeat is requested
		}
	}

	if req.Deadline != "" {
		t, err := time.Parse("2006-01-02", req.Deadline)
		if err != nil {
			return "", invalidInputf("deadline must be YYYY-MM-DD format, got: %s", req.Deadline)
		}
		ts := t.Unix()
		if ts < todayMidnightIn(loc) {
			return "", invalidInputf("deadline cannot be in the past")
		}
		dd = &ts
	}

	var ato *int
	if req.Reminder != "" && req.Reminder != "none" {
		sec, err := parseReminder(req.Reminder)
		if err != nil {
			return "", err
		}
		if sr == nil {
			return "", invalidInputf("reminder requires a scheduled date; set when to today or YYYY-MM-DD")
		}
		ato = &sec
	}

	pr := []string{}
	if req.ParentTask != "" {
		pr = []string{req.ParentTask}
	} else if req.Project != "" {
		pr = []string{req.Project}
		if req.When == "" {
			st = 1
		}
	}

	agr := []string{}
	if req.Heading != "" {
		agr = []string{req.Heading}
		// Tasks under headings are structural — never inbox unless asked.
		if req.When == "" {
			st = 1
		}
	}

	nt := emptyNote()
	if req.Note != "" {
		nt = textNote(req.Note)
	}

	// Build repeat rule if specified
	var rr *json.RawMessage
	if req.Repeat != "" {
		// Resolve the reference day in the request's timezone; sr is already
		// a UTC-midnight-encoded date, so read its components in UTC.
		refDate := timeNow().In(loc)
		if sr != nil {
			refDate = time.Unix(*sr, 0).UTC()
		}
		rr, err = buildRepeatRule(req.Repeat, refDate)
		if err != nil {
			return "", fmt.Errorf("invalid repeat: %w", err)
		}
	}

	payload := taskCreatePayload{
		Tp: 0, Sr: sr, Dds: nil, Rt: []string{}, Rmd: nil,
		Ss: 0, Tr: false, Dl: []string{}, Icp: false, St: st,
		Ar: []string{}, Tt: req.Title, Do: 0, Lai: nil, Tir: tir,
		Tg: tg, Agr: agr, Ix: 0, Cd: now, Lt: false,
		Icc: 0, Md: nil, Ti: 0, Dd: dd, Ato: ato, Nt: nt,
		Icsd: nil, Pr: pr, Rp: nil, Acrd: nil, Sp: nil,
		Sb: 0, Rr: rr, Xx: defaultExtension(),
	}

	env := writeEnvelope{id: taskUUID, action: 0, kind: "Task6", payload: payload}
	if err := writeToHistory(env); err != nil {
		return "", err
	}
	syncAfterWrite()
	return taskUUID, nil
}

func completeTask(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	if _, err := requireTask(validationState(), "uuid", uuid); err != nil {
		return err
	}
	ts := nowTs()
	u := newTaskUpdate().status(3).stopDate(ts)
	env := writeEnvelope{id: uuid, action: 1, kind: "Task6", payload: u.build()}
	if err := writeToHistory(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func cancelTask(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	if _, err := requireTask(validationState(), "uuid", uuid); err != nil {
		return err
	}
	ts := nowTs()
	u := newTaskUpdate().status(2).stopDate(ts)
	env := writeEnvelope{id: uuid, action: 1, kind: "Task6", payload: u.build()}
	if err := writeToHistory(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func trashTask(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	if _, err := requireTask(validationState(), "uuid", uuid); err != nil {
		return err
	}
	u := newTaskUpdate().trash(true)
	env := writeEnvelope{id: uuid, action: 1, kind: "Task6", payload: u.build()}
	if err := writeToHistory(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func editTask(req EditTaskRequest) error {
	if err := validateUUID("uuid", req.UUID); err != nil {
		return err
	}
	if err := validateOptionalUUID("project", req.Project); err != nil {
		return err
	}
	if err := validateOptionalUUID("parent_task", req.ParentTask); err != nil {
		return err
	}
	if err := validateOptionalUUID("heading", req.Heading); err != nil {
		return err
	}
	if err := validateOptionalUUID("area", req.Area); err != nil {
		return err
	}
	tags, err := parseUUIDList("tags", req.Tags)
	if err != nil {
		return err
	}

	st := validationState()
	task, err := requireTask(st, "uuid", req.UUID)
	if err != nil {
		return err
	}
	if req.Heading != "" && req.Heading != "none" {
		if err := requireHeading(st, "heading", req.Heading); err != nil {
			return err
		}
	}
	if req.Project != "" && req.Project != "none" {
		if err := requireProject(st, "project", req.Project); err != nil {
			return err
		}
	}
	if req.ParentTask != "" && req.ParentTask != "none" {
		if _, err := requireTask(st, "parent_task", req.ParentTask); err != nil {
			return err
		}
	}
	if req.Area != "" && req.Area != "none" {
		if err := requireArea(st, "area", req.Area); err != nil {
			return err
		}
	}
	if err := requireTags(st, "tags", tags); err != nil {
		return err
	}

	loc, err := resolveLocation(req.Timezone)
	if err != nil {
		return err
	}
	fields, err := buildEditUpdate(req, task, loc)
	if err != nil {
		return err
	}
	env := writeEnvelope{id: req.UUID, action: 1, kind: "Task6", payload: fields}
	if err := writeToHistory(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

// buildEditUpdate constructs the update payload for an edit. task is the
// task's current synced state; inputs must already be format-validated, and
// loc is the resolved calendar-day timezone for this request.
func buildEditUpdate(req EditTaskRequest, task *thingscloud.Task, loc *time.Location) (map[string]any, error) {
	u := newTaskUpdate()
	if req.Repeat != "" && req.When == "inbox" {
		return nil, invalidInputf("repeat tasks cannot be in inbox; use when:anytime, today, someday, YYYY-MM-DD, or omit when")
	}

	if req.Title != "" {
		u.title(req.Title)
	}
	if req.Note == "none" {
		u.clearNote()
	} else if req.Note != "" {
		u.note(req.Note)
	}
	if req.When == "none" {
		u.fields["sr"] = nil
		u.fields["tir"] = nil
		// A reminder can't outlive its scheduled day.
		u.clearReminder()
	} else if req.When != "" {
		st, sr, tir, ok := parseWhen(req.When, loc)
		if !ok {
			return nil, invalidInputf("invalid when value: %s (use today, anytime, someday, inbox, none, or YYYY-MM-DD)", req.When)
		}
		u.schedule(st, sr, tir)
		if sr == nil && tir == nil {
			// Undated schedules (anytime/someday/inbox) drop any reminder.
			u.clearReminder()
		}
	}
	if req.Reminder == "none" {
		u.clearReminder()
	} else if req.Reminder != "" {
		sec, err := parseReminder(req.Reminder)
		if err != nil {
			return nil, err
		}
		if !editKeepsDate(req, task, loc) {
			return nil, invalidInputf("reminder requires a scheduled date; set when to today or YYYY-MM-DD")
		}
		u.reminder(sec)
	}
	if req.Deadline == "none" {
		u.clearDeadline()
	} else if req.Deadline != "" {
		t, err := time.Parse("2006-01-02", req.Deadline)
		if err != nil {
			return nil, invalidInputf("deadline must be YYYY-MM-DD format, got: %s", req.Deadline)
		}
		if t.Unix() < todayMidnightIn(loc) {
			return nil, invalidInputf("deadline cannot be in the past")
		}
		u.deadline(t.Unix())
	}
	assigningParent := req.ParentTask != "" && req.ParentTask != "none"
	assigningProject := req.Project != "" && req.Project != "none"
	switch {
	case req.ParentTask == "none" || (req.Project == "none" && req.ParentTask == ""):
		u.fields["pr"] = []string{}
	case assigningParent:
		u.project(req.ParentTask)
	case assigningProject:
		u.project(req.Project)
	}
	// Moving into a project/parent only forces a reschedule when the task is
	// leaving the inbox (inbox tasks can't live in projects); a task that
	// already has a date keeps it.
	if (assigningParent || assigningProject) && req.When == "" && task.Schedule == thingscloud.TaskScheduleInbox {
		u.schedule(1, nil, nil)
	}
	if req.Heading == "none" {
		u.clearHeading()
	} else if req.Heading != "" {
		u.heading(req.Heading)
		// Headed tasks are structural — leave the inbox unless when says otherwise.
		if req.When == "" && task.Schedule == thingscloud.TaskScheduleInbox {
			u.schedule(1, nil, nil)
		}
	}
	if req.Tags != "" {
		tags, err := parseUUIDList("tags", req.Tags)
		if err != nil {
			return nil, err
		}
		u.tags(tags)
	}
	if req.Area == "none" {
		u.clearArea()
	} else if req.Area != "" {
		u.area(req.Area)
	}
	if req.Repeat == "none" {
		u.fields["rr"] = nil
	} else if req.Repeat != "" {
		// If no new "when" is provided and the task lives in Inbox, move it to
		// Anytime to avoid an inconsistent repeat+inbox combination in Things.
		if req.When == "" && task.Schedule == thingscloud.TaskScheduleInbox {
			u.schedule(1, nil, nil)
		}

		rr, err := buildRepeatRule(req.Repeat, timeNow().In(loc))
		if err != nil {
			return nil, fmt.Errorf("invalid repeat: %w", err)
		}
		u.fields["rr"] = rr
	}
	return u.build(), nil
}

func moveTaskToToday(uuid, tzName string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	loc, err := resolveLocation(tzName)
	if err != nil {
		return err
	}
	if _, err := requireTask(validationState(), "uuid", uuid); err != nil {
		return err
	}
	today := todayMidnightIn(loc)
	u := newTaskUpdate().schedule(1, today, today)
	env := writeEnvelope{id: uuid, action: 1, kind: "Task6", payload: u.build()}
	if err := writeToHistory(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func moveTaskToAnytime(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	if _, err := requireTask(validationState(), "uuid", uuid); err != nil {
		return err
	}
	u := newTaskUpdate().schedule(1, nil, nil).clearReminder()
	env := writeEnvelope{id: uuid, action: 1, kind: "Task6", payload: u.build()}
	if err := writeToHistory(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func moveTaskToSomeday(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	if _, err := requireTask(validationState(), "uuid", uuid); err != nil {
		return err
	}
	u := newTaskUpdate().schedule(2, nil, nil).clearReminder()
	env := writeEnvelope{id: uuid, action: 1, kind: "Task6", payload: u.build()}
	if err := writeToHistory(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func moveTaskToInbox(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	if _, err := requireTask(validationState(), "uuid", uuid); err != nil {
		return err
	}
	u := newTaskUpdate().schedule(0, nil, nil).clearReminder()
	env := writeEnvelope{id: uuid, action: 1, kind: "Task6", payload: u.build()}
	if err := writeToHistory(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func uncompleteTask(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	if _, err := requireTask(validationState(), "uuid", uuid); err != nil {
		return err
	}
	u := newTaskUpdate().status(0)
	u.fields["sp"] = nil
	env := writeEnvelope{id: uuid, action: 1, kind: "Task6", payload: u.build()}
	if err := writeToHistory(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func untrashTask(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	if _, err := requireTask(validationState(), "uuid", uuid); err != nil {
		return err
	}
	u := newTaskUpdate().trash(false)
	env := writeEnvelope{id: uuid, action: 1, kind: "Task6", payload: u.build()}
	if err := writeToHistory(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

// purgeTask permanently deletes a task via Tombstone2. Unlike trash, this
// cannot be undone — the event removes the task from every synced device.
func purgeTask(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	if _, err := requireTask(validationState(), "uuid", uuid); err != nil {
		return err
	}
	tombUUID := generateUUID()
	payload := map[string]any{
		"dloid": uuid,
		"dld":   nowTs(),
	}
	env := writeEnvelope{id: tombUUID, action: 0, kind: "Tombstone2", payload: payload}
	if err := writeToHistory(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func createArea(title string, tagUUIDs []string) (string, error) {
	areaUUID := generateUUID()
	validatedTags, err := validateUUIDSlice("tags", tagUUIDs)
	if err != nil {
		return "", err
	}
	if err := requireTags(validationState(), "tags", validatedTags); err != nil {
		return "", err
	}
	payload := map[string]any{
		"ix": 0,
		"tt": title,
		"tg": validatedTags,
		"xx": defaultExtension(),
	}
	env := writeEnvelope{id: areaUUID, action: 0, kind: "Area3", payload: payload}
	if err := writeToHistory(env); err != nil {
		return "", err
	}
	syncAfterWrite()
	return areaUUID, nil
}

func editArea(uuid, title string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	if title == "" {
		return invalidInputf("title is required")
	}
	if err := requireArea(validationState(), "uuid", uuid); err != nil {
		return err
	}
	payload := map[string]any{"tt": title}
	env := writeEnvelope{id: uuid, action: 1, kind: "Area3", payload: payload}
	if err := writeToHistory(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func createTag(title, shorthand, parentUUID string) (string, error) {
	if err := validateOptionalUUID("parent", parentUUID); err != nil {
		return "", err
	}
	parentUUID = normalizeOptionalUUID(parentUUID)
	if parentUUID != "" {
		if err := requireTag(validationState(), "parent", parentUUID); err != nil {
			return "", err
		}
	}
	tagUUID := generateUUID()
	pn := []string{}
	if parentUUID != "" {
		pn = []string{parentUUID}
	}
	var sh any
	if shorthand != "" {
		sh = shorthand
	}
	payload := map[string]any{
		"ix": 0,
		"tt": title,
		"sh": sh,
		"pn": pn,
		"xx": defaultExtension(),
	}
	env := writeEnvelope{id: tagUUID, action: 0, kind: "Tag4", payload: payload}
	if err := writeToHistory(env); err != nil {
		return "", err
	}
	syncAfterWrite()
	return tagUUID, nil
}

func editTag(uuid, title, shorthand string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	if title == "" && shorthand == "" {
		return invalidInputf("nothing to change: set title and/or shorthand")
	}
	if err := requireTag(validationState(), "uuid", uuid); err != nil {
		return err
	}
	payload := map[string]any{}
	if title != "" {
		payload["tt"] = title
	}
	if shorthand == "none" {
		payload["sh"] = nil
	} else if shorthand != "" {
		payload["sh"] = shorthand
	}
	env := writeEnvelope{id: uuid, action: 1, kind: "Tag4", payload: payload}
	if err := writeToHistory(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func createHeading(title, projectUUID string) (string, error) {
	if err := validateOptionalUUID("project", projectUUID); err != nil {
		return "", err
	}
	projectUUID = normalizeOptionalUUID(projectUUID)
	if projectUUID != "" {
		if err := requireProject(validationState(), "project", projectUUID); err != nil {
			return "", err
		}
	}
	headingUUID := generateUUID()
	now := nowTs()

	pr := []string{}
	if projectUUID != "" {
		pr = []string{projectUUID}
	}

	payload := taskCreatePayload{
		Tp: 2, Sr: nil, Dds: nil, Rt: []string{}, Rmd: nil,
		Ss: 0, Tr: false, Dl: []string{}, Icp: false, St: 1,
		Ar: []string{}, Tt: title, Do: 0, Lai: nil, Tir: nil,
		Tg: []string{}, Agr: []string{}, Ix: 0, Cd: now, Lt: false,
		Icc: 0, Md: nil, Ti: 0, Dd: nil, Ato: nil, Nt: emptyNote(),
		Icsd: nil, Pr: pr, Rp: nil, Acrd: nil, Sp: nil,
		Sb: 0, Rr: nil, Xx: defaultExtension(),
	}

	env := writeEnvelope{id: headingUUID, action: 0, kind: "Task6", payload: payload}
	if err := writeToHistory(env); err != nil {
		return "", err
	}
	syncAfterWrite()
	return headingUUID, nil
}

func createProject(title, note, when, deadline, areaUUID, tzName string) (string, error) {
	if err := validateOptionalUUID("area", areaUUID); err != nil {
		return "", err
	}
	loc, err := resolveLocation(tzName)
	if err != nil {
		return "", err
	}
	areaUUID = normalizeOptionalUUID(areaUUID)
	if areaUUID != "" {
		if err := requireArea(validationState(), "area", areaUUID); err != nil {
			return "", err
		}
	}
	projectUUID := generateUUID()
	now := nowTs()

	var st int
	var sr, tir *int64
	var dd *int64

	switch when {
	case "today":
		st = 1
		today := todayMidnightIn(loc)
		sr = &today
		tir = &today
	case "someday":
		st = 2
	case "anytime", "":
		st = 1 // projects default to anytime
	default:
		return "", invalidInputf("invalid when value for project: %s (use today, anytime, or someday)", when)
	}

	if deadline != "" {
		t, err := time.Parse("2006-01-02", deadline)
		if err != nil {
			return "", invalidInputf("deadline must be YYYY-MM-DD format, got: %s", deadline)
		}
		ts := t.Unix()
		if ts < todayMidnightIn(loc) {
			return "", invalidInputf("deadline cannot be in the past")
		}
		dd = &ts
	}

	ar := []string{}
	if areaUUID != "" {
		ar = []string{areaUUID}
	}

	nt := emptyNote()
	if note != "" {
		nt = textNote(note)
	}

	payload := taskCreatePayload{
		Tp: 1, Sr: sr, Dds: nil, Rt: []string{}, Rmd: nil,
		Ss: 0, Tr: false, Dl: []string{}, Icp: false, St: st,
		Ar: ar, Tt: title, Do: 0, Lai: nil, Tir: tir,
		Tg: []string{}, Agr: []string{}, Ix: 0, Cd: now, Lt: false,
		Icc: 0, Md: nil, Ti: 0, Dd: dd, Ato: nil, Nt: nt,
		Icsd: nil, Pr: []string{}, Rp: nil, Acrd: nil, Sp: nil,
		Sb: 0, Rr: nil, Xx: defaultExtension(),
	}

	env := writeEnvelope{id: projectUUID, action: 0, kind: "Task6", payload: payload}
	if err := writeToHistory(env); err != nil {
		return "", err
	}
	syncAfterWrite()
	return projectUUID, nil
}

// ---------------------------------------------------------------------------
// Checklist item operations
// ---------------------------------------------------------------------------

func createChecklistItem(title, taskUUID string) (string, error) {
	if err := validateUUID("task_uuid", taskUUID); err != nil {
		return "", err
	}
	task, err := requireTask(validationState(), "task_uuid", taskUUID)
	if err != nil {
		return "", err
	}
	if task.Type != thingscloud.TaskTypeTask {
		return "", invalidInputf("checklist items can only be added to tasks, not projects or headings: %s", taskUUID)
	}
	itemUUID := generateUUID()
	now := nowTs()
	payload := map[string]any{
		"tt": title,
		"ts": []string{taskUUID},
		"ix": 0,
		"cd": now,
		"md": nil,
		"ss": 0,
		"sp": nil,
		"lt": false,
		"xx": defaultExtension(),
	}
	env := writeEnvelope{id: itemUUID, action: 0, kind: "ChecklistItem3", payload: payload}
	if err := writeToHistory(env); err != nil {
		return "", err
	}
	syncAfterWrite()
	return itemUUID, nil
}

func completeChecklistItem(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	if err := requireChecklistItem(validationState(), uuid); err != nil {
		return err
	}
	ts := nowTs()
	payload := map[string]any{
		"md": ts,
		"ss": 3,
		"sp": ts,
	}
	env := writeEnvelope{id: uuid, action: 1, kind: "ChecklistItem3", payload: payload}
	if err := writeToHistory(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func uncompleteChecklistItem(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	if err := requireChecklistItem(validationState(), uuid); err != nil {
		return err
	}
	payload := map[string]any{
		"md": nowTs(),
		"ss": 0,
		"sp": nil,
	}
	env := writeEnvelope{id: uuid, action: 1, kind: "ChecklistItem3", payload: payload}
	if err := writeToHistory(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func editChecklistItem(uuid, title string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	if title == "" {
		return invalidInputf("title is required")
	}
	if err := requireChecklistItem(validationState(), uuid); err != nil {
		return err
	}
	payload := map[string]any{
		"md": nowTs(),
		"tt": title,
	}
	env := writeEnvelope{id: uuid, action: 1, kind: "ChecklistItem3", payload: payload}
	if err := writeToHistory(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func deleteChecklistItem(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	if err := requireChecklistItem(validationState(), uuid); err != nil {
		return err
	}
	// Delete via Tombstone2
	tombUUID := generateUUID()
	payload := map[string]any{
		"dloid": uuid,
		"dld":   nowTs(),
	}
	env := writeEnvelope{id: tombUUID, action: 0, kind: "Tombstone2", payload: payload}
	if err := writeToHistory(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func handleCreateTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", 405)
		return
	}
	var req CreateTaskRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeJSONDecodeError(w, "invalid JSON: ", err)
		return
	}
	if req.Title == "" {
		jsonError(w, "title is required", 400)
		return
	}
	taskUUID, err := createTask(req)
	if err != nil {
		code := http.StatusInternalServerError
		if isInvalidInput(err) {
			code = http.StatusBadRequest
		}
		jsonError(w, err.Error(), code)
		return
	}
	jsonResponse(w, map[string]string{"status": "created", "uuid": taskUUID, "title": req.Title})
}

func handleCompleteTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", 405)
		return
	}
	var req UUIDRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeJSONDecodeError(w, "invalid JSON: ", err)
		return
	}
	if req.UUID == "" {
		jsonError(w, "uuid is required", 400)
		return
	}
	if err := completeTask(req.UUID); err != nil {
		code := http.StatusInternalServerError
		if isInvalidInput(err) {
			code = http.StatusBadRequest
		}
		jsonError(w, err.Error(), code)
		return
	}
	jsonResponse(w, map[string]string{"status": "completed", "uuid": req.UUID})
}

func handleTrashTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", 405)
		return
	}
	var req UUIDRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeJSONDecodeError(w, "invalid JSON: ", err)
		return
	}
	if req.UUID == "" {
		jsonError(w, "uuid is required", 400)
		return
	}
	if err := trashTask(req.UUID); err != nil {
		code := http.StatusInternalServerError
		if isInvalidInput(err) {
			code = http.StatusBadRequest
		}
		jsonError(w, err.Error(), code)
		return
	}
	jsonResponse(w, map[string]string{"status": "trashed", "uuid": req.UUID})
}

func handleEditTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, "method not allowed", 405)
		return
	}
	var req EditTaskRequest
	if err := decodeJSONBody(w, r, &req); err != nil {
		writeJSONDecodeError(w, "invalid JSON: ", err)
		return
	}
	if req.UUID == "" {
		jsonError(w, "uuid is required", 400)
		return
	}
	if err := editTask(req); err != nil {
		code := http.StatusInternalServerError
		if isInvalidInput(err) {
			code = http.StatusBadRequest
		}
		jsonError(w, err.Error(), code)
		return
	}
	jsonResponse(w, map[string]string{"status": "updated", "uuid": req.UUID})
}

// Ensure writeEnvelope implements Identifiable
var _ thingscloud.Identifiable = writeEnvelope{}

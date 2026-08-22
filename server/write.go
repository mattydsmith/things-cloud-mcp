package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"log"
	"math/big"
	"net/http"
	"strings"
	gosync "sync"
	"time"

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
	if len(id) < 20 || len(id) > 32 {
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
	return string(encoded)
}

// syncForRead syncs before a read operation, logging any errors.
// Returns the error so callers can optionally surface it.
func syncForRead() error {
	if _, err := syncer.Sync(); err != nil {
		log.Printf("[SYNC] pre-read sync failed: %v", err)
		return err
	}
	return nil
}

// syncAfterWrite syncs after a write to refresh local state.
// Errors are logged but not returned (best-effort refresh).
func syncAfterWrite() {
	if _, err := syncer.Sync(); err != nil {
		log.Printf("[SYNC] post-write refresh failed: %v", err)
	}
}

func nowTs() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

func todayMidnightUTC() int64 {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Unix()
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
	Tags       string `json:"tags,omitempty"`        // comma-separated tag UUIDs
	Repeat     string `json:"repeat,omitempty"`      // daily, weekly, monthly, yearly, every N days/weeks/months/years, optional "until YYYY-MM-DD"
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
	Area       string `json:"area,omitempty"`
	Tags       string `json:"tags,omitempty"`
	Repeat     string `json:"repeat,omitempty"` // daily, weekly, monthly, yearly, every N days/weeks/months/years, optional "until YYYY-MM-DD", none
}

// UUIDRequest is the JSON body for complete/trash endpoints.
type UUIDRequest struct {
	UUID string `json:"uuid"`
}

// ---------------------------------------------------------------------------
// Repeat rule builder
// ---------------------------------------------------------------------------

// repeatValueRequestsRule reports whether repeat asks for an actual recurrence
// (i.e. is not empty and not the explicit "none" no-op sentinel).
func repeatValueRequestsRule(repeat string) bool {
	return repeat != "" && repeat != "none"
}

// buildRepeatRule builds a RepeaterConfiguration JSON from a repeat string.
// Formats: "daily", "weekly", "monthly", "yearly", "every N days/weeks/months/years"
// Optional end date: append "until YYYY-MM-DD".
// Append " after completion" for repeat-after-completion mode (tp=1).
// Returns nil if repeat is empty.
func buildRepeatRule(repeat string, refDate time.Time) (*json.RawMessage, error) {
	if !repeatValueRequestsRule(repeat) {
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

func isConflictError(err error) bool {
	var statusErr *thingscloud.HTTPStatusError
	return errors.As(err, &statusErr) && statusErr.StatusCode == http.StatusConflict
}

// parseWhen interprets the when parameter. Returns (st, sr, tir, handled).
// For named values (today/anytime/someday/inbox/none) and YYYY-MM-DD dates.
// A future date goes to Upcoming (st=2), today's date goes to Today (st=1).
func parseWhen(when string) (st int, sr, tir *int64, handled bool) {
	switch when {
	case "today":
		today := todayMidnightUTC()
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
			today := todayMidnightUTC()
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
	tg, err := parseUUIDList("tags", req.Tags)
	if err != nil {
		return "", err
	}

	taskUUID := generateUUID()
	now := nowTs()

	var st int
	var sr, tir *int64
	var dd *int64

	if req.When != "" {
		s, r, t, ok := parseWhen(req.When)
		if !ok {
			return "", invalidInputf("invalid when value: %s (use today, anytime, someday, inbox, or YYYY-MM-DD)", req.When)
		}
		st, sr, tir = s, r, t
	} else {
		st = 0 // inbox
	}

	// TODO(docs/known-issues/repeat-disabled.md): repeat rules are temporarily
	// disabled as a safety precaution; remove this block (the logic below is
	// left intact) once the payload has been confirmed safe.
	if repeatValueRequestsRule(req.Repeat) {
		return "", invalidInputf("the 'repeat' parameter is currently unsupported: sending a recurrence rule through this API has been reported to corrupt/crash Things clients on macOS and iOS; do not use it until this is resolved (see docs/known-issues/repeat-disabled.md). Create the task without 'repeat' and add the recurrence manually in Things.")
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
		if ts < todayMidnightUTC() {
			return "", invalidInputf("deadline cannot be in the past")
		}
		dd = &ts
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

	nt := emptyNote()
	if req.Note != "" {
		nt = textNote(req.Note)
	}

	// Build repeat rule if specified
	var rr *json.RawMessage
	if req.Repeat != "" {
		refDate := time.Now()
		if sr != nil {
			refDate = time.Unix(*sr, 0)
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
		Tg: tg, Agr: []string{}, Ix: 0, Cd: now, Lt: false,
		Icc: 0, Md: nil, Ti: 0, Dd: dd, Ato: nil, Nt: nt,
		Icsd: nil, Pr: pr, Rp: nil, Acrd: nil, Sp: nil,
		Sb: 0, Rr: rr, Xx: defaultExtension(),
	}

	env := writeEnvelope{id: taskUUID, action: 0, kind: "Task6", payload: payload}
	if err := historyWrite(env); err != nil {
		return "", err
	}
	syncAfterWrite()
	return taskUUID, nil
}

func completeTask(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	ts := nowTs()
	u := newTaskUpdate().status(3).stopDate(ts)
	env := writeEnvelope{id: uuid, action: 1, kind: "Task6", payload: u.build()}
	if err := historyWrite(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func trashTask(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	u := newTaskUpdate().trash(true)
	env := writeEnvelope{id: uuid, action: 1, kind: "Task6", payload: u.build()}
	if err := historyWrite(env); err != nil {
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
	if err := validateOptionalUUID("area", req.Area); err != nil {
		return err
	}
	tags, err := parseUUIDList("tags", req.Tags)
	if err != nil {
		return err
	}

	u := newTaskUpdate()

	// TODO(docs/known-issues/repeat-disabled.md): repeat rules are temporarily
	// disabled as a safety precaution; remove this block once the payload has
	// been confirmed safe. repeat:"none" (clear an existing rule) is
	// intentionally still allowed — it only nils out "rr", same as a task
	// with no rule at all.
	if repeatValueRequestsRule(req.Repeat) {
		return invalidInputf("the 'repeat' parameter is currently unsupported: sending a recurrence rule through this API has been reported to corrupt/crash Things clients on macOS and iOS; do not use it until this is resolved (see docs/known-issues/repeat-disabled.md). Edit the task without 'repeat' (use repeat:\"none\" to clear an existing rule) and set up recurrence manually in Things.")
	}

	if req.Repeat != "" && req.When == "inbox" {
		return invalidInputf("repeat tasks cannot be in inbox; use when:anytime, today, someday, YYYY-MM-DD, or omit when")
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
	} else if req.When != "" {
		st, sr, tir, ok := parseWhen(req.When)
		if !ok {
			return invalidInputf("invalid when value: %s (use today, anytime, someday, inbox, none, or YYYY-MM-DD)", req.When)
		}
		u.schedule(st, sr, tir)
	}
	if req.Deadline == "none" {
		u.clearDeadline()
	} else if req.Deadline != "" {
		t, err := time.Parse("2006-01-02", req.Deadline)
		if err != nil {
			return invalidInputf("deadline must be YYYY-MM-DD format, got: %s", req.Deadline)
		}
		if t.Unix() < todayMidnightUTC() {
			return invalidInputf("deadline cannot be in the past")
		}
		u.deadline(t.Unix())
	}
	if req.ParentTask != "" {
		u.project(req.ParentTask)
	} else if req.Project != "" {
		u.project(req.Project)
		if req.When == "" {
			u.schedule(1, nil, nil)
		}
	}
	if req.Tags != "" {
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
		// If no new "when" is provided and the current task lives in Inbox, move it to Anytime.
		// This avoids an inconsistent repeat+inbox combination in Things.
		if req.When == "" {
			if err := syncForRead(); err == nil {
				if task, tErr := syncer.State().Task(req.UUID); tErr == nil && task != nil && task.Schedule == thingscloud.TaskScheduleInbox {
					u.schedule(1, nil, nil)
				}
			}
		}

		refDate := time.Now()
		rr, err := buildRepeatRule(req.Repeat, refDate)
		if err != nil {
			return fmt.Errorf("invalid repeat: %w", err)
		}
		u.fields["rr"] = rr
	}
	env := writeEnvelope{id: req.UUID, action: 1, kind: "Task6", payload: u.build()}
	if err := historyWrite(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func moveTaskToToday(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	today := todayMidnightUTC()
	u := newTaskUpdate().schedule(1, today, today)
	env := writeEnvelope{id: uuid, action: 1, kind: "Task6", payload: u.build()}
	if err := historyWrite(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func moveTaskToAnytime(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	u := newTaskUpdate().schedule(1, nil, nil)
	env := writeEnvelope{id: uuid, action: 1, kind: "Task6", payload: u.build()}
	if err := historyWrite(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func moveTaskToSomeday(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	u := newTaskUpdate().schedule(2, nil, nil)
	env := writeEnvelope{id: uuid, action: 1, kind: "Task6", payload: u.build()}
	if err := historyWrite(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func moveTaskToInbox(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	u := newTaskUpdate().schedule(0, nil, nil)
	env := writeEnvelope{id: uuid, action: 1, kind: "Task6", payload: u.build()}
	if err := historyWrite(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func uncompleteTask(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	u := newTaskUpdate().status(0)
	u.fields["sp"] = nil
	env := writeEnvelope{id: uuid, action: 1, kind: "Task6", payload: u.build()}
	if err := historyWrite(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func untrashTask(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	u := newTaskUpdate().trash(false)
	env := writeEnvelope{id: uuid, action: 1, kind: "Task6", payload: u.build()}
	if err := historyWrite(env); err != nil {
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
	payload := map[string]any{
		"ix": 0,
		"tt": title,
		"tg": validatedTags,
		"xx": defaultExtension(),
	}
	env := writeEnvelope{id: areaUUID, action: 0, kind: "Area3", payload: payload}
	if err := historyWrite(env); err != nil {
		return "", err
	}
	syncAfterWrite()
	return areaUUID, nil
}

func createTag(title, shorthand, parentUUID string) (string, error) {
	if err := validateOptionalUUID("parent", parentUUID); err != nil {
		return "", err
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
	if err := historyWrite(env); err != nil {
		return "", err
	}
	syncAfterWrite()
	return tagUUID, nil
}

func createHeading(title, projectUUID string) (string, error) {
	if err := validateOptionalUUID("project", projectUUID); err != nil {
		return "", err
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
	if err := historyWrite(env); err != nil {
		return "", err
	}
	syncAfterWrite()
	return headingUUID, nil
}

func createProject(title, note, when, deadline, areaUUID string) (string, error) {
	if err := validateOptionalUUID("area", areaUUID); err != nil {
		return "", err
	}
	projectUUID := generateUUID()
	now := nowTs()

	var st int
	var sr, tir *int64
	var dd *int64

	switch when {
	case "today":
		st = 1
		today := todayMidnightUTC()
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
		if ts < todayMidnightUTC() {
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
	if err := historyWrite(env); err != nil {
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
	if err := historyWrite(env); err != nil {
		return "", err
	}
	syncAfterWrite()
	return itemUUID, nil
}

func completeChecklistItem(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	ts := nowTs()
	payload := map[string]any{
		"md": ts,
		"ss": 3,
		"sp": ts,
	}
	env := writeEnvelope{id: uuid, action: 1, kind: "ChecklistItem3", payload: payload}
	if err := historyWrite(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func uncompleteChecklistItem(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	payload := map[string]any{
		"md": nowTs(),
		"ss": 0,
		"sp": nil,
	}
	env := writeEnvelope{id: uuid, action: 1, kind: "ChecklistItem3", payload: payload}
	if err := historyWrite(env); err != nil {
		return err
	}
	syncAfterWrite()
	return nil
}

func deleteChecklistItem(uuid string) error {
	if err := validateUUID("uuid", uuid); err != nil {
		return err
	}
	// Delete via Tombstone2
	tombUUID := generateUUID()
	payload := map[string]any{
		"dloid": uuid,
		"dld":   nowTs(),
	}
	env := writeEnvelope{id: tombUUID, action: 0, kind: "Tombstone2", payload: payload}
	if err := historyWrite(env); err != nil {
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

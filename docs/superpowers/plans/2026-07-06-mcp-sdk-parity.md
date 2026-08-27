# MCP ↔ SDK Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose the proven-but-hidden SDK abilities through the MCP server: reminders (`ato`), heading assignment (`agr`), cancel task (`ss=2`), area/tag/checklist-item rename, repeat rule in read output, tasks-by-tag query, and task purge (`Tombstone2`) — all fully tested.

**Architecture:** All writes go through `server/write.go` core ops (shared by REST + MCP) and the `writeToHistory` seam; all reads go through the `sync` engine's SQLite-backed `State`. Two features (reminder read-back, repeat read-back) need plumbing in the root SDK package (`types.go` presence tracking) and the `sync` package (apply + persist) before the server can surface them. Everything else is server-only.

**Tech Stack:** Go, mark3labs/mcp-go, SQLite (mattn/go-sqlite3), stdlib testing.

## Global Constraints

- Package layout: root `package things` (SDK), `sync/` (engine), `server/` (MCP+REST, `package main`).
- Every write op validates target existence via `validationState()` before writing (prevents orphan events).
- Tests stub the network with `withCapturedWrites(t)` and entity existence with `withFakeStore(t, populatedStore())` (`server/write_validation_test.go`).
- Wire values: canceled `ss=2`; FrequencyUnit 16=daily, 256=weekly, 8=monthly, 4=yearly; `ato` = seconds after midnight of the `sr` day.
- Do not hand-edit `itemaction_string.go`. No enum changes are planned, so `go generate` is not needed.
- Run `go build ./... && go vet ./... && go test ./...` before claiming any task done; `gofmt` all touched files.
- Commit after each task with a `feat:`/`test:` message.

---

### Task 1: SDK presence tracking for `ato`/`rr` + `Task.Repeater` field

**Files:**
- Modify: `types.go` (Task struct ~182, TaskActionItemPayload ~240-247, UnmarshalJSON ~283, Has* helpers ~304)
- Test: `types_presence_test.go` (new, package `things`)

**Interfaces:**
- Produces: `Task.Repeater *RepeaterConfiguration`; `(TaskActionItemPayload).HasAlarmTimeOffset() bool`; `(TaskActionItemPayload).HasRepeater() bool` — consumed by Task 2.

- [x] **Step 1: Write failing test** — `types_presence_test.go`: unmarshal payloads `{"ato":null}`, `{"ato":3600}`, `{}`, `{"rr":null}`, `{"rr":{"fu":16,"fa":1,"of":[]}}` and assert `HasAlarmTimeOffset()`/`HasRepeater()` report presence correctly (true/true/false and true/true respectively).
- [x] **Step 2: Run** `go test -run TestPayloadPresence ./...` — expect compile failure (methods undefined).
- [x] **Step 3: Implement** — add `alarmTimeOffsetSet`, `repeaterSet` bool fields with `json:"-"`; in `UnmarshalJSON` add `_, p.alarmTimeOffsetSet = raw["ato"]` and `_, p.repeaterSet = raw["rr"]`; add `HasAlarmTimeOffset`/`HasRepeater` mirroring `HasScheduledDate`; add `Repeater *RepeaterConfiguration` to `Task`.
- [x] **Step 4: Run** — PASS; run full `go test ./...`.
- [x] **Step 5: Commit** `feat(sdk): track ato/rr payload presence, add Task.Repeater`

### Task 2: sync engine applies + persists reminder and repeat

**Files:**
- Modify: `sync/process.go:383,460` (applyTaskPayload), `sync/store.go` (saveTask ~183, getTask ~38-105)
- Test: `sync/store_test.go` (extend), `sync/process_apply_test.go` (new)

**Interfaces:**
- Consumes: Task 1's `HasAlarmTimeOffset`/`HasRepeater`/`Task.Repeater`.
- Produces: `getTask` returns tasks with `AlarmTimeOffset` and `Repeater` populated; explicit `"ato":null` / `"rr":null` payloads clear them.

- [x] **Step 1: Failing tests** — store roundtrip: save Task with `AlarmTimeOffset` + `Repeater{FrequencyUnit:16, FrequencyAmplitude:1}`, get it back, assert both survive. applyTaskPayload unit test: old task with ato+rr, payload with explicit nulls clears both; payload without the keys preserves both.
- [x] **Step 2: Run** `go test ./sync -run 'TestTaskStorage|TestApplyTaskPayload'` — FAIL (Repeater dropped, ato never cleared).
- [x] **Step 3: Implement** — process.go: copy `t.Repeater = old.Repeater` in the old-state block; replace `if p.AlarmTimeOffset != nil` with `if p.HasAlarmTimeOffset()`; add `if p.HasRepeater() { t.Repeater = p.Repeater }`. store.go: marshal `t.Repeater` to JSON into `recurrence_rule` on save; unmarshal on scan in `getTask`.
- [x] **Step 4: Run** sync package tests — PASS; full suite.
- [x] **Step 5: Commit** `feat(sync): persist reminder offset clearing and repeat rules`

### Task 3: reminders through create/edit/moves + read output

**Files:**
- Modify: `server/write.go` (requests, parseReminder helper, createTask, buildEditUpdate, taskUpdate, move ops), `server/mcp.go` (taskOutput, formatTask, tool params)
- Test: `server/write_reminder_test.go` (new), `server/mcp_format_test.go` (extend)

**Interfaces:**
- Produces: `CreateTaskRequest.Reminder`, `EditTaskRequest.Reminder` (`"HH:MM"` / `"none"`); `parseReminder(string) (int, error)`; `taskOutput.Reminder` JSON `reminder` `"HH:MM"`.

- [x] **Step 1: Failing tests** — create with `When:"today", Reminder:"17:00"` → payload `Ato=61200`; reminder without date → invalid-input; `"25:00"`/`"9am"` → invalid-input; edit set (task has sr) → `ato` in fields; edit `Reminder:"none"` → `ato:nil`; edit `When:"someday"` auto-clears ato; `moveTaskToSomeday/Anytime/Inbox` include `"ato":nil`, `moveTaskToToday` does not; formatTask with `AlarmTimeOffset=61200` → `reminder:"17:00"`.
- [x] **Step 2: Run** — FAIL.
- [x] **Step 3: Implement** — `parseReminder` strict `15:04` parse → seconds; createTask validates sr!=nil; taskUpdate `.reminder(sec)`/`.clearReminder()`; buildEditUpdate: `none`→clear, else require date from req.When or existing task (`ScheduledDate`/`TodayIndexReference`); clearing/going-dateless (`when` none/anytime/someday/inbox) auto-clears; moves to anytime/someday/inbox clear `ato`; formatTask emits `"%02d:%02d"`; MCP `reminder` param on create+edit tools.
- [x] **Step 4: Run** server tests — PASS.
- [x] **Step 5: Commit** `feat(server): reminder support (ato) on create, edit, moves, and output`

### Task 4: heading assignment on create/edit

**Files:**
- Modify: `server/write.go` (requests, requireHeading, createTask, buildEditUpdate, taskUpdate), `server/mcp.go` (tool params)
- Test: `server/write_heading_test.go` (new)

**Interfaces:**
- Produces: `CreateTaskRequest.Heading`, `EditTaskRequest.Heading`; `requireHeading(st, name, uuid) error`.

- [x] **Step 1: Failing tests** — fakeStore gains a heading task (`Type: TaskTypeHeading`, new `testHeadingUUID`); create with heading → `Agr=[uuid]` and `St=1` when When empty; heading pointing at a plain task → invalid-input; unknown → not found; edit set → `agr:[uuid]`; edit `"none"` → `agr:[]`; edit heading on inbox task bumps st=1 (CLI parity).
- [x] **Step 2: Run** — FAIL.
- [x] **Step 3: Implement** — mirror CLI semantics (`cmd/things-cli/main.go:383-397,889-897`).
- [x] **Step 4: Run** — PASS.
- [x] **Step 5: Commit** `feat(server): heading assignment on task create and edit`

### Task 5: cancel task

**Files:**
- Modify: `server/write.go` (cancelTask), `server/mcp.go` (handler + tool)
- Test: `server/write_cancel_test.go` (new)

- [x] **Step 1: Failing tests** — cancelTask envelope: action=1, kind=Task6, `ss=2`, `sp` set; unknown uuid → not found.
- [x] **Step 2: Run** — FAIL. **Step 3:** implement as `completeTask` clone with `status(2)`; register `things_cancel_task`; note in `things_uncomplete_task` description that it also reopens canceled tasks. **Step 4:** PASS. **Step 5: Commit** `feat(server): cancel task tool (ss=2)`

### Task 6: edit (rename) areas and tags

**Files:**
- Modify: `server/write.go` (editArea, editTag), `server/mcp.go` (handlers + tools)
- Test: `server/write_entity_edit_test.go` (new)

- [x] **Step 1: Failing tests** — editArea: envelope action=1 kind=Area3 payload `{"tt":...}`; empty title → invalid-input; unknown → not found. editTag: title and/or shorthand (`"none"` clears sh → nil); neither → invalid-input; envelope action=1 kind=Tag4.
- [x] **Step 2-4:** FAIL → implement → PASS. Tools: `things_edit_area(uuid, title)`, `things_edit_tag(uuid, title?, shorthand?)`.
- [x] **Step 5: Commit** `feat(server): rename areas and tags`

### Task 7: edit (rename) checklist items

**Files:**
- Modify: `server/write.go` (editChecklistItem), `server/mcp.go` (handler + tool)
- Test: `server/write_entity_edit_test.go` (extend)

- [x] **Steps:** failing test (envelope action=1 kind=ChecklistItem3 payload has `tt`+`md`; empty title / unknown uuid rejected) → implement → PASS → commit `feat(server): rename checklist items`. Tool: `things_edit_checklist_item(uuid, title)`.

### Task 8: repeat rule in read output

**Files:**
- Modify: `server/mcp.go` (taskOutput.Repeat, describeRepeat, formatTask)
- Test: `server/mcp_format_test.go` (extend)

- [x] **Step 1: Failing tests** — `describeRepeat`: daily/1 → "every day"; weekly/2 → "every 2 weeks"; monthly+Type=1 → "every month after completion"; yearly + LastScheduledAt(2027-03-01, not neverending) → "every year until 2027-03-01"; nil → "".
- [x] **Steps 2-4:** FAIL → implement (unit map 16/256/8/4 → day/week/month/year; amplitude>1 pluralizes; `IsNeverending()` guards until) → PASS.
- [x] **Step 5: Commit** `feat(server): expose repeat rule in task output`

### Task 9: tasks-by-tag query + tool

**Files:**
- Modify: `sync/state.go` (TasksWithTag), `server/mcp.go` (handler + tool)
- Test: `sync/state_tag_test.go` (new), `server/mcp_meta_test.go` (tool registration, if pattern exists)

**Interfaces:**
- Produces: `(*State).TasksWithTag(tagUUID string, opts QueryOpts) ([]*things.Task, error)`.

- [x] **Step 1: Failing test** — sync: save tag + two tasks, one tagged; TasksWithTag returns only the tagged one; completed excluded by default.
- [x] **Steps 2-4:** FAIL → implement (JOIN task_tags, same filter idioms as TasksInArea) → PASS. Tool `things_list_tag_tasks(uuid, limit?)` validates tag exists first.
- [x] **Step 5: Commit** `feat: list tasks by tag`

### Task 10: purge task (permanent delete)

**Files:**
- Modify: `server/write.go` (purgeTask), `server/mcp.go` (handler + tool)
- Test: `server/write_cancel_test.go` or new `server/write_purge_test.go`

- [x] **Steps:** failing test (envelope kind=Tombstone2 action=0, payload `dloid`=task uuid + `dld` set; unknown → not found) → implement (clone of deleteChecklistItem with requireTask) → PASS → commit `feat(server): permanent task purge via Tombstone2`. Tool `things_purge_task` with `mcp.WithDestructiveHintAnnotation(true)`; description: permanent, cannot be undone, prefer `things_trash_task`.

### Task 11: metadata, docs, full verification

**Files:**
- Modify: `server/mcp.go` (version → 1.3.0, mcpServerInstructions), `docs/BACKLOG.md`, `docs/2026-02-23-api-capabilities-review.md` (addendum), `cmd/README.md` if it states tool counts
- Test: full suite

- [x] **Steps:** bump `mcpServerVersion` to `"1.3.0"`; extend instructions (reminders need a when-date; purge is permanent; new edit tools); update BACKLOG (mark shipped items); append dated addendum to the capabilities review noting closed gaps; run `go build ./... && go vet ./... && gofmt -l . && go test ./...` — all clean; commit `docs: record MCP/SDK parity work; bump server to 1.3.0`.

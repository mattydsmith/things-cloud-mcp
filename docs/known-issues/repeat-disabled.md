# `repeat` is temporarily disabled

**Status:** `repeat` is rejected with an error on `things_create_task` and `things_edit_task`
(both MCP and REST). `repeat: "none"` (clearing an existing rule on `things_edit_task`) still works.

## Why

A user reported that creating tasks with `repeat` set corrupted their Things Cloud history and
crashed Things 3 on both macOS and iOS on every subsequent launch, with no local recovery path on
iOS (recovery required direct SQLite manipulation on macOS; iOS needed vendor-side history rollback).
Reported batch: 25 tasks written in one session, 21 without `repeat` synced cleanly, 4 with `repeat`
broke both clients.

Given the severity (irreversible on iOS) and the fact that **no payload validation exists anywhere
in this codebase** to catch a malformed write before it ships, `repeat` is disabled unconditionally
as a precaution while this is investigated — independent of how strong the evidence for the specific
root cause turns out to be.

## What the investigation found

The reporter's technical diagnosis was built from the **native Things.app's local SQLite schema**
(`TMTask` table, `rt1_*` columns, an XML-plist recurrence rule read via `hex(rt1_recurrenceRule)`).
That diagnosis does not transfer to this codebase: this SDK never touches that local database file —
it speaks the **Things Cloud HTTP/JSON wire protocol**, which represents a repeating task differently
(a single `Task6` item carrying an `rr` JSON field). A repo-wide search found zero references to
`rt1_*` anywhere in this codebase.

This SDK already has a working, unit-tested implementation of that wire-format rule:
`buildRepeatRule` (`server/write.go:360-476`, tests in `server/write_repeat_test.go`) and a typed
`RepeaterConfiguration` model (`repeat.go`, tests in `repeat_test.go`). `repeat` is not an
unimplemented or misleading parameter — it's a deliberately shipped feature
(see `docs/BACKLOG.md`: "Add recurring task support (Done)").

This repo also has its own prior crash investigation
(`docs/crash-investigation.md`, dated 2026-02-17) that specifically reproduction-tested "task with
daily repeat rule (created with repeat)" and "add weekly repeat to an existing task" on a clean test
account — both came back **OK, no crash**. The crash that investigation *did* observe happened on an
old account with 500+ pre-existing native-Things items and could not be reproduced on a clean
account; the two bugs found and fixed there (`sp: 0` instead of `null` on un-complete, and missing
past-date validation) were unrelated to `repeat`.

**This contradicts the new report's claim of an exact correlation between `repeat` and corruption.**
That contradiction is not resolved here — it needs input from whoever filed the new report: was it
reproduced on a fresh test account (isolating `repeat` from pre-existing native-Things data, the way
the prior investigation did), or on an account with a long pre-existing native-app history? Which
exact `repeat` value(s) were used for the 4 broken tasks?

Independent of that contradiction, two real risk factors remain unaddressed in the write path today:

- **No payload validation exists anywhere** in this codebase (confirmed by grep for `Validate`).
- Three payload fields — `icsd` (instance-creation-start-date), `icc` (instance-creation-count), and
  `rp` — are hardcoded to `nil`/`0` in every task-create/edit call regardless of whether `repeat` is
  set (`server/write.go:636-638`). Nothing in this codebase confirms whether Things.app requires
  these populated alongside a non-null `rr`. Suspicious signal: `repeat.go`'s own occurrence-math
  methods (`NextScheduledAt`, `ComputeFirstScheduledAt`) — exactly the kind of logic that would
  compute these values — are currently dead code, called by nothing outside their own tests.

## What's needed to re-enable

A genuine Things Cloud request/response capture of a repeating task created **directly in Things.app**
(not through this connector), on a **dedicated test account** (never production — the blast radius of
getting this wrong again is the same crash, and iOS has no local recovery). That capture would answer
both open questions:

1. Does `icsd`/`icc`/`rp` need to be populated alongside `rr`, and with what values?
2. Does the Cloud wire protocol need two linked `Task6` records (template + instance) for a repeating
   task, or does a single record with `rr` set (as this repo already sends) fully satisfy it?

Until that capture exists, do not remove the guards in `createTask()`/`editTask()`
(`server/write.go`).

## Read-path gap (separate, lower-risk, unrelated to the crash)

Independent of the write-path question above: this SDK's local sync cache currently drops the
recurrence rule on read. `TaskActionItemPayload.Repeater` (`types.go:230`) is correctly parsed off
the wire but never copied onto the domain `things.Task` struct in `applyTaskPayload`
(`sync/process.go:356-479`), and the `sync` package's `tasks.recurrence_rule` column
(`sync/schema.go:55`) is always written `NULL` (`sync/store.go:189`). This means a repeating task
synced down from a real account (created natively in Things, or via `things_create_task` before this
hotfix) loses its rule in this server's local cache — already flagged independently in
`docs/2026-02-23-api-capabilities-review.md`. This is a safe, well-scoped fix that doesn't require a
live account to build or test, and is unrelated to whether `repeat` is safe to re-enable for writes.

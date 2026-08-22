# Things MCP — Backlog

## Medium Priority

### ~~Add area assignment on task edit~~ (Done)
Added `area` parameter to `things_edit_task`. Set an area UUID to assign, or `"none"` to clear.

### ~~Add notes clearing (`note: "none"`)~~ (Done)
Added support for `note: "none"` on `things_edit_task` to clear notes, matching `deadline: "none"` pattern.

### ~~Add completed tasks list tool~~ (Done)
Added `things_list_completed` tool (33rd tool). Returns completed tasks ordered by most recent, with optional `limit` parameter (default 50).

## Lower Priority

### ~~Add recurring task support~~ (Done, since disabled — see below)
Added `repeat` parameter to `things_create_task` and `things_edit_task`. Supports: daily, weekly, monthly, yearly, every N days/weeks/months/years, after completion mode, and "none" to clear.

### Re-enable recurring task support (`repeat`)
`repeat` is currently rejected with an error on both tools after a report of Things Cloud history corruption and client crashes. See [`docs/known-issues/repeat-disabled.md`](known-issues/repeat-disabled.md) — needs a real Things Cloud payload capture from a dedicated test account before it can be safely re-enabled.

### Investigate tag/area deletion via Tombstone2
Areas and tags can't currently be deleted. The SDK supports `Tombstone2` entities for explicit deletion — test if writing a Tombstone2 for a tag/area UUID actually deletes it.

### ~~Add subtask support~~ (Done)
Added `parent_task` parameter to `things_create_task` and `things_edit_task`. Sets the `pr` wire field to the parent task UUID.

# Things Cloud API — Capabilities vs SDK Coverage

**Date:** 2026-02-23
**Purpose:** Cross-reference what the Things Cloud wire protocol supports against what this SDK exposes across its three surfaces: core SDK, server/MCP, and CLI.

---

## 1. API Endpoints (HTTP Layer)

All known endpoints are implemented at the SDK level.

| Endpoint | Method | SDK | Server | CLI |
|----------|--------|-----|--------|-----|
| `/version/1/account/{email}` | GET | `verify.go:29` | `/api/verify` | `initCLI()` |
| `/version/1/account/{email}` | PUT | `account.go:41-119` | debug endpoints | — |
| `/version/1/account/{email}` | DELETE | `account.go:20` | debug endpoint | — |
| `/version/1/account/{email}/own-history-keys` | GET | `histories.go:153` | debug endpoint | — |
| `/version/1/account/{email}/own-history-keys` | POST | `histories.go:192` | — | — |
| `/version/1/account/{email}/own-history-keys/{id}` | DELETE | `histories.go:224` | debug endpoint | — |
| `/version/1/history/{id}` | GET | `histories.go:89` | — | — |
| `/version/1/history/{id}/items` | GET | `items.go:39` | via sync engine | via `loadState()` |
| `/version/1/history/{id}/commit` | POST | `histories.go:251` | via write ops | via write cmds |
| `/version/1/app-instance/{id}` | PUT | `app_instance.go:20` | — | — |

**No gaps** — every discovered endpoint has SDK support.

---

## 2. Entity Write Operations

| Entity | Wire Kind | Create | Edit | Delete | Gaps |
|--------|-----------|--------|------|--------|------|
| Task | `Task6` | SDK, Server, CLI | SDK, Server, CLI | Trash: all; Purge: CLI only | **Purge not in MCP** |
| Project | `Task6` (tp=1) | Server, CLI | Via task edit | Via trash | — |
| Heading | `Task6` (tp=2) | Server, CLI | Via task edit | — | **No delete** |
| Area | `Area3` | SDK, Server, CLI | — | — | **No edit, no delete** |
| Tag | `Tag4` | SDK, Server, CLI | — | — | **No edit, no delete** |
| ChecklistItem | `ChecklistItem3` | Server, CLI | — | Server (Tombstone) | **No title edit** |
| Settings | `Settings3` | — | — | — | **Not implemented at all** |

---

## 3. Task Wire Fields — What's Settable

Source: `TaskActionItemPayload` in `types.go:209-274`

### Fully exposed (create + edit + read)

| Field | Wire | Create | Edit (MCP) | Edit (CLI) | Read |
|-------|------|--------|------------|------------|------|
| Title | `tt` | Yes | Yes | Yes | Yes |
| Notes | `nt` | Yes | Yes | Yes | Yes |
| Schedule | `st` | Yes | Yes | Yes | Yes |
| Scheduled date | `sr` | Yes | Yes | Yes | Yes |
| Today index ref | `tir` | Yes | Yes | Yes | — |
| Deadline | `dd` | Yes | Yes | Yes | Yes |
| Completion date | `sp` | On complete | On complete | On complete | Yes |
| Area IDs | `ar` | Yes | Yes | Yes | Yes |
| Parent/Project | `pr` | Yes | Yes | Yes | Yes |
| Tag IDs | `tg` | Yes | Yes | Yes | Yes |
| Trashed | `tr` | Yes | Yes | Yes | Yes |
| Repeat config | `rr` | Yes | Yes | — | **Not in output** |

### Partially exposed

| Field | Wire | Status | Gap |
|-------|------|--------|-----|
| Action group (heading) | `agr` | CLI create+edit only | **Not in MCP edit** |
| Status | `ss` | Complete (3) / uncomplete (0) | **No cancel (ss=2)** |
| Type | `tp` | Set on create | **Can't change after create** |

### Not settable (always default/null)

| Field | Wire | Default | Notes |
|-------|------|---------|-------|
| Index (ordering) | `ix` | 0 | **Can't reorder tasks** |
| Today index | `ti` | 0 | **Can't reorder in Today view** |
| Due order | `do` | 0 | **Can't control deadline sort** |
| Delegate IDs | `dl` | `[]` | **Not settable** |
| Reminder date | `rmd` | null | **Not implemented** |
| Alarm time offset | `ato` | null | Read-only on Task struct |
| Deadline suppression | `dds` | null | **Not settable or readable** |
| Complete by children | `icp` | false | **Not settable** |
| Completed count | `icc` | 0 | **Not settable** |
| Subtask behavior | `sb` | 0 | **Not settable** |
| Instance creation start | `icsd` | null | Internal recurring field |
| Leavable | `lt` | false | Internal |
| Last action item | `lai` | null | Internal |
| Action required date | `acrd` | null | **Not settable** |
| Extension data | `xx` | `{sn:{},_t:"oo"}` | Pass-through only |

---

## 4. State Queries (Sync Engine)

| Query | Implemented | File | MCP Tool |
|-------|-------------|------|----------|
| Tasks in Inbox | Yes | `sync/state.go:106` | `things_list_inbox` |
| Tasks in Today | Yes | `sync/state.go:119` | `things_list_today` |
| Tasks in Anytime | **No** | — | — |
| Tasks in Someday | **No** | — | — |
| Tasks in Upcoming | **No** | — | — |
| All open tasks | Yes | `sync/state.go:42` | `things_list_all_tasks` |
| All projects | Yes | `sync/state.go:55` | `things_list_projects` |
| Tasks in project | Yes | `sync/state.go:142` | `things_list_project_tasks` |
| Tasks in area | Yes | `sync/state.go:161` | `things_list_area_tasks` |
| Completed tasks | Yes | `sync/state.go:180` | `things_list_completed` |
| Trashed tasks | **No** | Filtered out everywhere | — |
| Checklist items | Yes | `sync/state.go:195` | `things_list_checklist_items` |
| All areas | Yes | `sync/state.go:68` | `things_list_areas` |
| All tags | Yes | `sync/state.go:87` | `things_list_tags` |
| Single task/area/tag | Yes | `sync/state.go:27-39` | `things_get_task/area/tag` |
| Tasks by tag | **No** | — | — |
| Subtasks of task | **No** | — | — |
| Search by title/note | MCP only | `server/mcp.go:493` | `things_search_tasks` |

---

## 5. Gap Summary

### High-value gaps (Things 3 features users would expect)

| # | Gap | Impact | Effort |
|---|-----|--------|--------|
| 1 | **No Anytime/Someday/Upcoming list views** | Missing 3 of 5 main Things views | Low — SQL queries on existing data |
| 2 | **No area/tag editing** (rename, change shorthand) | Can't rename after creation | Low — same wire format as create |
| 3 | **No area/tag deletion** | Backlog item, Tombstone2 untested | Low — same pattern as checklist delete |
| 4 | **No heading assignment via MCP** | CLI has `--heading` (`agr`), MCP doesn't | Trivial — add `heading` param to `things_edit_task` |
| 5 | **No cancel task** (ss=2) | Status defined but never writable | Trivial — add `cancelTask()` |
| 6 | **No task reordering** | `ix`, `ti`, `do` not settable | Medium — need to understand ordering semantics |
| 7 | **No purge via MCP** | Tombstone deletion only in CLI + checklist | Low — same pattern as `deleteChecklistItem` |
| 8 | **No reminders** | `rmd` and `ato` not settable | Low — wire format fields exist |
| 9 | **Repeat config not in read output** | Can set repeaters but can't see them | Low — add to `formatTask()` |
| 10 | **No checklist item title editing** | Can create/complete/delete but not rename | Low — modify action on ChecklistItem3 |

### Lower-priority gaps

| # | Gap | Notes |
|---|-----|-------|
| 11 | No tasks-by-tag query | Would need SQL join on task_tags |
| 12 | No subtasks-of-task query | Would need SQL query on parent_uuid |
| 13 | No trashed tasks query | Filtered out in all queries |
| 14 | No delegate support | `dl` field exists, unclear UI mapping |
| 15 | No Settings3 support | App settings (groupTodayByParent, etc.) |
| 16 | No action required date | `acrd` field, unclear semantics |
| 17 | No deadline suppression | `dds` field, used for snoozing deadline alerts |
| 18 | No subtask behavior control | `sb` field, project completion rules |
| 19 | ChangeLog returns UnknownChange | Full typed reconstruction not done |

### Consistency gaps (feature exists in one surface but not another)

| Feature | CLI | Server/MCP | Gap |
|---------|-----|------------|-----|
| Heading assignment (`agr`) | `--heading` on create+edit | Create only | MCP edit missing heading param |
| Purge (Tombstone2) | `purge` command | Checklist items only | No task purge via MCP |
| Batch operations | `batch` command | — | No batch endpoint in server |
| Scheduled date (`--scheduled`) | Create+edit | Via `when` param | Different UX, same wire result |
| Checklist on create | `--checklist` flag | Separate tool | Minor convenience gap |

---

## Addendum — 2026-07-06

Status update after the MCP/SDK parity batch (server v1.3.0). Closed since this review:

| Gap (from §5) | Resolution |
|---------------|------------|
| #1 Anytime/Someday/Upcoming views | Shipped earlier (things_list_anytime/someday/upcoming) |
| #2 Area/tag editing | `things_edit_area`, `things_edit_tag` |
| #4 Heading assignment via MCP | `heading` param on `things_create_task` + `things_edit_task` |
| #5 Cancel task | `things_cancel_task` |
| #7 Purge via MCP | `things_purge_task` (destructive-hinted) |
| #8 Reminders | `reminder` param (HH:MM → `ato`) + `reminder` in output; sync engine honors explicit `ato`/`rr` clears via payload presence tracking |
| #9 Repeat config in read output | `repeat` summary in task output; `rr` now persisted by the sync engine |
| #10 Checklist item title editing | `things_edit_checklist_item` |
| #11 Tasks-by-tag query | `things_list_tag_tasks` / `State.TasksWithTag` |

Still open: #3 area/tag deletion (Tombstone2 untested for those kinds), #6 task reordering (`ix`/`ti`/`do` semantics unknown), and the low-priority items (#12–#19) minus #11.

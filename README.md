# Things Cloud

A Go server and SDK for the [Things 3](https://culturedcode.com/things/) cloud API. Gives [Claude](https://claude.ai) (and other MCP-compatible AI assistants) full read/write access to your task manager.

Built on a reverse-engineered, unofficial SDK — there is no official API documentation from Cultured Code.

## What it does

The server syncs your Things Cloud data into a local SQLite database and exposes it through two interfaces:

- **MCP Endpoint** (`/mcp`) — [Model Context Protocol](https://modelcontextprotocol.io/) for AI assistants like Claude
- **REST API** (`/api/*`) — Bearer token auth, for scripts and apps

Changes you make in the Things app show up immediately. Tasks Claude creates appear in Things within seconds. No restart required.
The server performs an initial sync at startup and now exits if it cannot establish a clean local snapshot.

```
┌──────────────┐     ┌──────────────────┐     ┌──────────────┐
│  Things App  │────▶│  Things Cloud    │◀────│  API Server  │
│  (Mac/iOS)   │     │  (Cultured Code) │     │  (Fly.io)    │
└──────────────┘     └──────────────────┘     └──────┬───────┘
                                                     │
                                              ┌──────┴───────┐
                                              │              │
                                         /api/*         /mcp
                                        REST API    MCP Endpoint
                                              │              │
                                         curl/apps    Claude.ai
                                                     Connector
```

## MCP tools (36)

Once connected, Claude can do pretty much everything you'd do in the Things app:

- **Browse your views** — Today, Inbox, Anytime, Someday, Upcoming, All Tasks, by Project, by Area, Completed (with date filters)
- **Look things up** — get any task, area, or tag; search by title or note content
- **Create** — tasks (with notes, dates, deadlines, project, tags, recurrence), projects, areas, tags, headings, and checklist items
- **Edit** — title, notes, dates, deadlines, project, area, tags, and recurrence
- **Organise** — move tasks between Today, Anytime, Someday, and Inbox
- **Complete & restore** — complete or reopen tasks and checklist items; trash and untrash
- **Diagnostics** — built-in smoke test covering the full create/read/edit/complete/trash cycle

<details>
<summary>Full tool reference</summary>

### Read tools

| Tool | Description | Parameters |
|------|-------------|------------|
| `things_list_today` | Tasks scheduled for today | `limit`, `offset` |
| `things_list_inbox` | Tasks in the inbox | `limit`, `offset` |
| `things_list_anytime` | Tasks in Anytime | `limit`, `offset` |
| `things_list_someday` | Tasks in Someday | `limit`, `offset` |
| `things_list_upcoming` | Tasks in Upcoming | `limit`, `offset` |
| `things_list_all_tasks` | All open tasks | `limit`, `offset` |
| `things_list_projects` | All projects | `limit`, `offset` |
| `things_list_areas` | All areas | `limit`, `offset` |
| `things_list_tags` | All tags | `limit`, `offset` |
| `things_list_project_tasks` | Tasks in a project | `project_uuid`, `limit`, `offset` |
| `things_list_area_tasks` | Tasks in an area | `area_uuid`, `limit`, `offset` |
| `things_list_completed` | Recently completed tasks | `limit`, `completed_after`, `completed_before` |
| `things_list_checklist_items` | Checklist items for a task | `task_uuid` |
| `things_get_task` | Get a single task | `uuid` |
| `things_get_area` | Get a single area | `uuid` |
| `things_get_tag` | Get a single tag | `uuid` |

### Write tools

| Tool | Description | Parameters |
|------|-------------|------------|
| `things_create_task` | Create a task | `title` (req), `note`, `when`, `deadline`, `project`, `parent_task`, `tags`, `repeat` |
| `things_create_project` | Create a project | `title` (req), `note`, `when`, `deadline`, `area` |
| `things_create_heading` | Create a heading in a project | `title` (req), `project` |
| `things_create_area` | Create an area | `title` (req), `tags` |
| `things_create_tag` | Create a tag | `title` (req), `shorthand`, `parent` |
| `things_edit_task` | Edit a task | `uuid` (req), `title`, `note`, `when`, `deadline`, `project`, `parent_task`, `area`, `tags`, `repeat` |
| `things_complete_task` | Complete a task | `uuid` |
| `things_uncomplete_task` | Reopen a completed task | `uuid` |
| `things_trash_task` | Move to trash | `uuid` |
| `things_untrash_task` | Restore from trash | `uuid` |
| `things_move_to_today` | Schedule for today | `uuid` |
| `things_move_to_anytime` | Move to anytime | `uuid` |
| `things_move_to_someday` | Move to someday | `uuid` |
| `things_move_to_inbox` | Move to inbox | `uuid` |

### Search tools

| Tool | Description | Parameters |
|------|-------------|------------|
| `things_search_tasks` | Search tasks by title or note | `query` |

### Checklist item tools

| Tool | Description | Parameters |
|------|-------------|------------|
| `things_create_checklist_item` | Add a checklist item to a task | `title` (req), `task_uuid` (req) |
| `things_complete_checklist_item` | Complete a checklist item | `uuid` |
| `things_uncomplete_checklist_item` | Reopen a checklist item | `uuid` |
| `things_delete_checklist_item` | Delete a checklist item | `uuid` |

### Diagnostic tools

| Tool | Description | Parameters |
|------|-------------|------------|
| `things_smoke_test` | Run a smoke test: create, read, edit, complete, trash | — |

### Parameter reference

#### `when` — Task scheduling

| Value | Effect |
|-------|--------|
| `today` | Today view |
| `anytime` | Anytime view (triaged, no date) |
| `someday` | Someday view (deferred) |
| `inbox` | Inbox (default for new tasks) |
| `none` | Strip dates without moving the task (edit only) |
| `YYYY-MM-DD` | Future date → Upcoming; today/past → Today |

#### `deadline` — Hard deadlines

| Value | Effect |
|-------|--------|
| `YYYY-MM-DD` | Set a hard deadline |
| `none` | Clear an existing deadline (edit only) |

#### `repeat` — Recurring tasks

| Value | Effect |
|-------|--------|
| `daily` | Every day |
| `weekly` | Every week |
| `monthly` | Every month |
| `yearly` | Every year |
| `every N days/weeks/months/years` | Custom interval |
| `... until YYYY-MM-DD` | End recurrence on that date |
| `... after completion` | Repeat-after-completion mode |
| `none` | Clear recurrence (edit only) |

</details>

## Operational Notes

- **Startup is fail-fast.** The server now requires the initial sync to succeed before it starts serving REST or MCP traffic.
- **Shutdown is graceful.** `SIGINT` and `SIGTERM` trigger an HTTP shutdown with a timeout instead of dropping in-flight requests immediately.
- **Read surfaces support pagination.** REST list endpoints and the corresponding MCP list tools accept optional `limit` and `offset` values.
- **Old `tir`/`sr` cache corruption is repaired automatically.** Opening a pre-fix sync database triggers a one-time local cache reset and full resync from Things Cloud history.
- **Native view parity is testable on macOS.** Run `go run ./cmd/parity -url http://127.0.0.1:8765` to compare Today, Inbox, Anytime, Someday, and Upcoming UUIDs with Things 3 through AppleScript. The command exits non-zero on any mismatch.
- **Mac-local deployments can bind only to loopback.** Set `BIND_ADDR=127.0.0.1`; use `DB_PATH` to choose the persistent SQLite mirror location.
- **Soft-deleted rows are purged from the local cache after sync.** This keeps the SQLite mirror from growing forever while preserving normal sync semantics.

## Getting started

See **[docs/installation.md](docs/installation.md)** for full setup instructions. The short version:

1. Clone the repo and deploy to Fly.io
2. Set your Things Cloud credentials as Fly secrets
3. Add the MCP URL as a custom connector in Claude.ai
4. Create a Skill file for your workflow

### Privacy and cost

Your credentials are stored as encrypted secrets on your own Fly.io account — you're not giving them to anyone. Fly.io doesn't bill you if your monthly usage is under $5. After 2+ months of active development and daily use, the author has not been billed.

### Test with a separate account

Since this uses the Things Cloud sync protocol directly, consider creating a separate Things Cloud account to experiment with before pointing it at your main account.

## Skills

The MCP server gives Claude the ability to interact with Things, but to get the most out of it you need a **Claude Skill file** — a small instruction file that teaches Claude your specific projects, tags, and workflows.

See **[docs/skills.md](docs/skills.md)** for a step-by-step guide to creating your own.

## REST API

All `/api/*` endpoints require `Authorization: Bearer <API_KEY>` when `API_KEY` is set.

See **[docs/endpoints-and-things-cloud.md](docs/endpoints-and-things-cloud.md)** for how the REST and MCP surfaces sync from Things Cloud history, answer reads from the local SQLite mirror, and commit writes back to the cloud.

| Endpoint | Description |
|----------|-------------|
| `GET /` | Health check |
| `GET /api/verify` | Verify Things Cloud credentials |
| `GET /api/sync` | Trigger sync, returns change count |
| `GET /api/tasks/today` | Tasks scheduled for today. Optional query params: `limit`, `offset` |
| `GET /api/tasks/inbox` | Tasks in the inbox. Optional query params: `limit`, `offset` |
| `GET /api/tasks/anytime` | Tasks in Anytime. Optional query params: `limit`, `offset` |
| `GET /api/tasks/someday` | Tasks in Someday. Optional query params: `limit`, `offset` |
| `GET /api/tasks/upcoming` | Tasks in Upcoming. Optional query params: `limit`, `offset` |
| `GET /api/projects` | All projects. Optional query params: `limit`, `offset` |
| `GET /api/areas` | All areas. Optional query params: `limit`, `offset` |
| `GET /api/tags` | All tags. Optional query params: `limit`, `offset` |
| `POST /api/tasks/create` | Create a task |
| `POST /api/tasks/edit` | Edit a task |
| `POST /api/tasks/complete` | Complete a task |
| `POST /api/tasks/trash` | Trash a task |

## SDK

The underlying Go SDK can be used directly as a library. See **[docs/sdk.md](docs/sdk.md)** for documentation including:

- Getting started and quick start guide
- CLI tool (`things-cli`) with create, edit, complete, trash, batch commands
- Working with histories and items
- Persistent sync engine with 40+ semantic change types
- Wire format notes from reverse engineering

## Testing

113 integration tests across 5 test suites:

```bash
./tests/test-smoke.sh          # Core read/write workflow (11 checks)
./tests/test-mcp.sh 010        # All MCP write tools (44 checks)
./tests/test-mcp-read.sh       # All MCP read tools (29 checks)
./tests/test-mcp-protocol.sh   # JSON-RPC handshake and error handling (11 checks)
API_KEY=your-key ./tests/test-api.sh  # All REST endpoints (18 checks)
```

## Local development

```bash
go build -v -o things-server ./server/
export THINGS_USERNAME='...' THINGS_PASSWORD='...'
mkdir -p /data
./things-server

go test -v ./...
```

## Credits

Built on top of [arthursoares/things-cloud-sdk](https://github.com/arthursoares/things-cloud-sdk), which reverse-engineered the Things Cloud sync protocol.

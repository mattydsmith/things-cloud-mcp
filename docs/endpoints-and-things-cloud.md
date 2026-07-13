# How Endpoints Interact with Things Cloud

This server does **not** expose the raw Things Cloud API directly.

Instead, it sits between Things Cloud and two local surfaces:

- the MCP endpoint at `/mcp`
- the REST API at `/api/*`

The important distinction is:

- **reads** come from a local SQLite mirror, after an incremental sync
- **writes** are committed back to Things Cloud history, then pulled back into the local mirror

## Short Version

### Read path

For most read operations, the server:

1. Runs an incremental sync from Things Cloud
2. Updates the local SQLite cache
3. Answers the request from SQLite, not directly from the cloud response

That means endpoints like:

- `GET /api/tasks/today`
- `GET /api/tasks/inbox`
- `GET /api/tasks/anytime`
- `GET /api/tasks/someday`
- `GET /api/tasks/upcoming`
- `GET /api/projects`
- `GET /api/areas`
- `GET /api/tags`

and the matching MCP read tools all behave as **local queries over synced state**.

### Write path

For write operations, the server:

1. Builds a Things wire-format payload
2. Syncs the writable history to get the latest ancestor index
3. Sends a commit to Things Cloud
4. Re-syncs local state so subsequent reads reflect the change

That means endpoints like:

- `POST /api/tasks/create`
- `POST /api/tasks/edit`
- `POST /api/tasks/complete`
- `POST /api/tasks/trash`

and the matching MCP write tools are **history commits**, not direct SQL writes.

## Things Cloud Endpoints Used Under the Hood

The server uses a small set of underlying Things Cloud endpoints:

| Things Cloud endpoint | Method | Why the server calls it |
|---|---|---|
| `/version/1/account/{email}` | `GET` | Verify credentials and discover the user's history key |
| `/version/1/history/{id}` | `GET` | Fetch the latest server index before incremental sync |
| `/version/1/history/{id}/items` | `GET` | Pull incremental history items into the local sync cache |
| `/version/1/history/{id}/commit` | `POST` | Write task/project/area/tag/checklist changes back to Things Cloud |

There are other SDK-supported endpoints in the repo, but those four are the main ones used by the server's read/write flow.

## How Read Endpoints Work

### 1. Pre-read sync

Most read handlers call the sync engine before they answer.

Conceptually:

1. Load the saved history ID and last synced server index from SQLite
2. Ask Things Cloud for the current server index
3. If there is new history, fetch `/items` pages starting from the saved index
4. Apply those items to the local SQLite database
5. Run the local query and format the result

This is why the read surfaces feel "live" without querying Things Cloud for every list operation.

### 2. Local query phase

Once sync completes, the endpoint reads from the local `sync.State()` API.

Examples:

- Inbox is a SQL query over tasks with `schedule = 0`
- Today is a SQL query over active `schedule = 1` tasks whose `scheduled_date` or `today_index_ref` is earlier than the next UTC day boundary. This preserves Things carry-over behavior.
- Anytime, Someday, and Upcoming are **derived locally** from schedule, date, placement, visibility, and recurrence fields in the synced task rows.

There is no separate Things Cloud "Anytime endpoint" or "Someday endpoint". Those views are computed from synced history state.

### 3. Why Anytime / Someday / Upcoming are local derivations

The new dedicated queries are still based on the same underlying task fields:

- **Anytime**: all active `schedule = 1` tasks. Native Things treats this as an inclusive view, so Today tasks also appear here.
- **Someday**: standalone, non-recurring `schedule = 2` tasks that are not future-dated.
- **Upcoming**: active `schedule = 2` tasks with a future `scheduled_date` or `today_index_ref`, plus repeating templates carrying a repeater rule (`rr`). Generated instances link back through `rt` and are not automatically Upcoming.

All five core views exclude canceled, deleted, trashed, and hidden-by-parent tasks by default.

So these read endpoints are best understood as:

- **Things Cloud history sync first**
- **local view classification second**

## How Write Endpoints Work

### 1. Request mapping

Write handlers convert the API or MCP request into the Things wire format used by the history stream.

Examples:

- `when: "today"` becomes `st=1` plus today's `sr`/`tir`
- `when: "someday"` becomes `st=2` with no date
- moving to Someday writes `schedule(2, nil, nil)`

### 2. History sync before commit

Before writing, the server calls `history.Sync()` to refresh the writable history object's latest server index.

That matters because Things Cloud commits are sent with an `ancestor-index`. If the local writer is stale, the commit can conflict.

### 3. Commit to Things Cloud

The server then sends a `POST` to:

- `/version/1/history/{id}/commit`

using a single-item commit payload keyed by UUID.

### 4. Conflict retry

If Things Cloud responds with `409 Conflict`, the server:

1. re-syncs the history index
2. retries the commit once

This handles common races with simultaneous edits from the Things app.

### 5. Post-write refresh

After a successful commit, the server runs the sync engine again so the local SQLite mirror reflects the newly written cloud state.

That keeps subsequent reads and MCP responses consistent with the cloud history rather than relying on an assumed local mutation.

## Endpoint Categories

### Direct cloud passthrough-ish endpoints

These are close to direct Things Cloud calls:

- `GET /api/verify`
- `GET /api/sync`

`/api/verify` checks credentials against the account endpoint.
`/api/sync` explicitly runs the sync engine and returns how many semantic changes were detected.

### Synced local read endpoints

These endpoints first sync, then answer from SQLite:

- all `/api/tasks/*` read endpoints
- `/api/projects`
- `/api/areas`
- `/api/tags`
- most MCP read tools
- search tools that operate on synced task content

### Cloud write endpoints

These endpoints write a new history commit to Things Cloud and then re-sync:

- task create/edit/move/complete/trash endpoints
- project/area/tag/heading/checklist creation endpoints
- matching MCP write tools

## Practical Implications

### Reads are eventually consistent with cloud history

The server is usually very fresh because it syncs before reads, but the true source of truth is still the Things Cloud history stream.

If the Things app has just written something and Things Cloud propagation is still in flight, a read can briefly lag.

### Derived views depend on local classification rules

Views like Today, Anytime, Someday, and Upcoming are defined by this repo's sync/query logic, not by dedicated Things Cloud endpoints.

That means if you change the local classification rules, the endpoint behavior changes even though the upstream Things Cloud API stays the same.

### Writes are safer because they round-trip through cloud history

The server does not "pretend" a write succeeded by mutating SQLite directly. It commits to Things Cloud first, then pulls the resulting history state back into the mirror.

That design keeps the local cache aligned with the actual source of truth.

## See Also

- `README.md` for the public endpoint and tool lists
- `docs/sdk.md` for the lower-level SDK and sync engine
- `docs/2026-02-23-api-capabilities-review.md` for the detailed endpoint-to-code mapping

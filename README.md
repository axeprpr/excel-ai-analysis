# Excel AI Analysis

`excel-ai-analysis` is a standalone local analytics service for Excel-style question answering.

It is built around one core idea:

- upload spreadsheet files into an isolated session
- import them into a session-local `SQLite` database
- ask natural-language questions
- return text, SQL, table data, and chart-ready output
- optionally let a configured OpenAI-compatible model propose SQL before falling back to the built-in planner

The repository already contains a runnable V1 baseline with backend APIs, a `shadcn/ui` chat frontend, local session storage, and local chart integration scaffolding.

## What It Does

Current end-to-end flow:

1. Create or auto-create a session
2. Upload one or more spreadsheet files
3. Import files into the session's own local `SQLite` database
4. Build schema metadata for the imported tables
5. Ask a natural-language question
6. Execute a heuristic text-to-SQL flow against the session database
7. Return:
   - summary text
   - generated SQL
   - table rows
   - visualization metadata
   - chart output in `data`, `mermaid`, or `mcp` mode

This service is intended to work well both as:

- a direct local service with its own chat UI
- an upstream tool for systems such as Dify

## Current Implementation Status

Implemented today:

- session lifecycle APIs
- one local `SQLite` database per session
- local file upload and import task tracking
- real `.csv` import into SQLite
- real `.xlsx` import into SQLite with one SQLite table per non-empty sheet
- batched `.xlsx` row insertion to reduce large-sheet memory pressure
- placeholder `.xls` handling with explicit warnings
- schema catalog persistence in:
  - JSON files
  - SQLite catalog tables
- query execution against imported SQLite data when possible
- fallback placeholder query responses when true execution is not possible
- query modes:
  - `detail`
  - `aggregate`
  - `topn`
  - `trend`
  - `count`
  - `share`
  - `compare`
  - `year-over-year` compare planning
  - `month-over-month` compare planning
- planner diagnostics:
  - candidate table selection
  - planning confidence
  - selection reason
  - structured filter planning
- business-term table scoring for concepts such as `gmv`, `revenue`, `customer`, `order`, and `product`
- OpenAI-compatible SQL planning path
- LLM SQL repair retry using the SQLite execution error before heuristic fallback
- chart output modes:
  - `data`
  - `mermaid`
  - `mcp`
- local `@antv/mcp-server-chart` sidecar deployment via Compose
- real MCP HTTP execution against the configured local chart sidecar in `mcp` mode
- `shadcn/ui` frontend in `frontend/`
- chat-style UI with inline result rendering
- readiness, health, smoke, and basic deployment tooling
- exported OpenAPI 3.1 document at `GET /openapi.json`
- offline-ready local images for backend, frontend, and chart-mcp

Current limits:

- `.csv` is the strongest import path
- `.xlsx` now supports multi-sheet import, but it is still not a full enterprise workbook pipeline for formula-heavy files, type-rich formatting, or massive heterogeneous workbooks
- invalid `.xlsx` files fall back to placeholder schema scaffolding
- `.xls` is not truly parsed yet
- AI text-to-SQL is now a hybrid of local planner rules and optional OpenAI-compatible SQL generation, but it still lacks join planning and deeper semantic reasoning
- the LLM-backed SQL path now includes one repair retry using the SQLite execution error, but it is still not a full multi-step autonomous SQL repair loop
- compare mode supports grouped compare plus basic `yoy` and `mom` time-bucket compare, but does not yet produce richer enterprise-style delta/percentage-delta outputs
- chart MCP execution depends on a reachable local `mcp_server_url` and a supported chart shape

## Architecture

The repository currently runs as a local multi-service stack:

- `excel-ai-analysis`
  - Go API server
  - session management
  - import orchestration
  - SQLite access
  - query logic
- `frontend`
  - Vite + React + TypeScript
  - `shadcn/ui` chat-first workspace
- `chart-mcp`
  - local `@antv/mcp-server-chart` sidecar

At runtime, the data model is session-first:

- each session gets its own directory
- each session gets its own `session.db`
- uploaded files, task metadata, and schema snapshots are stored under that session directory

Suggested mental model:

- `session` = one isolated workbook analysis workspace

## Session Model

Every session owns:

- uploaded files
- import tasks
- schema metadata
- one dedicated local `SQLite` database
- query history context via the session database and schema state

Typical on-disk layout:

```text
data/
  sessions/
    sess_xxxxxxxx/
      session.db
      session.json
      uploads/
        sales.csv
        customers.xlsx
      imports/
        import_abcd1234.json
      schema/
        tables.json
```

Current session states:

- `active`
- `importing`
- `ready`
- `expired`
- `deleted`

Current implementation mostly uses:

- `active`
- `importing`
- `ready`

## Quick Start

### Run backend locally

```bash
make run
```

Default backend address:

- `http://127.0.0.1:8080`

### Run tests

```bash
make test
```

### Build frontend

```bash
make frontend-build
```

### Smoke check

```bash
make smoke
```

### Run the full local stack

```bash
make up
```

After startup:

- backend API: `http://127.0.0.1:8080`
- frontend UI: `http://127.0.0.1:4173`
- local chart MCP sidecar: `http://127.0.0.1:1122/mcp`

Offline deployment note:

- runtime containers no longer depend on `npm install` or `npx` at startup
- `frontend` and `chart-mcp` are built into local images before deployment
- when you want LLM planning or MCP chart execution in an offline environment, point the service to your own reachable internal endpoints

## Runtime Services

### Backend

The Go service exposes:

- REST APIs
- health and readiness probes
- local session storage
- local SQLite execution

### Frontend

The main UI is the React app in `frontend/`.

It is built with:

- Vite
- React
- TypeScript
- `shadcn/ui`

The frontend is chat-first:

- left side for session management, uploads, and settings
- right side for the conversation
- assistant messages render:
  - summary
  - SQL
  - table rows
  - chart output
  - warnings

Chart rendering behavior:

- `data` mode: inline visual card rendering based on returned data
- `mermaid` mode: Mermaid content rendered directly into SVG in the chat
- `mcp` mode: MCP payload and endpoint info shown for downstream chart execution

The legacy `GET /console` page still exists, but it is not the primary UI.

### Chart MCP Sidecar

`compose.yaml` includes a local `@antv/mcp-server-chart` service.

Current purpose:

- provide a local chart MCP target
- allow the backend and frontend to reference a local chart-rendering sidecar

Current implementation status:

- deployed locally
- exposed to the system
- referenced by settings and query output
- executed by the backend query path in `mcp` mode through MCP JSON-RPC over HTTP

## API Overview

Implemented top-level routes:

- `GET /`
- `GET /openapi.json`
- `GET /console`
- `GET /healthz`
- `GET /readyz`
- `GET /api/status`
- `GET /api/settings/model`
- `PUT /api/settings/model`
- `POST /api/chat/upload`
- `POST /api/chat/query`
- `GET /api/sessions`
- `POST /api/sessions`
- `GET /api/sessions/:session_id`
- `DELETE /api/sessions/:session_id`
- `GET /api/sessions/:session_id/files`
- `POST /api/sessions/:session_id/files/upload`
- `GET /api/sessions/:session_id/imports`
- `GET /api/sessions/:session_id/imports/:task_id`
- `GET /api/sessions/:session_id/schema`
- `GET /api/sessions/:session_id/database`
- `POST /api/sessions/:session_id/query`

### Discovery And Ops

- `GET /`
  - returns service metadata, capabilities, routes, and runtime config
- `GET /openapi.json`
  - exports an OpenAPI 3.1 document for Dify, agent platforms, and tool import flows
- `GET /healthz`
  - basic process health
- `GET /readyz`
  - checks local readiness, including SQLite availability and data directory readiness
- `GET /api/status`
  - returns global local-node summary counts across sessions

### Model Settings

- `GET /api/settings/model`
- `PUT /api/settings/model`

Current stored settings include:

- `provider`
- `model`
- `base_url`
- `api_key`
- `default_chart_mode`
- `mcp_server_url`

These settings are stored locally and are currently used as configuration input rather than as a fully wired LLM execution layer.

If `provider`, `model`, `base_url`, and `api_key` are all configured, the query layer will first try an OpenAI-compatible SQL generation request and then fall back to the built-in planner if that request fails or returns unsafe SQL.

### Session APIs

- `POST /api/sessions`
  - create a session explicitly
- `GET /api/sessions`
  - list sessions with summary counters
- `GET /api/sessions/:session_id`
  - inspect one session
- `DELETE /api/sessions/:session_id`
  - delete a session and its local files

Session summaries currently include:

- `uploaded_file_count`
- `table_count`
- `import_task_count`
- `total_row_count`

### File APIs

- `POST /api/sessions/:session_id/files/upload`
  - upload files into an existing session
- `GET /api/sessions/:session_id/files`
  - inspect uploaded files in a session

File listings currently include:

- file count
- total size
- extension counts
- latest file metadata

### Import APIs

- `GET /api/sessions/:session_id/imports`
- `GET /api/sessions/:session_id/imports/:task_id`

Import task data currently includes:

- `task_id`
- `status`
- `file_count`
- `file_names`
- `warning_count`
- `duration_ms`
- `started_at`
- `finished_at`

### Schema API

- `GET /api/sessions/:session_id/schema`

The schema response exposes imported table and column structure. It prefers the SQLite schema catalog when available.

### Database Diagnostics API

- `GET /api/sessions/:session_id/database`

Current diagnostics include:

- SQLite table names
- schema catalog
- preview rows
- import tasks
- aggregate counts such as:
  - `table_count`
  - `total_row_count`
  - `import_task_count`

### Query API

- `POST /api/sessions/:session_id/query`

Request body:

```json
{
  "question": "Show sales by category",
  "chart_mode": "mermaid"
}
```

Current query response shape includes:

- `session_id`
- `question`
- `summary`
- `sql`
- `rows`
- `columns`
- `row_count`
- `executed`
- `query_plan`
- `visualization`
- `chart`
- `warnings`

## Dify-Friendly Integration

The repository now contains two simplified endpoints specifically aimed at upstream orchestration systems such as Dify.

### 1. Upload With Auto Session

`POST /api/chat/upload`

This is the recommended first call for agent-style usage.

Behavior:

- accepts `multipart/form-data`
- if `session_id` is missing, auto-creates a new session
- uploads one or more files
- runs import synchronously inside the request
- if `question` is provided, runs a query immediately after import

Accepted form fields:

- `session_id` optional
- `question` optional
- `chart_mode` optional
- one or more `file` fields

Example behavior:

- first call with files and no `session_id`
- service returns a fresh `session_id`
- Dify stores that `session_id` in conversation variables

Example response shape:

```json
{
  "session_id": "sess_xxx",
  "session_created": true,
  "import": {
    "task_id": "import_xxx",
    "status": "completed",
    "file_count": 1,
    "file_names": ["sales.csv"],
    "warning_count": 0,
    "warnings": []
  },
  "files": [
    {
      "name": "sales.csv",
      "extension": ".csv",
      "size": 62
    }
  ],
  "answer": {
    "session_id": "sess_xxx",
    "summary": "Query on sales from sales.csv ran in detail mode and executed against SQLite, returning 2 row(s).",
    "sql": "SELECT category, amount FROM sales LIMIT 100;",
    "rows": [],
    "columns": [],
    "chart_mode": "mermaid",
    "chart": {},
    "warnings": []
  }
}
```

### 2. Follow-Up Query

`POST /api/chat/query`

This is the recommended follow-up call after Dify has already stored a `session_id`.

Request body:

```json
{
  "session_id": "sess_xxx",
  "question": "Count rows",
  "chart_mode": "data"
}
```

Behavior:

- reuses an existing session
- requires the session to already be `ready`
- runs a follow-up question against that session

Recommended Dify pattern:

1. first request uses `POST /api/chat/upload`
2. Dify stores `session_id`
3. later messages use `POST /api/chat/query`

This is the easiest integration mode because Dify does not need to manage the lower-level session creation and import workflow itself.

## Import Behavior

Current supported upload extensions:

- `.csv`
- `.xlsx`
- `.xls`

### CSV

Current CSV behavior:

- real file parsing
- normalized column names
- basic type inference
- real SQLite table creation
- row insertion into SQLite

### XLSX

Current XLSX behavior:

- minimal real import support
- reads the first useful sheet path
- creates a SQLite table
- imports a small but real schema/data path when parsing succeeds

If parsing fails:

- the system falls back to placeholder schema scaffolding

### XLS

Current `.xls` behavior:

- accepted
- generates placeholder schema/table scaffolding
- returns explicit warnings

It should be treated as a compatibility placeholder, not as a completed import feature.

## Query Behavior

The current query layer is heuristic and intentionally narrow.

It is not yet a true LLM-driven text-to-SQL engine.

Current behavior:

- if imported SQLite data is available, the service tries to execute SQL against the session database
- if true execution cannot be completed, the service falls back to placeholder rows
- the system always returns explainability fields such as:
  - `summary`
  - `sql`
  - `query_plan`
  - `warnings`

Current query modes:

- `detail`
  - row-level preview style queries
- `aggregate`
  - total-style queries such as sum
- `topn`
  - grouped ranking queries
- `trend`
  - time-bucketed trend queries
- `count`
  - `COUNT(*)` style queries

### Query Plan Metadata

The current response includes a `query_plan` object with fields such as:

- `source_table`
- `source_file`
- `source_sheet`
- `selected_columns`
- `filters`
- `question`
- `chart_type`
- `mode`
- `sql`

This is intended to help both humans and upstream systems understand how the answer was formed.

## Chart Output Modes

The query layer supports three chart output modes.

### `data`

Returns structured rows and visualization metadata. This is the most portable output mode.

Best for:

- custom frontend rendering
- Dify agents
- downstream UI frameworks

### `mermaid`

Returns Mermaid chart content.

Current frontend behavior:

- Mermaid content is rendered directly into SVG inside assistant chat messages

Best for:

- lightweight embedded chat visualizations
- markdown-friendly chart responses

### `mcp`

Returns chart-generation metadata intended for MCP-based downstream rendering.

Current response includes:

- local MCP endpoint
- deployment name
- tool name
- chart payload
- execution status
- chart result metadata
- rendered chart URL when returned by the MCP server

Best for:

- local execution through `@antv/mcp-server-chart`
- systems that want both a structured chart payload and a rendered chart artifact

## Frontend

The main UI lives in `frontend/`.

It is a chat-first workspace, not a generic dashboard shell.

Current user-facing capabilities:

- create sessions
- inspect session list and summary counts
- upload files into the selected session
- configure provider/model/base URL/API key/MCP settings
- ask questions in a chat box
- see answers inline with:
  - summary
  - SQL
  - chart area
  - tabular result rows
  - warnings

Current frontend tech stack:

- React
- TypeScript
- Vite
- Tailwind
- `shadcn/ui`
- Mermaid

The frontend dev server proxies:

- `/api`
- `/healthz`
- `/readyz`

to the Go backend.

## Database Layout

Each session database currently contains internal metadata tables such as:

- `session_meta`
- `import_tasks`
- `imported_tables`
- `imported_columns`

Imported user data tables are then added alongside those metadata tables.

This gives the service two useful layers:

- actual imported data
- queryable import/session diagnostics

## Deployment

### Docker Compose

`compose.yaml` starts:

- backend
- frontend
- local chart MCP sidecar

The frontend and chart MCP services are now built from Dockerfiles in this repository instead of downloading npm dependencies at runtime.

### Health Checks

Current readiness and health tooling:

- `/healthz`
- `/readyz`
- Docker healthchecks
- `make smoke`

### Version Injection

The service supports `APP_VERSION`, and version metadata is exposed in:

- `/`
- `/healthz`
- `/readyz`

## Current Roadmap Gaps

Important unfinished areas:

- full `.xlsx` workbook support
- real `.xls` parsing
- actual LLM-backed text-to-SQL
- richer SQL validation and sandboxing
- true MCP execution inside the backend
- richer chart rendering beyond lightweight current UI presentation
- production auth and multi-user access control

## Recommended Use Cases Right Now

This repository is currently best suited for:

- local demos
- internal prototyping
- Dify tool integration
- session-based spreadsheet Q&A experiments
- chart-enabled chat workflows over small to medium imported datasets

It is not yet positioned as:

- a production BI platform
- a fully general spreadsheet ingestion engine
- a complete autonomous charting runtime

## Repository Layout

High-level structure:

```text
.
├── README.md
├── RELEASE_CHECKLIST.md
├── Dockerfile
├── compose.yaml
├── Makefile
├── cmd/
│   └── server/
├── internal/
│   └── api/
├── frontend/
└── scripts/
```

Current intent:

- `cmd/server`
  - Go service entrypoint
- `internal/api`
  - HTTP handlers and most service logic
- `frontend`
  - main chat UI
- `scripts`
  - smoke and helper scripts

## Summary

Current state of the project:

- runnable
- locally deployable
- session-isolated
- SQLite-backed
- chart-aware
- Dify-friendly
- frontend-backed by `shadcn/ui`

If you want the simplest integration path today:

1. start the stack with `make up`
2. use `POST /api/chat/upload` for the first request
3. store the returned `session_id`
4. use `POST /api/chat/query` for follow-up questions

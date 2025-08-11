# Excel AI Analysis

`excel-ai-analysis` is a standalone container service that turns uploaded Excel files into a local AI-queryable data workspace.

## Goal

Provide an API service for Excel question answering:

- Upload one or more Excel files.
- Support large files, including files with around 100,000 rows each.
- Parse Excel content and use AI to infer schema and table structure.
- Store structured data in local `SQLite3`.
- Support text-to-SQL querying for natural-language question answering.
- Return query results in a format that can be rendered as charts or tables.

## Current Status

The repository already contains a runnable first implementation.

What works now:

- Session creation, listing, inspection, and deletion
- Local session workspace creation with a dedicated `SQLite3` database
- File upload for `.csv`, `.xlsx`, and `.xls`
- Real CSV import into SQLite tables
- Minimal real XLSX import into SQLite tables
- Import task lifecycle tracking in both JSON files and SQLite
- Schema catalog persistence in both JSON files and SQLite
- Query API with basic modes:
  - detail
  - aggregate
  - top-n
  - trend by month
  - count
- Database inspection API for SQLite tables, imported schema catalog, and import task diagnostics
- Local container build and local compose startup files
- Local `@antv/mcp-server-chart` sidecar deployment via Compose

Current limitation:

- `.csv` has real row import into SQLite
- `.xlsx` has minimal real import support for the first sheet
- invalid `.xlsx` files fall back to placeholder schema scaffolding
- `.xls` currently uses placeholder schema/table scaffolding and is not fully parsed yet

## Quick Start

### Run locally

```bash
make run
```

### Run tests

```bash
make test
```

### Smoke check

```bash
make smoke
```

### Run with Docker Compose

```bash
make up
```

## Current API Surface

Implemented endpoints:

- `GET /`
- `GET /console`
- `GET /healthz`
- `GET /readyz`
- `GET /api/settings/model`
- `PUT /api/settings/model`
- `GET /api/status`
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

Current endpoint summary highlights:

- `GET /api/status` returns global summary counts across local sessions
- `GET /console` serves a minimal browser console for config, upload, and chat
- `GET /api/sessions` and `GET /api/sessions/:session_id` return session-level summary counters
- `GET /api/sessions/:session_id/files` returns file totals, extension counts, and latest file metadata
- `GET /api/sessions/:session_id/imports` returns task list plus aggregate task stats
- `GET /api/sessions/:session_id/database` returns SQLite diagnostics, preview rows, and aggregate counts
- `GET/PUT /api/settings/model` stores local model and MCP settings
- `GET /api/settings/model` and `PUT /api/settings/model` manage local model and MCP endpoint settings

## Current Query Behavior

The query layer is still heuristic, but it is no longer just static placeholder output.

Current behavior:

- If a real imported SQLite table is available, the service tries to execute SQL against the local session database.
- If real execution is not possible, it falls back to placeholder response data.
- Query responses include:
  - `sql`
  - `rows`
  - `columns`
  - `row_count`
  - `executed`
  - `summary`
  - `query_plan`
  - `visualization`
  - `warnings`

Current query modes:

- `detail`
- `aggregate`
- `topn`
- `trend`
- `count`

Current visualization metadata:

- `type`
- `x`
- `y`
- `series`
- `preferred_format`
- `source_table`

Current chart output modes:

- `data`
- `mermaid`
- `mcp`

## Service Scope

This repository is intended to run as a single independent container.

For chart MCP integration, the default local development setup uses a second local sidecar service in `compose.yaml`:

- `excel-ai-analysis`
- `chart-mcp`
- `frontend`

The container will provide:

- Session-based workspace isolation.
- File upload API for multiple Excel files.
- Background import pipeline for large Excel parsing.
- AI-assisted schema generation and table creation.
- Local `SQLite3` storage for imported datasets.
- Text-to-SQL API for asking questions in natural language.
- Data visualization output support based on common chart solutions.

## Session Model

The core unit of the service is a `session`.

Each session represents one isolated analysis workspace:

- A session owns its uploaded Excel files.
- A session owns its own local `SQLite3` database file.
- A session keeps its own schema context for AI text-to-SQL.
- Queries, results, and chart metadata are generated within a session.

In other words, each session should have its own local resources instead of sharing one global database:

- A dedicated session directory, for example `./data/sessions/<session_id>/`
- A dedicated SQLite database, for example `./data/sessions/<session_id>/session.db`
- Session-specific uploaded source files
- Session-specific import metadata and generated schema artifacts

This means different users, tasks, or business topics can be separated by session instead of mixing all uploaded data into one global database context.

Typical usage:

1. Create a session.
2. Upload one or more Excel files into that session.
3. Wait for the import task to finish.
4. Ask questions against that session.
5. Render tables or charts from that session's query results.

## High-Level Flow

1. Client creates a session.
2. Client uploads one or more Excel files into the session.
3. Service stores files locally and creates an import task.
4. Import pipeline reads workbook sheets in chunks.
5. AI analyzes headers, column semantics, and likely field types.
6. Service writes data into that session's own local `SQLite3` database.
7. Excel data is written into SQLite tables.
8. User sends a natural-language question for that session.
9. AI generates SQL based on the session schema.
10. Service executes SQL, returns rows, and optionally returns chart-friendly data.

## Session Storage Layout

Suggested local structure:

```text
data/
  sessions/
    sess_123/
      session.db
      uploads/
        sales.xlsx
        customers.xlsx
      imports/
        import_123.json
      schema/
        tables.json
```

This design keeps each session self-contained and easier to manage for:

- Isolation
- Cleanup
- Debugging
- Re-import
- Future session export or backup

## Session Lifecycle

A session should have an explicit lifecycle instead of staying forever.

Suggested states:

- `active`: session has been created and can accept uploads and queries
- `importing`: one or more files are being parsed and written into the session database
- `ready`: import is complete and the session is ready for question answering
- `expired`: session has passed its retention window and is no longer queryable
- `deleted`: session files and database have been removed

Suggested lifecycle rules:

1. A session is created with status `active`.
2. When files are uploaded and import starts, status becomes `importing`.
3. After import and schema generation finish, status becomes `ready`.
4. If the session is idle for too long or passes its retention policy, status becomes `expired`.
5. Expired sessions can be cleaned automatically or deleted explicitly by API.

Suggested retention strategy for the first version:

- Keep session data on local disk for a limited time
- Update `last_accessed_at` on upload, import, and query
- Run a background cleanup job to remove expired session directories
- Make retention configurable, for example `24h`, `72h`, or `7d`

Minimum session metadata:

- `session_id`
- `status`
- `created_at`
- `updated_at`
- `last_accessed_at`
- `expires_at`
- `database_path`
- `uploaded_files`
- `tables`

## Planned API

### 1. Create session

`POST /api/sessions`

Example response:

```json
{
  "session_id": "sess_123",
  "status": "active",
  "database_path": "./data/sessions/sess_123/session.db",
  "uploaded_file_count": 0,
  "table_count": 0,
  "import_task_count": 0,
  "total_row_count": 0,
  "expires_at": "2026-03-29T09:00:00Z"
}
```

### 2. Get session

`GET /api/sessions/:session_id`

Example response:

```json
{
  "session_id": "sess_123",
  "status": "ready",
  "created_at": "2026-03-26T09:00:00Z",
  "last_accessed_at": "2026-03-26T10:30:00Z",
  "expires_at": "2026-03-29T09:00:00Z",
  "database_path": "./data/sessions/sess_123/session.db",
  "tables": ["sales_2025", "customer_list"]
}
```

### 3. Upload Excel files

`POST /api/sessions/:session_id/files/upload`

Expected capability:

- Multipart upload
- Multiple files per request
- Large file support
- Async import task response

Example response:

```json
{
  "session_id": "sess_123",
  "task_id": "import_123",
  "status": "pending",
  "session_status": "importing",
  "files": [
    {
      "name": "sales.csv",
      "extension": ".csv",
      "size": 12345
    }
  ]
}
```

### 4. Check import status

`GET /api/sessions/:session_id/imports/:task_id`

Example response:

```json
{
  "session_id": "sess_123",
  "task_id": "import_123",
  "status": "completed",
  "session_status": "ready",
  "warning_count": 0,
  "duration_ms": 120,
  "tables": ["sales_2025", "customer_list"]
}
```

### 5. Ask questions in natural language

`POST /api/sessions/:session_id/query`

Example request:

```json
{
  "question": "What are the top 10 products by revenue this quarter?"
}
```

Example response:

```json
{
  "session_id": "sess_123",
  "sql": "SELECT product_name, SUM(revenue) AS total_revenue ...",
  "rows": [],
  "row_count": 0,
  "executed": true,
  "visualization": {
    "type": "bar",
    "x": "product_name",
    "y": "total_revenue",
    "series": ["total_revenue"],
    "preferred_format": "chart"
  }
}
```

### 6. Delete session

`DELETE /api/sessions/:session_id`

Expected behavior:

- Remove session database
- Remove uploaded files
- Remove import metadata and schema artifacts
- Mark session as deleted or return not found on future access

Example response:

```json
{
  "session_id": "sess_123",
  "status": "deleted"
}
```

## Import And Table-Building Strategy

Excel import is not just file parsing. The service should convert workbook content into a queryable relational structure that AI can reliably use.

### Import principles

- Process files sheet by sheet
- Process large sheets in chunks instead of loading everything into memory
- Preserve the raw source file in the session directory
- Build stable table names and column names
- Keep enough metadata so AI can understand what each table represents

### Table creation strategy

For each uploaded Excel file:

1. Read workbook metadata and enumerate sheets.
2. Detect the effective header row for each sheet.
3. Normalize sheet names into SQLite-safe table names.
4. Normalize column names into SQL-safe field names.
5. Infer basic field types from sampled and streamed row values.
6. Ask AI to enrich semantic meaning of columns when needed.
7. Create SQLite tables in the session database.
8. Insert sheet rows in batches.
9. Persist table schema metadata for later text-to-SQL generation.

Suggested table naming:

- Use file name + sheet name as the base
- Normalize to lowercase snake_case
- Add suffixes only when conflicts happen

Example:

- `sales.xlsx` + `Q1 Report` -> `sales_q1_report`
- `finance.xlsx` + `Sheet1` -> `finance_sheet1`

Suggested column normalization:

- Convert header text to lowercase snake_case
- Remove spaces and special characters
- Deduplicate repeated names
- Replace empty headers with generated names such as `column_1`, `column_2`

### Field type inference

The first version should use practical type inference instead of trying to be perfect.

Suggested SQLite-oriented types:

- `TEXT`
- `INTEGER`
- `REAL`
- `DATE`
- `DATETIME`

Suggested inference signals:

- Header text
- Sampled cell values
- Value consistency across chunks
- AI semantic hints for ambiguous business columns

Examples:

- `订单日期` -> likely `DATE`
- `销售额` -> likely `REAL`
- `数量` -> likely `INTEGER`
- `客户名称` -> likely `TEXT`

### Metadata generated per table

Each imported table should produce metadata that is easier for AI to consume than raw SQLite introspection alone.

Suggested metadata fields:

- `table_name`
- `source_file`
- `source_sheet`
- `row_count`
- `columns`
- `primary_semantics`
- `time_columns`
- `metric_columns`
- `dimension_columns`
- `sample_values`

Example metadata fragment:

```json
{
  "table_name": "sales_q1_report",
  "source_file": "sales.xlsx",
  "source_sheet": "Q1 Report",
  "row_count": 102348,
  "columns": [
    { "name": "order_date", "type": "DATE", "semantic": "time" },
    { "name": "product_name", "type": "TEXT", "semantic": "dimension" },
    { "name": "revenue", "type": "REAL", "semantic": "metric" }
  ]
}
```

## Text-To-SQL Context

Text-to-SQL should not rely only on the database schema. It should also use the metadata produced during import.

The AI query layer should have access to:

- Session table list
- Column names and SQLite types
- Source file and sheet names
- Semantic labels such as metric, dimension, and time
- Row counts and sample values
- Query constraints such as row limits and forbidden full-table scans when possible

This helps the model answer questions like:

- "这个季度销售额最高的前十个产品是什么"
- "按月份看客户增长趋势"
- "华东区利润率最低的城市有哪些"

### Text-to-SQL execution rules

The first version should enforce a narrow execution policy:

- Generate read-only SQL only
- Prefer `SELECT` queries
- Reject destructive SQL such as `DROP`, `DELETE`, and `UPDATE`
- Apply result row limits by default
- Return generated SQL together with rows for auditability

### Recommended answer payload

The query API should return both data and structured explanation for downstream rendering.

Suggested response fields:

- `sql`
- `rows`
- `columns`
- `summary`
- `visualization`
- `warnings`

## Visualization

The query result should support direct data presentation.

Initial direction:

- Return normalized chart metadata from the API.
- Support common chart types such as table, bar, line, pie, and scatter.
- Keep the chart layer replaceable so it can connect to a local chart solution later.

Suggested chart selection rules:

- Use `table` for detail records
- Use `bar` for ranking and category comparison
- Use `line` for time trends
- Use `pie` only for low-cardinality composition
- Use `scatter` for correlation-style analysis

There is an MCP option available from chart providers, but it must run locally. For now, this repository will only define the output contract needed for local chart rendering.

## Minimal Architecture

The first version should stay simple and keep all responsibilities inside one container, but still separate concerns internally.

Suggested modules:

- `api`: HTTP endpoints for session, upload, import status, query, and delete
- `session`: session creation, metadata persistence, retention, and cleanup
- `storage`: local file storage and session directory management
- `importer`: Excel parsing, chunked row reading, and table loading
- `schema`: header detection, column normalization, type inference, and metadata generation
- `ai`: schema enrichment and text-to-SQL prompt orchestration
- `query`: SQL validation, execution, result shaping, and chart suggestion
- `worker`: background job execution for import and cleanup

Suggested request flow:

1. API receives a request.
2. Session layer resolves the session workspace.
3. Storage layer reads or writes local files.
4. Importer or query layer handles the business logic.
5. AI layer is called only where semantic inference is needed.
6. Query result is returned as rows plus visualization metadata.

## Recommended Project Layout

Suggested repository layout for the first implementation:

```text
.
├── README.md
├── cmd/
│   └── server/
│       └── main.*
├── internal/
│   ├── api/
│   ├── session/
│   ├── storage/
│   ├── importer/
│   ├── schema/
│   ├── ai/
│   ├── query/
│   └── worker/
├── data/
│   └── sessions/
├── configs/
└── scripts/
```

Directory intent:

- `cmd/server`: service entrypoint
- `internal/api`: HTTP routes and request handlers
- `internal/session`: session lifecycle and metadata management
- `internal/storage`: local file paths, uploads, and session workspace helpers
- `internal/importer`: Excel parsing and batch import logic
- `internal/schema`: column normalization, type inference, and metadata output
- `internal/ai`: prompts, model calls, and AI result shaping
- `internal/query`: text-to-SQL generation, validation, execution, and formatting
- `internal/worker`: async task runner and cleanup jobs
- `data/sessions`: local runtime data, not source code
- `configs`: runtime config templates
- `scripts`: local development and maintenance scripts

## Async Task Model

Excel import should be asynchronous by default because files can be large and imports may involve AI-assisted schema work.

Suggested task types:

- `import`
- `cleanup`
- `rebuild_schema`

Suggested task states:

- `pending`
- `running`
- `completed`
- `failed`

Minimum task metadata:

- `task_id`
- `session_id`
- `type`
- `status`
- `created_at`
- `started_at`
- `finished_at`
- `error`

For the first version, tasks can be implemented with a simple in-process worker and persisted JSON metadata under the session directory.

## Constraints And Guardrails

The service should explicitly optimize for stability over maximum flexibility.

Recommended guardrails:

- Limit concurrent imports per session
- Reject unsupported file formats early
- Cap workbook and sheet parsing concurrency
- Use batched inserts for SQLite writes
- Add query timeout and row limit controls
- Log generated SQL for debugging
- Keep the AI prompt bounded to session-local schema only

## V1 Delivery Scope

The first deliverable does not need to solve every analytics problem. It only needs to make the core loop reliable:

1. Create session
2. Upload Excel files
3. Import sheets into the session database
4. Generate schema metadata
5. Ask a natural-language question
6. Generate and run read-only SQL
7. Return rows and chart-ready metadata

Out of scope for V1:

- Cross-session joins
- Multi-tenant auth system
- Distributed workers
- Heavy query optimization
- Rich dashboard editor
- Full MCP-driven chart runtime integration

## Non-Goals For The First Version

- Distributed storage
- Multi-node task scheduling
- Cloud database dependency
- Complex permissions model
- Production-grade chart orchestration

## First Version Focus

- Session-based workspace management
- One SQLite database per session
- Session lifecycle and cleanup
- Reliable Excel upload
- Large-sheet import
- AI-based schema inference
- Stable table naming and schema metadata generation
- In-process async import worker
- Local SQLite persistence
- Natural-language to SQL query
- Chart-friendly response payload

## Status

This README is the initial project definition. Implementation has not started yet.

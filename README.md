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

## Service Scope

This repository is intended to run as a single independent container.

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
  "session_status": "importing"
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
  "visualization": {
    "type": "bar",
    "x": "product_name",
    "y": "total_revenue"
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

## Visualization

The query result should support direct data presentation.

Initial direction:

- Return normalized chart metadata from the API.
- Support common chart types such as table, bar, line, pie, and scatter.
- Keep the chart layer replaceable so it can connect to a local chart solution later.

There is an MCP option available from chart providers, but it must run locally. For now, this repository will only define the output contract needed for local chart rendering.

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
- Local SQLite persistence
- Natural-language to SQL query
- Chart-friendly response payload

## Status

This README is the initial project definition. Implementation has not started yet.

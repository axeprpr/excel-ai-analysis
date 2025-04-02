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

## Planned API

### 1. Create session

`POST /api/sessions`

Example response:

```json
{
  "session_id": "sess_123",
  "status": "active",
  "database_path": "./data/sessions/sess_123/session.db"
}
```

### 2. Upload Excel files

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
  "status": "pending"
}
```

### 3. Check import status

`GET /api/sessions/:session_id/imports/:task_id`

Example response:

```json
{
  "session_id": "sess_123",
  "task_id": "import_123",
  "status": "completed",
  "tables": ["sales_2025", "customer_list"]
}
```

### 4. Ask questions in natural language

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
- Reliable Excel upload
- Large-sheet import
- AI-based schema inference
- Local SQLite persistence
- Natural-language to SQL query
- Chart-friendly response payload

## Status

This README is the initial project definition. Implementation has not started yet.

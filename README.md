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

- File upload API for multiple Excel files.
- Background import pipeline for large Excel parsing.
- AI-assisted schema generation and table creation.
- Local `SQLite3` storage for imported datasets.
- Text-to-SQL API for asking questions in natural language.
- Data visualization output support based on common chart solutions.

## High-Level Flow

1. Client uploads one or more Excel files.
2. Service stores files locally and creates an import task.
3. Import pipeline reads workbook sheets in chunks.
4. AI analyzes headers, column semantics, and likely field types.
5. Service creates tables in local `SQLite3`.
6. Excel data is written into SQLite tables.
7. User sends a natural-language question.
8. AI generates SQL based on the imported schema.
9. Service executes SQL, returns rows, and optionally returns chart-friendly data.

## Planned API

### 1. Upload Excel files

`POST /api/files/upload`

Expected capability:

- Multipart upload
- Multiple files per request
- Large file support
- Async import task response

Example response:

```json
{
  "task_id": "import_123",
  "status": "pending"
}
```

### 2. Check import status

`GET /api/imports/:task_id`

Example response:

```json
{
  "task_id": "import_123",
  "status": "completed",
  "tables": ["sales_2025", "customer_list"]
}
```

### 3. Ask questions in natural language

`POST /api/query`

Example request:

```json
{
  "question": "What are the top 10 products by revenue this quarter?"
}
```

Example response:

```json
{
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

- Reliable Excel upload
- Large-sheet import
- AI-based schema inference
- Local SQLite persistence
- Natural-language to SQL query
- Chart-friendly response payload

## Status

This README is the initial project definition. Implementation has not started yet.

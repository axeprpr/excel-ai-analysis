# Excel AI Analysis

`excel-ai-analysis` is a backend-only service for session-scoped spreadsheet analytics.

Core flow:

1. Import spreadsheet data into a session-local SQLite database.
2. Ask natural-language questions.
3. Return summary, SQL, rows, chart metadata, and structured repair trace.

## Current Scope

- Backend only. Frontend has been removed.
- One SQLite database per session.
- Import support:
  - `.csv` (best support)
  - `.xlsx` (structured-sheet support)
  - `.xls` (placeholder support with warnings)
- Query modes:
  - `detail`, `aggregate`, `topn`, `trend`, `count`, `share`, `compare`
- Chart output modes:
  - `data`, `mermaid`, `mcp`
- OpenAI-compatible endpoint:
  - `POST /v1/chat/completions`
- Workflow endpoints:
  - `POST /api/chat/upload`
  - `POST /api/chat/upload-url`
  - `POST /api/chat/query`

## Why This Design

- Session isolation avoids cross-dataset leakage.
- Backend keeps deterministic SQL safety checks.
- Optional LLM planning can improve SQL generation.
- Multi-pass repair and `repair_trace` improve debuggability in workflow orchestration.

## API Summary

### 1) Upload local files

`POST /api/chat/upload` (multipart/form-data)

Fields:

- `session_id` (optional)
- `question` (optional)
- `chart_mode` (optional)
- `model_config` (optional JSON string)
- `file` (one or many)

Behavior:

- Creates session when `session_id` is omitted.
- Imports files immediately.
- If `question` is provided, returns answer in the same response.

### 2) Upload by URL (S3/HTTP)

`POST /api/chat/upload-url` (application/json)

```json
{
  "session_id": "",
  "file_urls": ["https://example.com/data.csv"],
  "question": "帮我做分布分析",
  "chart_mode": "auto",
  "model_config": {
    "provider": "openai-compatible",
    "model": "Qwen/Qwen3.5-9B",
    "base_url": "https://api.siliconflow.cn/v1",
    "api_key": "sk-xxx",
    "embedding_provider": "openai-compatible",
    "embedding_model": "BAAI/bge-m3",
    "embedding_base_url": "https://api.siliconflow.cn/v1",
    "embedding_api_key": "sk-xxx"
  }
}
```

### 3) Query existing session

`POST /api/chat/query` (application/json)

```json
{
  "session_id": "sess_xxx",
  "question": "按URL分类统计访问量",
  "chart_mode": "data",
  "model_config": {
    "provider": "openai-compatible",
    "model": "Qwen/Qwen3.5-9B",
    "base_url": "https://api.siliconflow.cn/v1",
    "api_key": "sk-xxx"
  }
}
```

### 4) OpenAI-compatible chat

`POST /v1/chat/completions`

Behavior:

- Analysis-style messages are routed to session-aware query pipeline.
- Normal chat is proxied to configured OpenAI-compatible upstream model.
- For analysis, send `session_id` in request body.

## Request-Level Model Override

`model_config` is request-scoped. It overrides persisted model settings for the current request only.

Supported fields:

- `provider`
- `model`
- `base_url`
- `api_key`
- `embedding_provider`
- `embedding_model`
- `embedding_base_url`
- `embedding_api_key`
- `default_chart_mode`
- `mcp_server_url`

## Deployment

### Run locally

```bash
make run
```

Default:

- API: `http://127.0.0.1:8080`

### Run with Docker Compose

```bash
make up
```

Starts:

- backend (`excel-ai-analysis`)
- chart sidecar (`chart-mcp`)

### Health checks

- `GET /healthz`
- `GET /readyz`

## Notes

- Use workflow tools (Dify/code nodes/etc.) against backend APIs directly.
- `repair_trace` in query response is a structured execution/debug trace, not raw chain-of-thought.

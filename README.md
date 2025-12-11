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

## Workflow Code Node Example

The service is designed for workflow engines (such as Dify code nodes).  
Use one code node to call `/api/chat/upload-url`, and let the backend auto-create session when `session_id` is empty.

Example Python code node:

```python
import os
import requests

# Inputs from workflow
question = inputs.get("question", "")
file_urls = inputs.get("file_urls", [])  # list[str], e.g. S3 pre-signed URLs
session_id = inputs.get("session_id", "")  # optional; empty means auto-create
chart_mode = inputs.get("chart_mode", "auto")

api_base = os.getenv("EXCEL_AI_API_BASE", "http://excel-ai-analysis:8080")

# Model config comes from workflow secrets / env vars.
# Do not hardcode key/model/base_url in code.
model_config = {
    "provider": os.getenv("LLM_PROVIDER", "openai-compatible"),
    "model": os.getenv("LLM_MODEL", ""),
    "base_url": os.getenv("LLM_BASE_URL", ""),
    "api_key": os.getenv("LLM_API_KEY", ""),
    "embedding_provider": os.getenv("EMBED_PROVIDER", "openai-compatible"),
    "embedding_model": os.getenv("EMBED_MODEL", ""),
    "embedding_base_url": os.getenv("EMBED_BASE_URL", ""),
    "embedding_api_key": os.getenv("EMBED_API_KEY", ""),
}

payload = {
    "session_id": session_id,
    "file_urls": file_urls,
    "question": question,
    "chart_mode": chart_mode,
    "model_config": model_config,
}

resp = requests.post(
    f"{api_base}/api/chat/upload-url",
    json=payload,
    timeout=180,
)
resp.raise_for_status()
data = resp.json()

# Return to workflow
result = {
    "session_id": data.get("session_id", ""),
    "summary": data.get("summary", ""),
    "sql": data.get("sql", ""),
    "rows": data.get("rows", []),
    "chart": data.get("chart", {}),
    "repair_trace": data.get("repair_trace", []),
}
```

Recommended workflow fields:

- `question` (string)
- `file_urls` (array of object URLs)
- `session_id` (string, optional)
- `chart_mode` (`auto`/`data`/`mermaid`/`mcp`)

Secret fields (in workflow secret manager):

- `LLM_BASE_URL`
- `LLM_MODEL`
- `LLM_API_KEY`
- `EMBED_BASE_URL`
- `EMBED_MODEL`
- `EMBED_API_KEY`

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

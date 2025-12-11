# Excel AI Analysis

`excel-ai-analysis` 是一个纯后端服务，用于按 `session` 隔离的表格数据分析。

核心流程：

1. 将表格数据导入到 session 专属 SQLite 数据库。
2. 使用自然语言提问。
3. 返回摘要、SQL、结果行、图表元数据与结构化 `repair_trace`。

## 当前范围

- 仅后端服务，已移除前端。
- 每个 session 一个独立 SQLite 数据库。
- 导入支持：
  - `.csv`（支持最好）
  - `.xlsx`（支持结构化 sheet）
  - `.xls`（占位支持，返回告警）
- 查询模式：
  - `detail`、`aggregate`、`topn`、`trend`、`count`、`share`、`compare`
- 图表输出模式：
  - `data`、`mermaid`、`mcp`
- 工作流接口：
  - `POST /api/chat/upload`
  - `POST /api/chat/upload-url`
  - `POST /api/chat/query`

## 设计原因

- session 隔离，避免跨数据集污染。
- 后端执行确定性的 SQL 安全检查。
- 可选 LLM 规划提升 SQL 生成质量。
- 多轮修复与 `repair_trace` 提升流程编排可观测性。

## API 概览

### 1) 上传本地文件

`POST /api/chat/upload`（`multipart/form-data`）

字段：

- `session_id`（可选）
- `question`（可选）
- `chart_mode`（可选）
- `model_config`（可选，JSON 字符串）
- `file`（可传一个或多个）

行为：

- 未传 `session_id` 时自动创建 session。
- 立即执行导入。
- 若传入 `question`，同一次请求直接返回问答结果。

### 2) 按 URL 上传（S3/HTTP）

`POST /api/chat/upload-url`（`application/json`）

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

### 3) 查询已有 session

`POST /api/chat/query`（`application/json`）

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

## 请求级模型覆盖

`model_config` 是请求级配置，仅覆盖当前请求，不会改写持久化全局配置。

支持字段：

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

## 工作流代码节点示例

该服务面向流程编排系统（如 Dify Code 节点）。  
推荐使用一个代码节点调用 `/api/chat/upload-url`，当 `session_id` 为空时由后端自动创建。

Python 示例：

```python
import os
import requests

# 工作流输入
question = inputs.get("question", "")
file_urls = inputs.get("file_urls", [])  # list[str]，例如 S3 临时 URL
session_id = inputs.get("session_id", "")  # 可选；空值表示自动创建
chart_mode = inputs.get("chart_mode", "auto")

api_base = os.getenv("EXCEL_AI_API_BASE", "http://excel-ai-analysis:8080")

# 模型配置通过环境变量 / 密钥管理注入，不要硬编码
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

# 返回给后续节点
result = {
    "session_id": data.get("session_id", ""),
    "summary": data.get("summary", ""),
    "sql": data.get("sql", ""),
    "rows": data.get("rows", []),
    "chart": data.get("chart", {}),
    "repair_trace": data.get("repair_trace", []),
}
```

推荐的工作流字段：

- `question`（字符串）
- `file_urls`（对象存储 URL 数组）
- `session_id`（字符串，可选）
- `chart_mode`（`auto`/`data`/`mermaid`/`mcp`）

推荐放入密钥管理的字段：

- `LLM_BASE_URL`
- `LLM_MODEL`
- `LLM_API_KEY`
- `EMBED_BASE_URL`
- `EMBED_MODEL`
- `EMBED_API_KEY`

## 部署

### 本地运行

```bash
make run
```

默认地址：

- API: `http://127.0.0.1:8080`

### Docker Compose 运行

```bash
make up
```

默认启动：

- 后端服务（`excel-ai-analysis`）
- 图表侧车（`chart-mcp`）

### 健康检查

- `GET /healthz`
- `GET /readyz`

## 说明

- 推荐通过工作流工具（Dify / 代码节点 / 自定义编排）直接调用后端 API。
- `repair_trace` 是结构化执行/修复轨迹，不是原始思维链。

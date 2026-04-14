#!/usr/bin/env python3

import argparse
import json
import mimetypes
import os
from pathlib import Path

import requests
from mcp.server.fastmcp import FastMCP
from mcp.server.transport_security import TransportSecuritySettings


API_BASE = os.environ.get("EXCEL_AI_API_BASE", "http://127.0.0.1:8080").rstrip("/")
THREAD_DIRS = [
    Path(item)
    for item in os.environ.get(
        "DEERFLOW_THREADS_DIRS",
        "/deerflow-threads,/app/backend/.deer-flow/threads,/root/deer-flow/backend/.deer-flow/threads",
    ).split(",")
    if item.strip()
]

mcp = FastMCP(
    "excel-ai-analysis",
    transport_security=TransportSecuritySettings(enable_dns_rebinding_protection=False),
)


def normalize_chart_mode(value: str) -> str:
    normalized = str(value or "").strip().lower()
    if normalized in {"chart", "image", "img", "picture", "png", "jpg", "jpeg"}:
        return "mcp"
    if normalized in {"data", "mermaid", "mcp"}:
        return normalized
    return ""


def parse_json_object(raw: str | dict) -> dict:
    if isinstance(raw, dict):
        return raw
    if not str(raw or "").strip():
        return {}
    payload = json.loads(raw)
    if not isinstance(payload, dict):
        raise ValueError("expected a JSON object")
    return payload


def parse_json_list(raw: str | list[str]) -> list[str]:
    if isinstance(raw, list):
        return [str(item).strip() for item in raw if str(item).strip()]
    text = str(raw or "").strip()
    if not text:
        return []
    payload = json.loads(text)
    if isinstance(payload, list):
        return [str(item).strip() for item in payload if str(item).strip()]
    if isinstance(payload, str) and payload.strip():
        return [payload.strip()]
    raise ValueError("expected a JSON array of strings")


def resolve_runtime_uploaded_path(path_str: str) -> Path:
    candidate = Path(path_str)
    if candidate.exists():
        return candidate

    name = candidate.name
    if not name:
        return candidate

    for threads_dir in THREAD_DIRS:
        if not threads_dir.exists():
            continue
        matches = list(threads_dir.glob(f"*/user-data/uploads/{name}"))
        if matches:
            return max(matches, key=lambda item: (item.stat().st_mtime, item.stat().st_size))

    return candidate


def request_json(method: str, path: str, **kwargs) -> dict:
    response = requests.request(method, f"{API_BASE}{path}", timeout=600, **kwargs)
    response.raise_for_status()
    return response.json()


def build_markdown_table(rows: list[dict], limit: int = 5) -> list[str]:
    if not rows:
        return ["- 无结果行"]
    preview = rows[:limit]
    columns = list(preview[0].keys())
    if not columns:
        return ["- 无结果列"]
    lines = [
        "| " + " | ".join(columns) + " |",
        "| " + " | ".join(["---"] * len(columns)) + " |",
    ]
    for row in preview:
        lines.append("| " + " | ".join(str(row.get(column, "")) for column in columns) + " |")
    return lines


def format_answer(answer: dict) -> str:
    lines = []
    summary = str(answer.get("summary", "")).strip()
    if summary:
        lines.extend(["## 分析摘要", summary, ""])

    if answer.get("analysis_report"):
        lines.append("## 自动分析视图")
        for idx, item in enumerate(answer["analysis_report"][:3], start=1):
            if not isinstance(item, dict):
                continue
            response = item.get("response", {})
            query_plan = response.get("query_plan", {})
            lines.append(
                f"- 视图 {idx}: {query_plan.get('mode', '')} / {query_plan.get('chart_type', '')} / {query_plan.get('source_table', '')}"
            )
        lines.append("")

    lines.extend(
        [
            "## 查询信息",
            f"- session_id: {answer.get('session_id', '')}",
            f"- row_count: {answer.get('row_count', 0)}",
            f"- executed: {answer.get('executed', False)}",
            "",
        ]
    )

    sql = str(answer.get("sql", "")).strip()
    if sql:
        lines.extend(["## SQL", "```sql", sql, "```", ""])

    rows = answer.get("rows") or []
    if isinstance(rows, list):
        lines.extend(["## 结果预览", *build_markdown_table([row for row in rows if isinstance(row, dict)]), ""])

    chart = answer.get("chart") or {}
    if isinstance(chart, dict) and chart:
        lines.append("## 图表信息")
        for key in ("mode", "type", "url", "tool"):
            if key in chart and chart[key]:
                lines.append(f"- {key}: {chart[key]}")
        lines.append("")

    warnings = answer.get("warnings") or []
    if warnings:
        lines.append("## 提示")
        for item in warnings[:8]:
            lines.append(f"- {item}")

    return "\n".join(lines).strip()


def package_response(payload: dict) -> dict:
    answer = payload.get("answer")
    final_markdown = ""
    if isinstance(answer, dict):
        final_markdown = format_answer(answer)
    return {
        **payload,
        "final_markdown": final_markdown,
    }


@mcp.tool()
def analyze_spreadsheet_files(
    question: str,
    file_paths_json: str | list[str],
    session_id: str = "",
    chart_mode: str = "data",
    llm_config_input: str | dict = "",
) -> dict:
    file_paths = parse_json_list(file_paths_json)
    if not file_paths:
        raise ValueError("file_paths_json is required")

    resolved_paths = []
    for item in file_paths:
        resolved = resolve_runtime_uploaded_path(item)
        if not resolved.exists():
            raise FileNotFoundError(f"file not found: {item}")
        resolved_paths.append(resolved)

    data = {"question": question}
    if session_id.strip():
        data["session_id"] = session_id.strip()
    normalized_chart_mode = normalize_chart_mode(chart_mode)
    if normalized_chart_mode:
        data["chart_mode"] = normalized_chart_mode
    model_config = parse_json_object(llm_config_input)
    if model_config:
        data["model_config"] = json.dumps(model_config, ensure_ascii=False)

    files = []
    handles = []
    try:
        for path in resolved_paths:
            handle = path.open("rb")
            handles.append(handle)
            content_type = mimetypes.guess_type(path.name)[0] or "application/octet-stream"
            files.append(("file", (path.name, handle, content_type)))
        payload = request_json("POST", "/api/chat/upload", data=data, files=files)
        return package_response(payload)
    finally:
        for handle in handles:
            handle.close()


@mcp.tool()
def analyze_spreadsheet_urls(
    question: str,
    file_urls_json: str | list[str],
    session_id: str = "",
    chart_mode: str = "data",
    llm_config_input: str | dict = "",
) -> dict:
    file_urls = parse_json_list(file_urls_json)
    if not file_urls:
        raise ValueError("file_urls_json is required")

    payload = {
        "question": question,
        "file_urls": file_urls,
    }
    if session_id.strip():
        payload["session_id"] = session_id.strip()
    normalized_chart_mode = normalize_chart_mode(chart_mode)
    if normalized_chart_mode:
        payload["chart_mode"] = normalized_chart_mode
    model_config = parse_json_object(llm_config_input)
    if model_config:
        payload["model_config"] = model_config

    response = request_json("POST", "/api/chat/upload-url", json=payload)
    return package_response(response)


@mcp.tool()
def query_spreadsheet_session(
    session_id: str,
    question: str,
    chart_mode: str = "data",
    llm_config_input: str | dict = "",
) -> dict:
    if not session_id.strip():
        raise ValueError("session_id is required")
    if not question.strip():
        raise ValueError("question is required")

    payload = {
        "session_id": session_id.strip(),
        "question": question,
    }
    normalized_chart_mode = normalize_chart_mode(chart_mode)
    if normalized_chart_mode:
        payload["chart_mode"] = normalized_chart_mode
    model_config = parse_json_object(llm_config_input)
    if model_config:
        payload["model_config"] = model_config

    response = request_json("POST", "/api/chat/query", json=payload)
    return package_response(response)


@mcp.tool()
def inspect_spreadsheet_session(session_id: str) -> dict:
    if not session_id.strip():
        raise ValueError("session_id is required")
    schema = request_json("GET", f"/api/sessions/{session_id.strip()}/schema")
    files = request_json("GET", f"/api/sessions/{session_id.strip()}/files")
    session = request_json("GET", f"/api/sessions/{session_id.strip()}")
    summary_lines = [
        "## Session 概览",
        f"- session_id: {session.get('session_id', '')}",
        f"- status: {session.get('status', '')}",
        f"- uploaded_file_count: {session.get('uploaded_file_count', 0)}",
        f"- table_count: {session.get('table_count', 0)}",
        "",
        "## 文件",
    ]
    for item in files.get("files", [])[:10]:
        summary_lines.append(f"- {item.get('name', '')} ({item.get('extension', '')})")
    summary_lines.append("")
    summary_lines.append("## 表结构")
    for table in schema.get("tables", [])[:10]:
        columns = ", ".join(column.get("name", "") for column in table.get("columns", [])[:8])
        summary_lines.append(f"- {table.get('table_name', '')}: {columns}")
    return {
        "session": session,
        "schema": schema,
        "files": files,
        "final_markdown": "\n".join(summary_lines).strip(),
    }


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="excel-ai-analysis MCP server")
    parser.add_argument("--transport", choices=["stdio", "sse", "streamable-http"], default="stdio")
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=8000)
    parser.add_argument("--mount-path", default="/mcp")
    args = parser.parse_args()

    if args.transport == "stdio":
        mcp.run()
    else:
        mcp.settings.host = args.host
        mcp.settings.port = args.port
        mcp.run(transport=args.transport, mount_path=args.mount_path)

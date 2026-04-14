---
name: excel-ai-analysis-mcp
description: MCP-first skill for spreadsheet uploads, session-based Excel/CSV analysis, and follow-up querying.
---

# Excel AI Analysis MCP

如果用户要分析 Excel、CSV、账单、报表、台账、明细表，优先调用：

```text
excel-ai-analysis.analyze_spreadsheet_files(...)
excel-ai-analysis.query_spreadsheet_session(...)
excel-ai-analysis.inspect_spreadsheet_session(...)
```

强制规则：

- 当前 skill 已在上下文中，不要再读取 `/mnt/skills/custom/excel-ai-analysis-mcp/SKILL.md`。
- 如果用户问题里已经包含本地文件路径、上传后的运行时路径，优先调用 `analyze_spreadsheet_files(question=用户原话, file_paths_json=[...])`。
- 如果用户在同一批数据上继续追问，优先复用已有 `session_id`，调用 `query_spreadsheet_session(...)`，不要重复上传文件。
- 如果用户只说“继续分析这个表”“再看下趋势”，但没有文件也没有 `session_id`，先追问文件或 `session_id`。
- 不要调用 `read_file`、`bash`、`cat` 之类的文件工具来替代表格 MCP；当前交付只开放了 Excel MCP 工具。
- 首次导入后，如果发现 `analyze_spreadsheet_files(...)` 返回的聚合结果还不够精确，最多再补一次 `query_spreadsheet_session(...)` 做二次查询，然后直接给最终答案，不要循环调用。
- 一旦工具已经返回可用的 `rows`、`summary`、`sql` 或 `final_markdown`，直接整理成中文答复，不要继续试探性调用其他工具。
- 如果用户明确要“图片”“图片链接”“柱状图图片”“折线图图片”或“用 MCP 图表模式”，必须把 `chart_mode` 设为 `mcp`。
- 在 `chart_mode="mcp"` 下，如果工具结果里有 `chart.url` 或 `chart.result.url`，必须在最终答复里显式输出这个图片链接。
- 默认 `chart_mode` 用 `data`；只有用户明确要求 Mermaid 或 MCP 图表时，才切换 `mermaid` 或 `mcp`。
- 输出必须是中文，优先保留服务返回的结论、SQL、结果预览和关键告警。

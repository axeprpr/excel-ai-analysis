package api

func OpenAPISpec() map[string]any {
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "Excel AI Analysis API",
			"version":     "1.0.0",
			"description": "Session-isolated spreadsheet import and query service with Dify-friendly chat endpoints.",
		},
		"servers": []map[string]any{
			{"url": "http://127.0.0.1:8080"},
		},
		"components": map[string]any{
			"schemas": map[string]any{
				"QueryResponse": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"session_id": map[string]any{"type": "string"},
						"question":   map[string]any{"type": "string"},
						"sql":        map[string]any{"type": "string"},
						"row_count":  map[string]any{"type": "integer"},
						"executed":   map[string]any{"type": "boolean"},
						"chart_mode": map[string]any{"type": "string", "enum": []string{"data", "mermaid", "mcp"}},
						"summary":    map[string]any{"type": "string"},
						"columns": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"rows": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "object"},
						},
						"warnings": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"query_plan": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"source_table":        map[string]any{"type": "string"},
								"source_file":         map[string]any{"type": "string"},
								"source_sheet":        map[string]any{"type": "string"},
								"candidate_tables":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
								"planning_confidence": map[string]any{"type": "number"},
								"selection_reason":    map[string]any{"type": "string"},
								"dimension_column":    map[string]any{"type": "string"},
								"metric_column":       map[string]any{"type": "string"},
								"time_column":         map[string]any{"type": "string"},
								"selected_columns":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
								"filters":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
								"planned_filters": map[string]any{
									"type": "array",
									"items": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"column":   map[string]any{"type": "string"},
											"operator": map[string]any{"type": "string"},
											"value":    map[string]any{"type": "string"},
										},
									},
								},
								"question":   map[string]any{"type": "string"},
								"chart_type": map[string]any{"type": "string"},
								"mode": map[string]any{
									"type": "string",
									"enum": []string{"detail", "aggregate", "topn", "trend", "count", "share", "compare"},
								},
								"sql": map[string]any{"type": "string"},
							},
						},
						"chart": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"mode":     map[string]any{"type": "string"},
								"executed": map[string]any{"type": "boolean"},
								"url":      map[string]any{"type": "string"},
								"endpoint": map[string]any{"type": "string"},
								"tool":     map[string]any{"type": "string"},
								"error":    map[string]any{"type": "string"},
							},
						},
					},
				},
			},
		},
		"paths": map[string]any{
			"/api/chat/upload": map[string]any{
				"post": map[string]any{
					"summary":     "Upload spreadsheet files and auto-create a session when needed",
					"operationId": "chatUpload",
					"description": "Creates a session when session_id is omitted, imports uploaded files, and optionally answers the provided question in the same request.",
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"multipart/form-data": map[string]any{
								"schema": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"session_id": map[string]any{"type": "string"},
										"question":   map[string]any{"type": "string"},
										"chart_mode": map[string]any{"type": "string", "enum": []string{"data", "mermaid", "mcp"}},
										"file": map[string]any{
											"type":        "array",
											"items":       map[string]any{"type": "string", "format": "binary"},
											"description": "One or more uploaded spreadsheet files.",
										},
									},
								},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Import completed and optional answer returned.",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"session_id": map[string]any{"type": "string"},
											"import": map[string]any{
												"type": "object",
												"properties": map[string]any{
													"task_id":     map[string]any{"type": "string"},
													"status":      map[string]any{"type": "string"},
													"file_count":  map[string]any{"type": "integer"},
													"table_count": map[string]any{"type": "integer"},
												},
											},
											"answer": map[string]any{
												"$ref": "#/components/schemas/QueryResponse",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			"/api/chat/query": map[string]any{
				"post": map[string]any{
					"summary":     "Query an existing session",
					"operationId": "chatQuery",
					"description": "Runs natural-language analysis against an existing session-local SQLite database. The service may use an OpenAI-compatible LLM for SQL planning and repair before falling back to the built-in planner.",
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"type":     "object",
									"required": []string{"session_id", "question"},
									"properties": map[string]any{
										"session_id": map[string]any{"type": "string"},
										"question":   map[string]any{"type": "string"},
										"chart_mode": map[string]any{"type": "string", "enum": []string{"data", "mermaid", "mcp"}},
									},
								},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Structured answer for the requested session.",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"$ref": "#/components/schemas/QueryResponse",
									},
								},
							},
						},
					},
				},
			},
			"/api/sessions": map[string]any{
				"get": map[string]any{
					"summary":     "List sessions",
					"operationId": "listSessions",
					"responses": map[string]any{
						"200": map[string]any{"description": "Local sessions with summary counters."},
					},
				},
				"post": map[string]any{
					"summary":     "Create a session",
					"operationId": "createSession",
					"responses": map[string]any{
						"201": map[string]any{"description": "Session created."},
					},
				},
			},
			"/api/sessions/{session_id}": map[string]any{
				"get": map[string]any{
					"summary":     "Get a session",
					"operationId": "getSession",
					"parameters": []map[string]any{
						pathParam("session_id", "Session identifier."),
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Session details."},
					},
				},
				"delete": map[string]any{
					"summary":     "Delete a session",
					"operationId": "deleteSession",
					"parameters": []map[string]any{
						pathParam("session_id", "Session identifier."),
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Session deleted."},
					},
				},
			},
			"/api/sessions/{session_id}/files/upload": map[string]any{
				"post": map[string]any{
					"summary":     "Upload files into an existing session",
					"operationId": "uploadFilesToSession",
					"parameters": []map[string]any{
						pathParam("session_id", "Session identifier."),
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"multipart/form-data": map[string]any{
								"schema": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"file": map[string]any{
											"type":  "array",
											"items": map[string]any{"type": "string", "format": "binary"},
										},
									},
								},
							},
						},
					},
					"responses": map[string]any{
						"202": map[string]any{"description": "Import task created."},
					},
				},
			},
			"/api/sessions/{session_id}/imports/{task_id}": map[string]any{
				"get": map[string]any{
					"summary":     "Get import task status",
					"operationId": "getImportTask",
					"parameters": []map[string]any{
						pathParam("session_id", "Session identifier."),
						pathParam("task_id", "Import task identifier."),
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Import task status."},
					},
				},
			},
			"/api/sessions/{session_id}/query": map[string]any{
				"post": map[string]any{
					"summary":     "Query a ready session",
					"operationId": "querySession",
					"description": "Returns structured query output including planner diagnostics, warnings, SQL, rows, visualization metadata, and chart output.",
					"parameters": []map[string]any{
						pathParam("session_id", "Session identifier."),
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"type":     "object",
									"required": []string{"question"},
									"properties": map[string]any{
										"question":   map[string]any{"type": "string"},
										"chart_mode": map[string]any{"type": "string", "enum": []string{"data", "mermaid", "mcp"}},
									},
								},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{
							"description": "Structured query response.",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"$ref": "#/components/schemas/QueryResponse",
									},
								},
							},
						},
					},
				},
			},
			"/api/sessions/{session_id}/schema": map[string]any{
				"get": map[string]any{
					"summary":     "Inspect imported schema for a session",
					"operationId": "getSessionSchema",
					"parameters": []map[string]any{
						pathParam("session_id", "Session identifier."),
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Schema catalog for the session."},
					},
				},
			},
			"/api/sessions/{session_id}/database": map[string]any{
				"get": map[string]any{
					"summary":     "Inspect session database diagnostics",
					"operationId": "getSessionDatabase",
					"parameters": []map[string]any{
						pathParam("session_id", "Session identifier."),
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Database diagnostics for the session."},
					},
				},
			},
		},
	}
}

func pathParam(name, description string) map[string]any {
	return map[string]any{
		"name":        name,
		"in":          "path",
		"required":    true,
		"description": description,
		"schema": map[string]any{
			"type": "string",
		},
	}
}

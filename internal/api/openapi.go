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
		"paths": map[string]any{
			"/api/chat/upload": map[string]any{
				"post": map[string]any{
					"summary":     "Upload spreadsheet files and auto-create a session when needed",
					"operationId": "chatUpload",
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
											"type":   "array",
											"items":  map[string]any{"type": "string", "format": "binary"},
											"description": "One or more uploaded spreadsheet files.",
										},
									},
								},
							},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "Import completed and optional answer returned."},
					},
				},
			},
			"/api/chat/query": map[string]any{
				"post": map[string]any{
					"summary":     "Query an existing session",
					"operationId": "chatQuery",
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"type": "object",
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
						"200": map[string]any{"description": "Structured answer for the requested session."},
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
					"parameters": []map[string]any{
						pathParam("session_id", "Session identifier."),
					},
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"type": "object",
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
						"200": map[string]any{"description": "Structured query response."},
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

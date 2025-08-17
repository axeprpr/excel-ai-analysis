package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenerateSQLWithOpenAICompatible(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected authorization header: %s", got)
		}

		var req openAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Model != "Qwen/Qwen3.5-9B" {
			t.Fatalf("unexpected model: %s", req.Model)
		}

		_ = json.NewEncoder(w).Encode(openAIChatResponse{
			Choices: []struct {
				Message openAIChatMessage `json:"message"`
			}{
				{
					Message: openAIChatMessage{
						Role:    "assistant",
						Content: "{\"sql\":\"SELECT COUNT(*) AS total_count FROM sales;\",\"mode\":\"count\"}",
					},
				},
			},
		})
	}))
	defer server.Close()

	handler := &Handler{}
	resp, err := handler.generateSQLWithOpenAICompatible(modelSettings{
		Provider: "openai-compatible",
		Model:    "Qwen/Qwen3.5-9B",
		BaseURL:  server.URL,
		APIKey:   "test-key",
	}, llmSQLRequest{
		Question: "count rows",
		Schema: schemaSnapshot{
			Tables: []tableSchema{
				{TableName: "sales"},
			},
		},
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if resp.SQL != "SELECT COUNT(*) AS total_count FROM sales;" {
		t.Fatalf("unexpected sql: %s", resp.SQL)
	}
	if resp.Mode != "count" {
		t.Fatalf("unexpected mode: %s", resp.Mode)
	}
}

func TestBuildQueryPlanWithFallbackRejectsUnsafeLLMSQL(t *testing.T) {
	handler := &Handler{}
	settings := modelSettings{
		Provider: "openai-compatible",
		Model:    "Qwen/Qwen3.5-9B",
		BaseURL:  "http://127.0.0.1:1",
		APIKey:   "test-key",
	}
	snapshot := schemaSnapshot{
		Tables: []tableSchema{
			{
				TableName:  "sales",
				SourceFile: "sales.csv",
				Columns: []schemaColumn{
					{Name: "category", Type: "TEXT", Semantic: "dimension"},
					{Name: "amount", Type: "REAL", Semantic: "metric"},
				},
			},
		},
	}

	if isSafeReadOnlySQL("DROP TABLE sales;") {
		t.Fatalf("expected DROP TABLE to be unsafe")
	}
	if !isSafeReadOnlySQL("SELECT category, amount FROM sales LIMIT 10;") {
		t.Fatalf("expected SELECT to be safe")
	}

	plan, warnings := handler.buildQueryPlanWithFallback(settings, snapshot, "show sales by category")
	if plan.SQL == "" {
		t.Fatalf("expected fallback sql to be populated")
	}
	if len(warnings) == 0 {
		t.Fatalf("expected fallback warning when llm request fails")
	}
}

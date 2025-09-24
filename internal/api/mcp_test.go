package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExecuteChartMCP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if got := r.Header.Get("Accept"); got != "application/json, text/event-stream" {
			t.Fatalf("expected Accept header %q, got %q", "application/json, text/event-stream", got)
		}

		var req mcpRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode mcp request: %v", err)
		}
		if req.Method != "tools/call" {
			t.Fatalf("expected tools/call, got %s", req.Method)
		}
		if req.Params["name"] != "generate_bar_chart" {
			t.Fatalf("expected generate_bar_chart, got %v", req.Params["name"])
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]any{
				"content": []map[string]any{
					{
						"type": "text",
						"text": "https://example.local/chart.png",
					},
				},
				"_meta": map[string]any{
					"spec": map[string]any{
						"type": "bar",
					},
				},
			},
		})
	}))
	defer server.Close()

	result, err := executeChartMCP(server.URL, map[string]any{
		"type":  "bar",
		"title": "Sales By Category",
		"x":     "category",
		"y":     "amount",
	}, []string{"category", "amount"}, []map[string]any{
		{"category": "A", "amount": 10},
		{"category": "B", "amount": 20},
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if result["tool_name"] != "generate_bar_chart" {
		t.Fatalf("unexpected tool_name: %v", result["tool_name"])
	}
	if result["url"] != "https://example.local/chart.png" {
		t.Fatalf("unexpected chart url: %v", result["url"])
	}
}

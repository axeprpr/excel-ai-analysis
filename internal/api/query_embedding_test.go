package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBuildHeuristicQueryPlanUsesEmbeddingSelection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode embedding request: %v", err)
		}
		if len(req.Input) != 3 {
			t.Fatalf("expected 3 embedding inputs, got %d", len(req.Input))
		}

		_ = json.NewEncoder(w).Encode(embeddingResponse{
			Data: []struct {
				Embedding []float64 `json:"embedding"`
			}{
				{Embedding: []float64{0, 1}},
				{Embedding: []float64{1, 0}},
				{Embedding: []float64{0, 1}},
			},
		})
	}))
	defer server.Close()

	handler := &Handler{}
	settings := modelSettings{
		EmbeddingProvider: "openai-compatible",
		EmbeddingModel:    "text-embedding-3-small",
		EmbeddingBaseURL:  server.URL,
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
			{
				TableName:  "customers",
				SourceFile: "customers.csv",
				Columns: []schemaColumn{
					{Name: "customer_name", Type: "TEXT", Semantic: "dimension"},
					{Name: "created_at", Type: "TEXT", Semantic: "time"},
				},
			},
		},
	}

	plan, warnings := handler.buildHeuristicQueryPlan(settings, snapshot, "show customer trend")
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", warnings)
	}
	if plan.SourceTable != "customers" {
		t.Fatalf("expected customers table, got %s", plan.SourceTable)
	}
	if plan.SelectionReason != "selected by embedding similarity against the imported schema catalog" {
		t.Fatalf("unexpected selection reason: %s", plan.SelectionReason)
	}
}

func TestBuildHeuristicQueryPlanFallsBackWhenEmbeddingFails(t *testing.T) {
	handler := &Handler{}
	settings := modelSettings{
		EmbeddingProvider: "openai-compatible",
		EmbeddingModel:    "text-embedding-3-small",
		EmbeddingBaseURL:  "http://127.0.0.1:1",
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

	plan, warnings := handler.buildHeuristicQueryPlan(settings, snapshot, "show sales by category")
	if plan.SourceTable != "sales" {
		t.Fatalf("expected heuristic fallback to select sales, got %s", plan.SourceTable)
	}
	if len(warnings) == 0 {
		t.Fatalf("expected fallback warning when embedding retrieval fails")
	}
}

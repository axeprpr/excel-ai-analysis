package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGenerateOpenAICompatibleEmbeddings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/embeddings" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer emb-key" {
			t.Fatalf("unexpected authorization header: %s", got)
		}

		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Model != "text-embedding-3-small" {
			t.Fatalf("unexpected model: %s", req.Model)
		}
		if len(req.Input) != 2 {
			t.Fatalf("expected 2 embedding inputs, got %d", len(req.Input))
		}

		_ = json.NewEncoder(w).Encode(embeddingResponse{
			Data: []struct {
				Embedding []float64 `json:"embedding"`
			}{
				{Embedding: []float64{1, 0}},
				{Embedding: []float64{0, 1}},
			},
		})
	}))
	defer server.Close()

	handler := &Handler{}
	vectors, err := handler.generateOpenAICompatibleEmbeddings(modelSettings{
		EmbeddingProvider: "openai-compatible",
		EmbeddingModel:    "text-embedding-3-small",
		EmbeddingBaseURL:  server.URL,
		EmbeddingAPIKey:   "emb-key",
	}, []string{"sales table", "customer table"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if len(vectors) != 2 || len(vectors[0]) != 2 {
		t.Fatalf("unexpected embedding shape: %#v", vectors)
	}
}

func TestRankTablesByEmbedding(t *testing.T) {
	snapshot := schemaSnapshot{
		Tables: []tableSchema{
			{TableName: "sales"},
			{TableName: "customers"},
		},
	}

	candidates := rankTablesByEmbedding(snapshot, []float64{1, 0}, [][]float64{
		{0.99, 0.01},
		{0.2, 0.8},
	})
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if candidates[0].TableName != "sales" {
		t.Fatalf("expected first candidate to be sales, got %s", candidates[0].TableName)
	}
	if candidates[0].Score <= candidates[1].Score {
		t.Fatalf("expected sales score to be higher than customers")
	}
}

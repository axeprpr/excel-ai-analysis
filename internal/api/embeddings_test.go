package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmbedTextsLocallyIsDeterministic(t *testing.T) {
	first, provider, err := embedTexts(modelSettings{}, []string{"sales revenue by category"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	second, secondProvider, err := embedTexts(modelSettings{}, []string{"sales revenue by category"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if provider != embeddingProviderLocal || secondProvider != embeddingProviderLocal {
		t.Fatalf("expected local embedding provider, got %q and %q", provider, secondProvider)
	}
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("expected one vector from local embedder")
	}
	if len(first[0]) != localEmbeddingDimensions {
		t.Fatalf("expected %d dimensions, got %d", localEmbeddingDimensions, len(first[0]))
	}
	for i := range first[0] {
		if first[0][i] != second[0][i] {
			t.Fatalf("expected deterministic local embeddings")
		}
	}
}

func TestEmbedTextsUsesOpenAICompatibleEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Fatalf("expected /embeddings, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("expected bearer token, got %q", got)
		}
		var req embeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode embedding request: %v", err)
		}
		if req.Model != "text-embedding-test" {
			t.Fatalf("expected embedding model text-embedding-test, got %q", req.Model)
		}
		if len(req.Input) != 2 {
			t.Fatalf("expected 2 embedding inputs, got %d", len(req.Input))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"embedding": []float64{1, 0, 0}},
				{"embedding": []float64{0, 1, 0}},
			},
		})
	}))
	defer server.Close()

	settings := modelSettings{
		Provider:          "openai-compatible",
		BaseURL:           server.URL,
		APIKey:            "secret",
		EmbeddingProvider: "openai-compatible",
		EmbeddingBaseURL:  server.URL,
		EmbeddingAPIKey:   "secret",
		EmbeddingModel:    "text-embedding-test",
	}

	vectors, provider, err := embedTexts(settings, []string{"sales", "customers"})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if provider != embeddingProviderOpenAICompatible {
		t.Fatalf("expected openai-compatible provider, got %q", provider)
	}
	if len(vectors) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vectors))
	}
	if len(vectors[0]) != 3 {
		t.Fatalf("expected 3 dimensions, got %d", len(vectors[0]))
	}
}

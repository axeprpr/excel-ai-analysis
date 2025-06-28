package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRootAndHealthRoutes(t *testing.T) {
	server := newServer(":0", t.TempDir())

	rootReq := httptest.NewRequest(http.MethodGet, "/", nil)
	rootRec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rootRec, rootReq)

	if rootRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rootRec.Code)
	}

	var rootResp map[string]any
	if err := json.Unmarshal(rootRec.Body.Bytes(), &rootResp); err != nil {
		t.Fatalf("failed to decode root response: %v", err)
	}
	if rootResp["service"] != "excel-ai-analysis" {
		t.Fatalf("unexpected service value: %v", rootResp["service"])
	}
	if rootResp["version"] != "dev" {
		t.Fatalf("unexpected version value: %v", rootResp["version"])
	}
	capabilities, ok := rootResp["capabilities"].([]any)
	if !ok || len(capabilities) == 0 {
		t.Fatalf("expected capabilities in root response")
	}
	routes, ok := rootResp["routes"].([]any)
	if !ok || len(routes) < 6 {
		t.Fatalf("expected expanded route list in root response")
	}

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRec := httptest.NewRecorder()
	server.Handler.ServeHTTP(healthRec, healthReq)

	if healthRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, healthRec.Code)
	}
}

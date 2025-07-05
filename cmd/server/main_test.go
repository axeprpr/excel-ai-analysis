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
	config, ok := rootResp["config"].(map[string]any)
	if !ok {
		t.Fatalf("expected config in root response")
	}
	if config["max_request_body_mb"] != float64(256) {
		t.Fatalf("unexpected max_request_body_mb value: %v", config["max_request_body_mb"])
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

	readyReq := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	readyRec := httptest.NewRecorder()
	server.Handler.ServeHTTP(readyRec, readyReq)

	if readyRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, readyRec.Code)
	}

	var readyResp map[string]any
	if err := json.Unmarshal(readyRec.Body.Bytes(), &readyResp); err != nil {
		t.Fatalf("failed to decode ready response: %v", err)
	}
	if readyResp["status"] != "ok" {
		t.Fatalf("unexpected ready status: %v", readyResp["status"])
	}
}

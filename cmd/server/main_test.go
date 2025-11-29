package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRootAndHealthRoutes(t *testing.T) {
	server := newServer(":0", t.TempDir(), "test-version")

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
	if rootResp["version"] != "test-version" {
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
	var healthResp map[string]any
	if err := json.Unmarshal(healthRec.Body.Bytes(), &healthResp); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}
	if healthResp["version"] != "test-version" {
		t.Fatalf("unexpected health version: %v", healthResp["version"])
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
	if readyResp["version"] != "test-version" {
		t.Fatalf("unexpected ready version: %v", readyResp["version"])
	}

	consoleReq := httptest.NewRequest(http.MethodGet, "/console", nil)
	consoleRec := httptest.NewRecorder()
	server.Handler.ServeHTTP(consoleRec, consoleReq)
	if consoleRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, consoleRec.Code)
	}
	if got := consoleRec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html;") {
		t.Fatalf("expected html content-type, got %q", got)
	}
	if len(consoleRec.Body.Bytes()) == 0 {
		t.Fatalf("expected console html body")
	}

	openAPIReq := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	openAPIRec := httptest.NewRecorder()
	server.Handler.ServeHTTP(openAPIRec, openAPIReq)
	if openAPIRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, openAPIRec.Code)
	}

	var openAPIResp map[string]any
	if err := json.Unmarshal(openAPIRec.Body.Bytes(), &openAPIResp); err != nil {
		t.Fatalf("failed to decode openapi response: %v", err)
	}
	if openAPIResp["openapi"] != "3.1.0" {
		t.Fatalf("unexpected openapi version: %v", openAPIResp["openapi"])
	}
	paths, ok := openAPIResp["paths"].(map[string]any)
	if !ok {
		t.Fatalf("expected paths in openapi response")
	}
	if _, ok := paths["/api/chat/upload"]; !ok {
		t.Fatalf("expected /api/chat/upload in openapi paths")
	}
	if _, ok := paths["/api/chat/query"]; !ok {
		t.Fatalf("expected /api/chat/query in openapi paths")
	}
}

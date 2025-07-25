package main

import (
	"embed"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/axeprpr/excel-ai-analysis/internal/api"
)

//go:embed web/console.html
var consoleHTML embed.FS

type healthResponse struct {
	Service string `json:"service"`
	Status  string `json:"status"`
	Version string `json:"version"`
}

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}

	version := os.Getenv("APP_VERSION")
	if version == "" {
		version = "dev"
	}

	server := newServer(addr, dataDir, version)

	log.Printf("server listening on %s", addr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func newServer(addr, dataDir, version string) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service": "excel-ai-analysis",
			"status":  "ok",
			"version": version,
			"config": map[string]any{
				"addr":                addr,
				"data_dir":            dataDir,
				"max_request_body_mb": 256,
			},
			"capabilities": []string{
				"session-isolated sqlite databases",
				"multi-file spreadsheet uploads",
				"csv and xlsx import into sqlite",
				"text-to-sql style query planning",
				"chart-oriented query metadata",
			},
			"routes": []string{
				"GET /",
				"GET /healthz",
				"GET /readyz",
				"GET /api/status",
				"GET /api/sessions",
				"POST /api/sessions",
				"GET /api/sessions/:session_id",
				"GET /api/sessions/:session_id/files",
				"POST /api/sessions/:session_id/files/upload",
				"GET /api/sessions/:session_id/imports",
				"GET /api/sessions/:session_id/imports/:task_id",
				"GET /api/sessions/:session_id/schema",
				"GET /api/sessions/:session_id/database",
				"POST /api/sessions/:session_id/query",
			},
		})
	})
	mux.HandleFunc("/console", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		content, err := consoleHTML.ReadFile("web/console.html")
		if err != nil {
			http.Error(w, "failed to load console", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(content)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(healthResponse{
			Service: "excel-ai-analysis",
			Status:  "ok",
			Version: version,
		})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		status := "ok"
		checks := map[string]string{
			"sqlite3": "ok",
			"data_dir": "ok",
		}

		if _, err := exec.LookPath("sqlite3"); err != nil {
			status = "degraded"
			checks["sqlite3"] = "missing"
		}

		sessionsDir := filepath.Join(dataDir, "sessions")
		if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
			status = "degraded"
			checks["data_dir"] = "unavailable"
		}

		code := http.StatusOK
		if status != "ok" {
			code = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"service": "excel-ai-analysis",
			"status":  status,
			"version": version,
			"checks":  checks,
		})
	})
	mux.Handle("/api/", api.NewHandler(dataDir))

	return &http.Server{
		Addr:              addr,
		Handler:           withRequestLimits(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

func withRequestLimits(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 256<<20)
		next.ServeHTTP(w, r)
	})
}

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type queryRequest struct {
	Question string `json:"question"`
}

type schemaSnapshot struct {
	Tables []tableSchema `json:"tables"`
}

func (h *Handler) handleSessionQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/query")
	if sessionID == "" || strings.Contains(sessionID, "/") {
		http.NotFound(w, r)
		return
	}

	sessionDir := filepath.Join(h.dataDir, "sessions", sessionID)
	meta, err := readSessionMetadata(sessionDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to read session", http.StatusInternalServerError)
		return
	}

	if meta.Status != "ready" {
		http.Error(w, "session is not ready for query", http.StatusConflict)
		return
	}

	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Question) == "" {
		http.Error(w, "question is required", http.StatusBadRequest)
		return
	}

	snapshot, err := readSchemaSnapshot(sessionDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "schema snapshot not found", http.StatusConflict)
			return
		}
		http.Error(w, "failed to read schema snapshot", http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	meta.LastAccessedAt = now
	meta.UpdatedAt = now
	if err := writeSessionMetadata(sessionDir, meta); err != nil {
		http.Error(w, "failed to update session metadata", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"question":   req.Question,
		"sql":        buildPlaceholderSQL(meta.Tables),
		"rows":       []map[string]any{},
		"columns":    []string{},
		"summary":    buildQuerySummary(req.Question, snapshot),
		"visualization": map[string]any{
			"type":   suggestVisualization(req.Question),
			"title":  req.Question,
			"x":      "dimension",
			"y":      "value",
			"tables": meta.Tables,
		},
		"warnings": []string{
			"Query execution is currently a placeholder response.",
			"AI text-to-SQL generation is not implemented yet.",
		},
	})
}

func readSchemaSnapshot(sessionDir string) (schemaSnapshot, error) {
	path := filepath.Join(sessionDir, "schema", "tables.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return schemaSnapshot{}, err
	}

	var snapshot schemaSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return schemaSnapshot{}, err
	}
	return snapshot, nil
}

func buildPlaceholderSQL(tables []string) string {
	if len(tables) == 0 {
		return "-- no imported tables available"
	}
	return "SELECT * FROM " + tables[0] + " LIMIT 100;"
}

func buildQuerySummary(question string, snapshot schemaSnapshot) string {
	if len(snapshot.Tables) == 0 {
		return "No imported tables are available for answering the question yet."
	}
	return "Received question: " + question + ". Query planning is currently based on session schema metadata only."
}

func suggestVisualization(question string) string {
	q := strings.ToLower(question)
	switch {
	case strings.Contains(q, "trend"), strings.Contains(q, "按月"), strings.Contains(q, "按天"), strings.Contains(q, "time"):
		return "line"
	case strings.Contains(q, "top"), strings.Contains(q, "排名"), strings.Contains(q, "最高"), strings.Contains(q, "lowest"):
		return "bar"
	case strings.Contains(q, "占比"), strings.Contains(q, "比例"), strings.Contains(q, "share"):
		return "pie"
	default:
		return "table"
	}
}

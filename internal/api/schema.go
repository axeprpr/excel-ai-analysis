package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (h *Handler) handleSessionSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/schema")
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
		"status":     meta.Status,
		"tables":     snapshot.Tables,
	})
}

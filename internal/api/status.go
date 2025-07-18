package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
)

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionsDir := filepath.Join(h.dataDir, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		http.Error(w, "failed to inspect sessions", http.StatusInternalServerError)
		return
	}

	sessionCount := 0
	readySessionCount := 0
	totalUploadedFiles := 0
	totalImportedTables := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		meta, err := readSessionMetadata(filepath.Join(sessionsDir, entry.Name()))
		if err != nil {
			continue
		}

		sessionCount++
		if meta.Status == "ready" {
			readySessionCount++
		}
		totalUploadedFiles += len(meta.UploadedFiles)
		totalImportedTables += len(meta.Tables)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":                "ok",
		"session_count":         sessionCount,
		"ready_session_count":   readySessionCount,
		"uploaded_file_count":   totalUploadedFiles,
		"imported_table_count":  totalImportedTables,
	})
}

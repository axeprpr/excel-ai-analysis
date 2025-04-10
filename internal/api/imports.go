package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (h *Handler) handleImportByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID, taskID, ok := parseImportPath(r.URL.Path)
	if !ok {
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

	task, err := readImportTask(sessionDir, taskID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to read import task", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":     sessionID,
		"task_id":        task.TaskID,
		"status":         task.Status,
		"session_status": meta.Status,
		"file_count":     task.FileCount,
		"file_names":     task.FileNames,
		"tables":         meta.Tables,
	})
}

func parseImportPath(path string) (string, string, bool) {
	const prefix = "/api/sessions/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}

	rest := strings.TrimPrefix(path, prefix)
	parts := strings.Split(rest, "/")
	if len(parts) != 3 || parts[1] != "imports" || parts[0] == "" || parts[2] == "" {
		return "", "", false
	}

	return parts[0], parts[2], true
}

func readImportTask(sessionDir, taskID string) (importTask, error) {
	path := filepath.Join(sessionDir, "imports", taskID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return importTask{}, err
	}

	var task importTask
	if err := json.Unmarshal(data, &task); err != nil {
		return importTask{}, err
	}
	return task, nil
}

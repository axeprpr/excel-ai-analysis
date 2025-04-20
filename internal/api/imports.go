package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (h *Handler) handleImports(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/imports")
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

	tasks, err := readAllImportTasks(sessionDir)
	if err != nil {
		http.Error(w, "failed to read import tasks", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":     sessionID,
		"session_status": meta.Status,
		"tasks":          tasks,
	})
}

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
		"type":           task.Type,
		"status":         task.Status,
		"session_status": meta.Status,
		"created_at":     task.CreatedAt,
		"started_at":     task.StartedAt,
		"finished_at":    task.FinishedAt,
		"updated_at":     task.UpdatedAt,
		"error":          task.Error,
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

func readAllImportTasks(sessionDir string) ([]importTask, error) {
	importDir := filepath.Join(sessionDir, "imports")
	entries, err := os.ReadDir(importDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []importTask{}, nil
		}
		return nil, err
	}

	tasks := make([]importTask, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		task, err := readImportTask(sessionDir, strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}

	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
	})

	return tasks, nil
}

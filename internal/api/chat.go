package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const maxMultipartFormMemory = 256 << 20

type chatQueryRequest struct {
	SessionID string `json:"session_id"`
	Question  string `json:"question"`
	ChartMode string `json:"chart_mode"`
}

func (h *Handler) handleChatUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(maxMultipartFormMemory); err != nil {
		http.Error(w, "invalid multipart form", http.StatusBadRequest)
		return
	}

	sessionID := strings.TrimSpace(r.FormValue("session_id"))
	sessionCreated := false
	if sessionID == "" {
		meta, err := h.createSessionMetadata()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sessionID = meta.SessionID
		if err := writeSessionMetadata(filepath.Join(h.dataDir, "sessions", sessionID), meta); err != nil {
			http.Error(w, "failed to persist session metadata", http.StatusInternalServerError)
			return
		}
		sessionCreated = true
	}

	_, task, savedFiles, err := h.prepareSessionUpload(sessionID, r.MultipartForm)
	if err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			http.Error(w, "session not found", http.StatusNotFound)
		case strings.Contains(err.Error(), "unsupported file type"), strings.Contains(err.Error(), "no files uploaded"):
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	h.processImportTask(sessionID, task.TaskID)

	finalTask, err := readImportTask(filepath.Join(h.dataDir, "sessions", sessionID), task.TaskID)
	if err != nil {
		http.Error(w, "failed to read import task", http.StatusInternalServerError)
		return
	}
	if finalTask.Status != "completed" {
		http.Error(w, "import did not complete successfully", http.StatusConflict)
		return
	}

	response := map[string]any{
		"session_id":      sessionID,
		"session_created": sessionCreated,
		"import": map[string]any{
			"task_id":       finalTask.TaskID,
			"status":        finalTask.Status,
			"file_count":    finalTask.FileCount,
			"file_names":    finalTask.FileNames,
			"warning_count": len(finalTask.Warnings),
			"warnings":      finalTask.Warnings,
		},
		"files": savedFiles,
	}

	question := strings.TrimSpace(r.FormValue("question"))
	if question == "" {
		writeJSON(w, http.StatusOK, response)
		return
	}

	refreshedMeta, err := readSessionMetadata(filepath.Join(h.dataDir, "sessions", sessionID))
	if err != nil {
		http.Error(w, "failed to read refreshed session metadata", http.StatusInternalServerError)
		return
	}

	answer, err := h.executeSessionQuery(
		filepath.Join(h.dataDir, "sessions", sessionID),
		refreshedMeta,
		queryRequest{
			Question:  question,
			ChartMode: r.FormValue("chart_mode"),
		},
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response["answer"] = answer
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) handleChatQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req chatQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.Question = strings.TrimSpace(req.Question)
	if req.SessionID == "" {
		http.Error(w, "session_id is required", http.StatusBadRequest)
		return
	}
	if req.Question == "" {
		http.Error(w, "question is required", http.StatusBadRequest)
		return
	}

	sessionDir := filepath.Join(h.dataDir, "sessions", req.SessionID)
	meta, err := readSessionMetadata(sessionDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to read session", http.StatusInternalServerError)
		return
	}
	if meta.Status != "ready" {
		http.Error(w, "session is not ready for query", http.StatusConflict)
		return
	}

	answer, err := h.executeSessionQuery(sessionDir, meta, queryRequest{
		Question:  req.Question,
		ChartMode: req.ChartMode,
	})
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "schema snapshot not found", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": req.SessionID,
		"answer":     answer,
	})
}

package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxMultipartFormMemory = 256 << 20

type chatQueryRequest struct {
	SessionID   string         `json:"session_id"`
	Question    string         `json:"question"`
	ChartMode   string         `json:"chart_mode"`
	ModelConfig *modelSettings `json:"model_config,omitempty"`
}

type chatUploadURLRequest struct {
	SessionID   string         `json:"session_id"`
	FileURLs    []string       `json:"file_urls"`
	Question    string         `json:"question"`
	ChartMode   string         `json:"chart_mode"`
	ModelConfig *modelSettings `json:"model_config,omitempty"`
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
			Question:    question,
			ChartMode:   r.FormValue("chart_mode"),
			ModelConfig: parseModelConfigFromForm(r.FormValue("model_config")),
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
		Question:    req.Question,
		ChartMode:   req.ChartMode,
		ModelConfig: req.ModelConfig,
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

func (h *Handler) handleChatUploadURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req chatUploadURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(req.FileURLs) == 0 {
		http.Error(w, "file_urls are required", http.StatusBadRequest)
		return
	}

	sessionID := strings.TrimSpace(req.SessionID)
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

	savedFiles, err := h.downloadUploadURLs(sessionID, req.FileURLs)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	fileNames := make([]string, 0, len(savedFiles))
	for _, f := range savedFiles {
		fileNames = append(fileNames, f.Name)
	}

	sessionDir := filepath.Join(h.dataDir, "sessions", sessionID)
	meta, err := readSessionMetadata(sessionDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to read session", http.StatusInternalServerError)
		return
	}
	now := timeNowUTC()
	meta.Status = "importing"
	meta.UpdatedAt = now
	meta.LastAccessedAt = now
	meta.UploadedFiles = appendUnique(meta.UploadedFiles, fileNames...)
	if err := writeSessionMetadata(sessionDir, meta); err != nil {
		http.Error(w, "failed to update session metadata", http.StatusInternalServerError)
		return
	}

	task, err := writeImportTask(sessionDir, sessionID, fileNames, now)
	if err != nil {
		http.Error(w, "failed to create import task", http.StatusInternalServerError)
		return
	}
	if err := syncImportTaskToDatabase(meta.DatabasePath, task); err != nil {
		http.Error(w, "failed to persist import task in database", http.StatusInternalServerError)
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

	question := strings.TrimSpace(req.Question)
	if question == "" {
		writeJSON(w, http.StatusOK, response)
		return
	}
	refreshedMeta, err := readSessionMetadata(filepath.Join(h.dataDir, "sessions", sessionID))
	if err != nil {
		http.Error(w, "failed to read refreshed session metadata", http.StatusInternalServerError)
		return
	}
	answer, err := h.executeSessionQuery(sessionDir, refreshedMeta, queryRequest{
		Question:    question,
		ChartMode:   req.ChartMode,
		ModelConfig: req.ModelConfig,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response["answer"] = answer
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) downloadUploadURLs(sessionID string, fileURLs []string) ([]uploadedFileInfo, error) {
	sessionDir := filepath.Join(h.dataDir, "sessions", sessionID)
	uploadDir := filepath.Join(sessionDir, "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, errors.New("failed to create upload directory")
	}
	client := &http.Client{Timeout: 120 * time.Second}
	files := make([]uploadedFileInfo, 0, len(fileURLs))
	for i, raw := range fileURLs {
		u := strings.TrimSpace(raw)
		if u == "" {
			continue
		}
		parsed, err := neturl.Parse(u)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, errors.New("invalid file url")
		}
		name := filepath.Base(parsed.Path)
		if name == "" || name == "." || name == "/" {
			name = "upload_" + strconv.Itoa(i+1) + ".csv"
		}
		if !isSupportedUploadFile(name) {
			return nil, errors.New("unsupported file type")
		}
		req, _ := http.NewRequest(http.MethodGet, u, nil)
		resp, err := client.Do(req)
		if err != nil {
			return nil, errors.New("failed to download file url")
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return nil, errors.New("failed to download file url")
		}
		var buf bytes.Buffer
		size, err := io.Copy(&buf, resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, errors.New("failed to download file url")
		}
		dst := filepath.Join(uploadDir, name)
		if err := os.WriteFile(dst, buf.Bytes(), 0o644); err != nil {
			return nil, errors.New("failed to save downloaded file")
		}
		files = append(files, uploadedFileInfo{
			Name:      name,
			Extension: strings.ToLower(filepath.Ext(name)),
			Size:      size,
		})
	}
	if len(files) == 0 {
		return nil, errors.New("file_urls are empty")
	}
	return files, nil
}

func parseModelConfigFromForm(raw string) *modelSettings {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var cfg modelSettings
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil
	}
	return &cfg
}

func timeNowUTC() time.Time {
	return time.Now().UTC()
}

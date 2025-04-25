package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type importTask struct {
	TaskID     string     `json:"task_id"`
	SessionID  string     `json:"session_id"`
	Type       string     `json:"type"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
	Error      string     `json:"error,omitempty"`
	FileCount  int        `json:"file_count"`
	FileNames  []string   `json:"file_names"`
}

func (h *Handler) handleSessionUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/files/upload")
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

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "invalid multipart form", http.StatusBadRequest)
		return
	}

	files := collectUploadedFiles(r.MultipartForm.File)
	if len(files) == 0 {
		http.Error(w, "no files uploaded", http.StatusBadRequest)
		return
	}

	uploadDir := filepath.Join(sessionDir, "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		http.Error(w, "failed to create upload directory", http.StatusInternalServerError)
		return
	}

	savedNames := make([]string, 0, len(files))
	for _, fh := range files {
		if !isSupportedUploadFile(fh.Filename) {
			http.Error(w, "unsupported file type", http.StatusBadRequest)
			return
		}

		savedName, err := saveUploadedFile(uploadDir, fh)
		if err != nil {
			http.Error(w, "failed to save uploaded file", http.StatusInternalServerError)
			return
		}
		savedNames = append(savedNames, savedName)
	}

	now := time.Now().UTC()
	meta.Status = "importing"
	meta.UpdatedAt = now
	meta.LastAccessedAt = now
	meta.UploadedFiles = appendUnique(meta.UploadedFiles, savedNames...)
	if err := writeSessionMetadata(sessionDir, meta); err != nil {
		http.Error(w, "failed to update session metadata", http.StatusInternalServerError)
		return
	}

	task, err := writeImportTask(sessionDir, sessionID, savedNames, now)
	if err != nil {
		http.Error(w, "failed to create import task", http.StatusInternalServerError)
		return
	}

	go h.processImportTask(sessionID, task.TaskID)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"session_id":     sessionID,
		"task_id":        task.TaskID,
		"status":         task.Status,
		"session_status": meta.Status,
		"file_count":     len(savedNames),
		"file_names":     savedNames,
		"created_at":     task.CreatedAt,
	})
}

func writeImportTask(sessionDir, sessionID string, fileNames []string, now time.Time) (importTask, error) {
	taskID, err := newTaskID()
	if err != nil {
		return importTask{}, err
	}

	task := importTask{
		TaskID:    taskID,
		SessionID: sessionID,
		Type:      "import",
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
		FileCount: len(fileNames),
		FileNames: fileNames,
	}

	importDir := filepath.Join(sessionDir, "imports")
	if err := os.MkdirAll(importDir, 0o755); err != nil {
		return importTask{}, err
	}

	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return importTask{}, err
	}

	if err := os.WriteFile(filepath.Join(importDir, taskID+".json"), data, 0o644); err != nil {
		return importTask{}, err
	}

	return task, nil
}

func saveUploadedFile(uploadDir string, fh *multipart.FileHeader) (string, error) {
	src, err := fh.Open()
	if err != nil {
		return "", err
	}
	defer src.Close()

	name := filepath.Base(fh.Filename)
	dstPath := filepath.Join(uploadDir, name)
	dst, err := os.Create(dstPath)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", err
	}
	return name, nil
}

func collectUploadedFiles(files map[string][]*multipart.FileHeader) []*multipart.FileHeader {
	var out []*multipart.FileHeader
	for _, group := range files {
		out = append(out, group...)
	}
	return out
}

func appendUnique(existing []string, values ...string) []string {
	seen := make(map[string]struct{}, len(existing))
	for _, item := range existing {
		seen[item] = struct{}{}
	}
	for _, item := range values {
		if _, ok := seen[item]; ok {
			continue
		}
		existing = append(existing, item)
		seen[item] = struct{}{}
	}
	return existing
}

func newTaskID() (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "import_" + strings.ToLower(hex.EncodeToString(buf[:])), nil
}

func isSupportedUploadFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".xlsx", ".xls", ".csv":
		return true
	default:
		return false
	}
}

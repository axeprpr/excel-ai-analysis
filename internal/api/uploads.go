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
	"os/exec"
	"path/filepath"
	"strconv"
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
	Warnings   []string   `json:"warnings,omitempty"`
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
	if err := syncImportTaskToDatabase(meta.DatabasePath, task); err != nil {
		http.Error(w, "failed to persist import task in database", http.StatusInternalServerError)
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
		"warnings":       task.Warnings,
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
		Warnings:  buildImportWarnings(fileNames),
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

func buildImportWarnings(fileNames []string) []string {
	warnings := make([]string, 0, 1)
	for _, name := range fileNames {
		if strings.EqualFold(filepath.Ext(name), ".xls") {
			warnings = append(warnings, ".xls files currently use placeholder import; real parsing is only implemented for .csv and .xlsx.")
			break
		}
	}
	return warnings
}

func syncImportTaskToDatabase(databasePath string, task importTask) error {
	startedAt := ""
	if task.StartedAt != nil {
		startedAt = task.StartedAt.Format(time.RFC3339)
	}

	finishedAt := ""
	if task.FinishedAt != nil {
		finishedAt = task.FinishedAt.Format(time.RFC3339)
	}

	statement := `
INSERT INTO import_tasks(
  task_id, session_id, type, status, created_at, started_at, finished_at, updated_at, error, file_count, file_names
) VALUES(
  ` + sqliteQuote(task.TaskID) + `,
  ` + sqliteQuote(task.SessionID) + `,
  ` + sqliteQuote(task.Type) + `,
  ` + sqliteQuote(task.Status) + `,
  ` + sqliteQuote(task.CreatedAt.Format(time.RFC3339)) + `,
  ` + sqliteQuote(startedAt) + `,
  ` + sqliteQuote(finishedAt) + `,
  ` + sqliteQuote(task.UpdatedAt.Format(time.RFC3339)) + `,
  ` + sqliteQuote(task.Error) + `,
  ` + strconv.Itoa(task.FileCount) + `,
  ` + sqliteQuote(strings.Join(task.FileNames, ",")) + `
) ON CONFLICT(task_id) DO UPDATE SET
  session_id=excluded.session_id,
  type=excluded.type,
  status=excluded.status,
  created_at=excluded.created_at,
  started_at=excluded.started_at,
  finished_at=excluded.finished_at,
  updated_at=excluded.updated_at,
  error=excluded.error,
  file_count=excluded.file_count,
  file_names=excluded.file_names;
`

	return exec.Command("sqlite3", databasePath, statement).Run()
}

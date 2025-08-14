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

type uploadedFileInfo struct {
	Name      string `json:"name"`
	Extension string `json:"extension"`
	Size      int64  `json:"size"`
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

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "invalid multipart form", http.StatusBadRequest)
		return
	}

	meta, task, savedFiles, err := h.prepareSessionUpload(sessionID, r.MultipartForm)
	if err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			http.NotFound(w, r)
		case strings.Contains(err.Error(), "unsupported file type"), strings.Contains(err.Error(), "no files uploaded"):
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	go h.processImportTask(sessionID, task.TaskID)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"session_id":     sessionID,
		"task_id":        task.TaskID,
		"status":         task.Status,
		"session_status": meta.Status,
		"file_count":     len(task.FileNames),
		"file_names":     task.FileNames,
		"files":          savedFiles,
		"warnings":       task.Warnings,
		"created_at":     task.CreatedAt,
	})
}

func (h *Handler) prepareSessionUpload(sessionID string, form *multipart.Form) (sessionMetadata, importTask, []uploadedFileInfo, error) {
	sessionDir := filepath.Join(h.dataDir, "sessions", sessionID)
	meta, err := readSessionMetadata(sessionDir)
	if err != nil {
		return sessionMetadata{}, importTask{}, nil, err
	}

	files := collectUploadedFiles(form.File)
	if len(files) == 0 {
		return sessionMetadata{}, importTask{}, nil, errors.New("no files uploaded")
	}

	uploadDir := filepath.Join(sessionDir, "uploads")
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return sessionMetadata{}, importTask{}, nil, errors.New("failed to create upload directory")
	}

	savedNames := make([]string, 0, len(files))
	savedFiles := make([]uploadedFileInfo, 0, len(files))
	for _, fh := range files {
		if !isSupportedUploadFile(fh.Filename) {
			return sessionMetadata{}, importTask{}, nil, errors.New("unsupported file type")
		}

		savedName, err := saveUploadedFile(uploadDir, fh)
		if err != nil {
			return sessionMetadata{}, importTask{}, nil, errors.New("failed to save uploaded file")
		}
		savedNames = append(savedNames, savedName)
		savedFiles = append(savedFiles, uploadedFileInfo{
			Name:      savedName,
			Extension: strings.ToLower(filepath.Ext(savedName)),
			Size:      fh.Size,
		})
	}

	now := time.Now().UTC()
	meta.Status = "importing"
	meta.UpdatedAt = now
	meta.LastAccessedAt = now
	meta.UploadedFiles = appendUnique(meta.UploadedFiles, savedNames...)
	if err := writeSessionMetadata(sessionDir, meta); err != nil {
		return sessionMetadata{}, importTask{}, nil, errors.New("failed to update session metadata")
	}

	task, err := writeImportTask(sessionDir, sessionID, savedNames, now)
	if err != nil {
		return sessionMetadata{}, importTask{}, nil, errors.New("failed to create import task")
	}
	if err := syncImportTaskToDatabase(meta.DatabasePath, task); err != nil {
		return sessionMetadata{}, importTask{}, nil, errors.New("failed to persist import task in database")
	}

	return meta, task, savedFiles, nil
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

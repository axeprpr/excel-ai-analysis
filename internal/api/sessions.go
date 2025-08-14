package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type sessionMetadata struct {
	SessionID      string    `json:"session_id"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	LastAccessedAt time.Time `json:"last_accessed_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	DatabasePath   string    `json:"database_path"`
	UploadedFiles  []string  `json:"uploaded_files"`
	Tables         []string  `json:"tables"`
}

type sessionResponse struct {
	sessionMetadata
	UploadedFileCount int `json:"uploaded_file_count"`
	TableCount        int `json:"table_count"`
	ImportTaskCount   int `json:"import_task_count"`
	TotalRowCount     int `json:"total_row_count"`
}

func (h *Handler) handleSessionsRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listSessions(w, r)
	case http.MethodPost:
		h.createSession(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	if sessionID == "" || strings.Contains(sessionID, "/") {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		h.getSession(w, r, sessionID)
	case http.MethodDelete:
		h.deleteSession(w, r, sessionID)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (h *Handler) createSession(w http.ResponseWriter, r *http.Request) {
	meta, err := h.createSessionMetadata()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := writeSessionMetadata(filepath.Join(h.dataDir, "sessions", meta.SessionID), meta); err != nil {
		http.Error(w, "failed to persist session metadata", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, buildSessionResponse(meta))
}

func (h *Handler) createSessionMetadata() (sessionMetadata, error) {
	now := time.Now().UTC()
	sessionID, err := newSessionID()
	if err != nil {
		return sessionMetadata{}, errors.New("failed to generate session id")
	}

	sessionDir := filepath.Join(h.dataDir, "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return sessionMetadata{}, errors.New("failed to create session directory")
	}

	meta := sessionMetadata{
		SessionID:      sessionID,
		Status:         "active",
		CreatedAt:      now,
		UpdatedAt:      now,
		LastAccessedAt: now,
		ExpiresAt:      now.Add(72 * time.Hour),
		DatabasePath:   filepath.ToSlash(filepath.Join(sessionDir, "session.db")),
		UploadedFiles:  []string{},
		Tables:         []string{},
	}

	if err := initializeSessionWorkspace(sessionDir, meta.DatabasePath); err != nil {
		return sessionMetadata{}, errors.New("failed to initialize session workspace")
	}

	if err := syncSessionMetaToDatabase(meta); err != nil {
		return sessionMetadata{}, errors.New("failed to initialize session metadata in database")
	}

	return meta, nil
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) {
	sessionsDir := filepath.Join(h.dataDir, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusOK, map[string]any{
				"sessions": []sessionMetadata{},
			})
			return
		}
		http.Error(w, "failed to list sessions", http.StatusInternalServerError)
		return
	}

	sessions := make([]sessionResponse, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		meta, err := readSessionMetadata(filepath.Join(sessionsDir, entry.Name()))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			http.Error(w, "failed to read session metadata", http.StatusInternalServerError)
			return
		}
		sessions = append(sessions, buildSessionResponse(meta))
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"sessions": sessions,
	})
}

func (h *Handler) getSession(w http.ResponseWriter, r *http.Request, sessionID string) {
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

	meta.LastAccessedAt = time.Now().UTC()
	meta.UpdatedAt = meta.LastAccessedAt
	if err := writeSessionMetadata(sessionDir, meta); err != nil {
		http.Error(w, "failed to update session metadata", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, buildSessionResponse(meta))
}

func (h *Handler) deleteSession(w http.ResponseWriter, r *http.Request, sessionID string) {
	sessionDir := filepath.Join(h.dataDir, "sessions", sessionID)
	if _, err := readSessionMetadata(sessionDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "failed to read session", http.StatusInternalServerError)
		return
	}

	if err := os.RemoveAll(sessionDir); err != nil {
		http.Error(w, "failed to delete session", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"session_id": sessionID,
		"status":     "deleted",
	})
}

func newSessionID() (string, error) {
	var buf [4]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "sess_" + strings.ToLower(hex.EncodeToString(buf[:])), nil
}

func readSessionMetadata(sessionDir string) (sessionMetadata, error) {
	path := filepath.Join(sessionDir, "session.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return sessionMetadata{}, err
	}

	var meta sessionMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return sessionMetadata{}, err
	}
	return meta, nil
}

func writeSessionMetadata(sessionDir string, meta sessionMetadata) error {
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(sessionDir, "session.json"), data, 0o644)
}

func buildSessionResponse(meta sessionMetadata) sessionResponse {
	resp := sessionResponse{
		sessionMetadata:    meta,
		UploadedFileCount:  len(meta.UploadedFiles),
		TableCount:         len(meta.Tables),
		ImportTaskCount:    0,
		TotalRowCount:      0,
	}

	if meta.DatabasePath == "" {
		return resp
	}

	if tasks, err := readImportTaskCatalogFromDatabase(meta.DatabasePath); err == nil {
		resp.ImportTaskCount = len(tasks)
	}
	if catalog, err := readSchemaCatalogFromDatabase(meta.DatabasePath); err == nil {
		totalRowCount := 0
		for _, table := range catalog {
			totalRowCount += table.RowCount
		}
		resp.TableCount = len(catalog)
		resp.TotalRowCount = totalRowCount
	}

	return resp
}

func initializeSessionWorkspace(sessionDir, databasePath string) error {
	for _, dir := range []string{
		sessionDir,
		filepath.Join(sessionDir, "uploads"),
		filepath.Join(sessionDir, "imports"),
		filepath.Join(sessionDir, "schema"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	file, err := os.OpenFile(databasePath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	return initializeSessionDatabase(databasePath)
}

func initializeSessionDatabase(databasePath string) error {
	cmd := exec.Command(
		"sqlite3",
		databasePath,
		`
CREATE TABLE IF NOT EXISTS session_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS import_tasks (
  task_id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  type TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  started_at TEXT,
  finished_at TEXT,
  updated_at TEXT NOT NULL,
  error TEXT,
  file_count INTEGER NOT NULL,
  file_names TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS imported_tables (
  table_name TEXT PRIMARY KEY,
  source_file TEXT NOT NULL,
  source_sheet TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS imported_columns (
  table_name TEXT NOT NULL,
  column_name TEXT NOT NULL,
  column_type TEXT NOT NULL,
  semantic TEXT NOT NULL,
  PRIMARY KEY(table_name, column_name)
);
`,
	)
	return cmd.Run()
}

func syncSessionMetaToDatabase(meta sessionMetadata) error {
	statements := []string{
		sqliteUpsert("session_id", meta.SessionID),
		sqliteUpsert("status", meta.Status),
		sqliteUpsert("created_at", meta.CreatedAt.Format(time.RFC3339)),
		sqliteUpsert("updated_at", meta.UpdatedAt.Format(time.RFC3339)),
		sqliteUpsert("last_accessed_at", meta.LastAccessedAt.Format(time.RFC3339)),
		sqliteUpsert("expires_at", meta.ExpiresAt.Format(time.RFC3339)),
		sqliteUpsert("database_path", meta.DatabasePath),
		sqliteUpsert("uploaded_files", strings.Join(meta.UploadedFiles, ",")),
		sqliteUpsert("tables", strings.Join(meta.Tables, ",")),
	}

	cmd := exec.Command("sqlite3", meta.DatabasePath, strings.Join(statements, "\n"))
	return cmd.Run()
}

func sqliteUpsert(key, value string) string {
	return "INSERT INTO session_meta(key, value) VALUES(" +
		sqliteQuote(key) + ", " + sqliteQuote(value) + ") " +
		"ON CONFLICT(key) DO UPDATE SET value=excluded.value;"
}

func sqliteQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func execSQLite(databasePath, statement string) error {
	return exec.Command("sqlite3", databasePath, statement).Run()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

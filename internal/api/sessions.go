package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
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

func (h *Handler) handleSessionsRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
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
	now := time.Now().UTC()
	sessionID, err := newSessionID()
	if err != nil {
		http.Error(w, "failed to generate session id", http.StatusInternalServerError)
		return
	}

	sessionDir := filepath.Join(h.dataDir, "sessions", sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		http.Error(w, "failed to create session directory", http.StatusInternalServerError)
		return
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
		http.Error(w, "failed to initialize session workspace", http.StatusInternalServerError)
		return
	}

	if err := writeSessionMetadata(sessionDir, meta); err != nil {
		http.Error(w, "failed to persist session metadata", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, meta)
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

	writeJSON(w, http.StatusOK, meta)
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
	return file.Close()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

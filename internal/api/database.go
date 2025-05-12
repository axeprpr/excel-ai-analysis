package api

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (h *Handler) handleSessionDatabase(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/database")
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

	info, err := os.Stat(meta.DatabasePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "session database not found", http.StatusConflict)
			return
		}
		http.Error(w, "failed to inspect session database", http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	meta.LastAccessedAt = now
	meta.UpdatedAt = now
	if err := writeSessionMetadata(sessionDir, meta); err != nil {
		http.Error(w, "failed to update session metadata", http.StatusInternalServerError)
		return
	}

	sqliteTables, err := listSQLiteTables(meta.DatabasePath)
	if err != nil {
		http.Error(w, "failed to inspect sqlite tables", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id":    sessionID,
		"status":        meta.Status,
		"database_path": meta.DatabasePath,
		"database_size": info.Size(),
		"modified_at":   info.ModTime().UTC(),
		"tables":        meta.Tables,
		"sqlite_tables": sqliteTables,
	})
}

func listSQLiteTables(databasePath string) ([]string, error) {
	var output []byte
	var err error
	for i := 0; i < 3; i++ {
		output, err = exec.Command(
			"sqlite3",
			databasePath,
			"PRAGMA busy_timeout = 2000; SELECT name FROM sqlite_master WHERE type='table' ORDER BY name;",
		).Output()
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		return nil, err
	}

	lines := bytes.Split(bytes.TrimSpace(output), []byte("\n"))
	if len(lines) == 1 && len(lines[0]) == 0 {
		return []string{}, nil
	}

	tables := make([]string, 0, len(lines))
	for _, line := range lines {
		name := string(bytes.TrimSpace(line))
		if name == "" {
			continue
		}
		tables = append(tables, name)
	}
	return tables, nil
}

package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type sessionFile struct {
	Name         string    `json:"name"`
	Extension    string    `json:"extension"`
	Size         int64     `json:"size"`
	ModifiedAt   time.Time `json:"modified_at"`
	RelativePath string    `json:"relative_path"`
}

func (h *Handler) handleSessionFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/files")
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

	files, err := listSessionFiles(sessionDir)
	if err != nil {
		http.Error(w, "failed to list session files", http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	meta.LastAccessedAt = now
	meta.UpdatedAt = now
	if err := writeSessionMetadata(sessionDir, meta); err != nil {
		http.Error(w, "failed to update session metadata", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"status":     meta.Status,
		"files":      files,
		"file_count": len(files),
		"total_size": totalFileSize(files),
	})
}

func listSessionFiles(sessionDir string) ([]sessionFile, error) {
	uploadDir := filepath.Join(sessionDir, "uploads")
	entries, err := os.ReadDir(uploadDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []sessionFile{}, nil
		}
		return nil, err
	}

	files := make([]sessionFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return nil, err
		}

		files = append(files, sessionFile{
			Name:         entry.Name(),
			Extension:    strings.ToLower(filepath.Ext(entry.Name())),
			Size:         info.Size(),
			ModifiedAt:   info.ModTime().UTC(),
			RelativePath: filepath.ToSlash(filepath.Join("uploads", entry.Name())),
		})
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].ModifiedAt.After(files[j].ModifiedAt)
	})

	return files, nil
}

func totalFileSize(files []sessionFile) int64 {
	var total int64
	for _, file := range files {
		total += file.Size
	}
	return total
}

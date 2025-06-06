package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type databaseCatalogTable struct {
	TableName   string         `json:"table_name"`
	SourceFile  string         `json:"source_file"`
	SourceSheet string         `json:"source_sheet"`
	RowCount    int            `json:"row_count"`
	Columns     []schemaColumn `json:"columns"`
	PreviewRows []map[string]any `json:"preview_rows"`
}

type databaseImportTask struct {
	TaskID     string `json:"task_id"`
	Status     string `json:"status"`
	Type       string `json:"type"`
	FileCount  int    `json:"file_count"`
	FileNames  string `json:"file_names"`
	UpdatedAt  string `json:"updated_at"`
}

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

	catalog, err := readSchemaCatalogFromDatabase(meta.DatabasePath)
	if err != nil {
		http.Error(w, "failed to read schema catalog from sqlite", http.StatusInternalServerError)
		return
	}

	importTasks, err := readImportTaskCatalogFromDatabase(meta.DatabasePath)
	if err != nil {
		http.Error(w, "failed to read import tasks from sqlite", http.StatusInternalServerError)
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
		"catalog":       catalog,
		"import_tasks":  importTasks,
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

func readSchemaCatalogFromDatabase(databasePath string) ([]databaseCatalogTable, error) {
	output, err := sqliteQuery(databasePath, `
SELECT table_name, source_file, source_sheet
FROM imported_tables
ORDER BY table_name;
`)
	if err != nil {
		return nil, err
	}

	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 {
		return []databaseCatalogTable{}, nil
	}

	var catalog []databaseCatalogTable
	if err := json.Unmarshal(trimmed, &catalog); err != nil {
		return nil, err
	}

	for i := range catalog {
		rowCountOutput, err := sqliteQueryText(databasePath, `SELECT COUNT(*) FROM `+sqliteIdent(catalog[i].TableName)+`;`)
		if err != nil {
			return nil, err
		}
		if _, err := fmt.Sscanf(strings.TrimSpace(string(rowCountOutput)), "%d", &catalog[i].RowCount); err != nil {
			return nil, err
		}

		columnOutput, err := sqliteQuery(databasePath, `
SELECT column_name AS name, column_type AS type, semantic
FROM imported_columns
WHERE table_name = `+sqliteQuote(catalog[i].TableName)+`
ORDER BY column_name;
`)
		if err != nil {
			return nil, err
		}

		columnTrimmed := bytes.TrimSpace(columnOutput)
		if len(columnTrimmed) == 0 {
			catalog[i].Columns = []schemaColumn{}
			continue
		}

		if err := json.Unmarshal(columnTrimmed, &catalog[i].Columns); err != nil {
			return nil, err
		}

		previewOutput, err := sqliteQuery(databasePath, `SELECT * FROM `+sqliteIdent(catalog[i].TableName)+` LIMIT 3;`)
		if err != nil {
			return nil, err
		}
		previewTrimmed := bytes.TrimSpace(previewOutput)
		if len(previewTrimmed) == 0 {
			catalog[i].PreviewRows = []map[string]any{}
			continue
		}
		if err := json.Unmarshal(previewTrimmed, &catalog[i].PreviewRows); err != nil {
			return nil, err
		}
	}

	return catalog, nil
}

func sqliteQuery(databasePath, sql string) ([]byte, error) {
	var output []byte
	var err error
	for i := 0; i < 3; i++ {
		output, err = exec.Command(
			"sqlite3",
			"-cmd", ".timeout 2000",
			"-json",
			databasePath,
			sql,
		).Output()
		if err == nil {
			return output, nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil, err
}

func sqliteQueryText(databasePath, sql string) ([]byte, error) {
	var output []byte
	var err error
	for i := 0; i < 3; i++ {
		output, err = exec.Command(
			"sqlite3",
			"-cmd", ".timeout 2000",
			databasePath,
			sql,
		).Output()
		if err == nil {
			return output, nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil, err
}

func readImportTaskCatalogFromDatabase(databasePath string) ([]databaseImportTask, error) {
	output, err := sqliteQuery(databasePath, `
SELECT task_id, status, type, file_count, file_names, updated_at
FROM import_tasks
ORDER BY updated_at DESC;
`)
	if err != nil {
		return nil, err
	}

	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 {
		return []databaseImportTask{}, nil
	}

	var tasks []databaseImportTask
	if err := json.Unmarshal(trimmed, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

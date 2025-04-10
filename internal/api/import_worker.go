package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type tableSchema struct {
	TableName   string   `json:"table_name"`
	SourceFile  string   `json:"source_file"`
	SourceSheet string   `json:"source_sheet"`
	Columns     []string `json:"columns"`
}

func (h *Handler) processImportTask(sessionID, taskID string) {
	sessionDir := filepath.Join(h.dataDir, "sessions", sessionID)

	task, err := readImportTask(sessionDir, taskID)
	if err != nil {
		return
	}
	task.Status = "running"
	task.UpdatedAt = time.Now().UTC()
	if err := writeImportTaskFile(sessionDir, task); err != nil {
		return
	}

	meta, err := readSessionMetadata(sessionDir)
	if err != nil {
		return
	}

	tables := make([]string, 0, len(task.FileNames))
	schemas := make([]tableSchema, 0, len(task.FileNames))
	for _, fileName := range task.FileNames {
		tableName := deriveTableName(fileName)
		tables = append(tables, tableName)
		schemas = append(schemas, tableSchema{
			TableName:   tableName,
			SourceFile:  fileName,
			SourceSheet: "sheet1",
			Columns:     []string{"column_1", "column_2", "column_3"},
		})
	}

	if err := writeSchemaSnapshot(sessionDir, schemas); err != nil {
		task.Status = "failed"
		task.UpdatedAt = time.Now().UTC()
		_ = writeImportTaskFile(sessionDir, task)
		return
	}

	now := time.Now().UTC()
	meta.Status = "ready"
	meta.Tables = appendUnique(meta.Tables, tables...)
	meta.UpdatedAt = now
	meta.LastAccessedAt = now
	if err := writeSessionMetadata(sessionDir, meta); err != nil {
		task.Status = "failed"
		task.UpdatedAt = now
		_ = writeImportTaskFile(sessionDir, task)
		return
	}

	task.Status = "completed"
	task.UpdatedAt = now
	_ = writeImportTaskFile(sessionDir, task)
}

func writeImportTaskFile(sessionDir string, task importTask) error {
	importDir := filepath.Join(sessionDir, "imports")
	if err := os.MkdirAll(importDir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(task, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(importDir, task.TaskID+".json"), data, 0o644)
}

func writeSchemaSnapshot(sessionDir string, schemas []tableSchema) error {
	schemaDir := filepath.Join(sessionDir, "schema")
	if err := os.MkdirAll(schemaDir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(map[string]any{
		"tables": schemas,
	}, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(schemaDir, "tables.json"), data, 0o644)
}

func deriveTableName(fileName string) string {
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(fileName)), filepath.Ext(fileName))
	var b strings.Builder
	lastUnderscore := false
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}

	name := strings.Trim(b.String(), "_")
	if name == "" {
		return "table_1"
	}
	return name
}

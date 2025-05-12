package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type tableSchema struct {
	TableName   string         `json:"table_name"`
	SourceFile  string         `json:"source_file"`
	SourceSheet string         `json:"source_sheet"`
	Columns     []schemaColumn `json:"columns"`
}

type schemaColumn struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Semantic string `json:"semantic"`
}

func (h *Handler) processImportTask(sessionID, taskID string) {
	sessionDir := filepath.Join(h.dataDir, "sessions", sessionID)

	task, err := readImportTask(sessionDir, taskID)
	if err != nil {
		return
	}
	startedAt := time.Now().UTC()
	task.Status = "running"
	task.StartedAt = &startedAt
	task.UpdatedAt = startedAt
	task.Error = ""
	if err := writeImportTaskFile(sessionDir, task); err != nil {
		return
	}

	meta, err := readSessionMetadata(sessionDir)
	if err != nil {
		markTaskFailed(sessionDir, task, "failed to read session metadata")
		return
	}
	if err := syncImportTaskToDatabase(meta.DatabasePath, task); err != nil {
		markTaskFailed(sessionDir, task, "failed to sync import task to database")
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
			Columns:     derivePlaceholderColumns(fileName),
		})
	}

	if err := writeSchemaSnapshot(sessionDir, schemas); err != nil {
		markTaskFailed(sessionDir, task, "failed to write schema snapshot")
		return
	}
	if err := syncSchemaToDatabase(meta.DatabasePath, schemas); err != nil {
		markTaskFailed(sessionDir, task, "failed to sync schema to database")
		return
	}

	now := time.Now().UTC()
	meta.Status = "ready"
	meta.Tables = appendUnique(meta.Tables, tables...)
	meta.UpdatedAt = now
	meta.LastAccessedAt = now
	if err := writeSessionMetadata(sessionDir, meta); err != nil {
		markTaskFailed(sessionDir, task, "failed to update session metadata")
		return
	}
	if err := syncSessionMetaToDatabase(meta); err != nil {
		markTaskFailed(sessionDir, task, "failed to sync session metadata to database")
		return
	}

	task.Status = "completed"
	task.FinishedAt = &now
	task.UpdatedAt = now
	task.Error = ""
	_ = writeImportTaskFile(sessionDir, task)
	_ = syncImportTaskToDatabase(meta.DatabasePath, task)
}

func markTaskFailed(sessionDir string, task importTask, message string) {
	now := time.Now().UTC()
	task.Status = "failed"
	task.FinishedAt = &now
	task.UpdatedAt = now
	task.Error = message
	_ = writeImportTaskFile(sessionDir, task)
	if meta, err := readSessionMetadata(sessionDir); err == nil {
		_ = syncImportTaskToDatabase(meta.DatabasePath, task)
	}
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

func syncSchemaToDatabase(databasePath string, schemas []tableSchema) error {
	statements := []string{
		"DELETE FROM imported_columns;",
		"DELETE FROM imported_tables;",
	}

	for _, table := range schemas {
		statements = append(statements,
			"INSERT INTO imported_tables(table_name, source_file, source_sheet) VALUES("+
				sqliteQuote(table.TableName)+", "+
				sqliteQuote(table.SourceFile)+", "+
				sqliteQuote(table.SourceSheet)+");",
		)

		for _, column := range table.Columns {
			statements = append(statements,
				"INSERT INTO imported_columns(table_name, column_name, column_type, semantic) VALUES("+
					sqliteQuote(table.TableName)+", "+
					sqliteQuote(column.Name)+", "+
					sqliteQuote(column.Type)+", "+
					sqliteQuote(column.Semantic)+");",
			)
		}
	}

	return execSQLite(databasePath, strings.Join(statements, "\n"))
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

func derivePlaceholderColumns(fileName string) []schemaColumn {
	tableName := deriveTableName(fileName)
	switch {
	case strings.Contains(tableName, "sale"), strings.Contains(tableName, "order"), strings.Contains(tableName, "revenue"):
		return []schemaColumn{
			{Name: "order_date", Type: "DATE", Semantic: "time"},
			{Name: "category", Type: "TEXT", Semantic: "dimension"},
			{Name: "amount", Type: "REAL", Semantic: "metric"},
		}
	case strings.Contains(tableName, "customer"), strings.Contains(tableName, "client"), strings.Contains(tableName, "user"):
		return []schemaColumn{
			{Name: "customer_name", Type: "TEXT", Semantic: "dimension"},
			{Name: "region", Type: "TEXT", Semantic: "dimension"},
			{Name: "created_at", Type: "DATETIME", Semantic: "time"},
		}
	case strings.Contains(tableName, "product"), strings.Contains(tableName, "item"):
		return []schemaColumn{
			{Name: "product_name", Type: "TEXT", Semantic: "dimension"},
			{Name: "category", Type: "TEXT", Semantic: "dimension"},
			{Name: "price", Type: "REAL", Semantic: "metric"},
		}
	default:
		return []schemaColumn{
			{Name: "column_1", Type: "TEXT", Semantic: "dimension"},
			{Name: "column_2", Type: "TEXT", Semantic: "dimension"},
			{Name: "column_3", Type: "REAL", Semantic: "metric"},
		}
	}
}

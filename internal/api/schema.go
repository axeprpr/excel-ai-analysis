package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type schemaTableResponse struct {
	TableName      string                 `json:"table_name"`
	SourceFile     string                 `json:"source_file"`
	SourceSheet    string                 `json:"source_sheet"`
	RowCount       int                    `json:"row_count"`
	Columns        []schemaColumn         `json:"columns"`
	ColumnCount    int                    `json:"column_count"`
	SemanticCounts map[string]int         `json:"semantic_counts"`
}

func (h *Handler) handleSessionSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/schema")
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

	snapshot, err := readSchemaSnapshot(sessionDir)
	catalog, catalogErr := readSchemaCatalogFromDatabase(meta.DatabasePath)
	if catalogErr != nil {
		http.Error(w, "failed to read schema catalog", http.StatusInternalServerError)
		return
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		http.Error(w, "failed to read schema snapshot", http.StatusInternalServerError)
		return
	}
	if len(catalog) == 0 && err != nil {
		http.Error(w, "schema snapshot not found", http.StatusConflict)
		return
	}

	now := time.Now().UTC()
	meta.LastAccessedAt = now
	meta.UpdatedAt = now
	if err := writeSessionMetadata(sessionDir, meta); err != nil {
		http.Error(w, "failed to update session metadata", http.StatusInternalServerError)
		return
	}

	if len(catalog) > 0 {
		tables := make([]schemaTableResponse, 0, len(catalog))
		for _, table := range catalog {
			tables = append(tables, buildSchemaTableResponse(tableSchema{
				TableName:   table.TableName,
				SourceFile:  table.SourceFile,
				SourceSheet: table.SourceSheet,
				Columns:     table.Columns,
			}, table.RowCount))
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"session_id": sessionID,
			"status":     meta.Status,
			"table_count": len(tables),
			"tables":     tables,
		})
		return
	}

	tables := make([]schemaTableResponse, 0, len(snapshot.Tables))
	for _, table := range snapshot.Tables {
		tables = append(tables, buildSchemaTableResponse(table, 0))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"status":     meta.Status,
		"table_count": len(tables),
		"tables":     tables,
	})
}

func buildSchemaTableResponse(table tableSchema, rowCount int) schemaTableResponse {
	return schemaTableResponse{
		TableName:      table.TableName,
		SourceFile:     table.SourceFile,
		SourceSheet:    table.SourceSheet,
		RowCount:       rowCount,
		Columns:        table.Columns,
		ColumnCount:    len(table.Columns),
		SemanticCounts: schemaSemanticCounts(table.Columns),
	}
}

func schemaSemanticCounts(columns []schemaColumn) map[string]int {
	counts := make(map[string]int)
	for _, column := range columns {
		key := column.Semantic
		if key == "" {
			key = "unknown"
		}
		counts[key]++
	}
	return counts
}

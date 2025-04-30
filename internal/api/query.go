package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type queryRequest struct {
	Question string `json:"question"`
}

type schemaSnapshot struct {
	Tables []tableSchema `json:"tables"`
}

func (h *Handler) handleSessionQuery(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/sessions/"), "/query")
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

	if meta.Status != "ready" {
		http.Error(w, "session is not ready for query", http.StatusConflict)
		return
	}

	var req queryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Question) == "" {
		http.Error(w, "question is required", http.StatusBadRequest)
		return
	}

	snapshot, err := readSchemaSnapshot(sessionDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "schema snapshot not found", http.StatusConflict)
			return
		}
		http.Error(w, "failed to read schema snapshot", http.StatusInternalServerError)
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
		"question":   req.Question,
		"sql":        buildPlaceholderSQL(snapshot),
		"rows":       buildPlaceholderRows(snapshot, req.Question),
		"columns":    buildQueryColumns(snapshot),
		"summary":    buildQuerySummary(req.Question, snapshot),
		"visualization": map[string]any{
			"type":   suggestVisualization(req.Question),
			"title":  req.Question,
			"x":      pickVisualizationX(snapshot),
			"y":      pickVisualizationY(snapshot),
			"tables": meta.Tables,
		},
		"warnings": []string{
			"Query execution is currently a placeholder response.",
			"AI text-to-SQL generation is not implemented yet.",
		},
	})
}

func readSchemaSnapshot(sessionDir string) (schemaSnapshot, error) {
	path := filepath.Join(sessionDir, "schema", "tables.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return schemaSnapshot{}, err
	}

	var snapshot schemaSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return schemaSnapshot{}, err
	}
	return snapshot, nil
}

func buildPlaceholderSQL(snapshot schemaSnapshot) string {
	if len(snapshot.Tables) == 0 {
		return "-- no imported tables available"
	}

	table := snapshot.Tables[0]
	if len(table.Columns) >= 2 {
		return "SELECT " + table.Columns[0].Name + ", " + table.Columns[1].Name + " FROM " + table.TableName + " LIMIT 100;"
	}
	return "SELECT * FROM " + table.TableName + " LIMIT 100;"
}

func buildQuerySummary(question string, snapshot schemaSnapshot) string {
	if len(snapshot.Tables) == 0 {
		return "No imported tables are available for answering the question yet."
	}
	return "Received question: " + question + ". Query planning is currently based on session schema metadata from " + snapshot.Tables[0].TableName + "."
}

func buildQueryColumns(snapshot schemaSnapshot) []string {
	if len(snapshot.Tables) == 0 {
		return []string{}
	}

	columns := make([]string, 0, len(snapshot.Tables[0].Columns))
	for _, column := range snapshot.Tables[0].Columns {
		columns = append(columns, column.Name)
	}
	return columns
}

func buildPlaceholderRows(snapshot schemaSnapshot, question string) []map[string]any {
	if len(snapshot.Tables) == 0 || len(snapshot.Tables[0].Columns) == 0 {
		return []map[string]any{}
	}

	columns := snapshot.Tables[0].Columns
	row := make(map[string]any, len(columns))
	for i, column := range columns {
		switch {
		case isMetricColumn(column):
			row[column.Name] = 12345 + i
		case isTimeColumn(column):
			row[column.Name] = "2025-04-01"
		default:
			row[column.Name] = placeholderDimensionValue(column.Name, question)
		}
	}
	return []map[string]any{row}
}

func pickVisualizationX(snapshot schemaSnapshot) string {
	if len(snapshot.Tables) == 0 || len(snapshot.Tables[0].Columns) == 0 {
		return "dimension"
	}

	for _, column := range snapshot.Tables[0].Columns {
		if !isMetricColumn(column) {
			return column.Name
		}
	}
	return snapshot.Tables[0].Columns[0].Name
}

func pickVisualizationY(snapshot schemaSnapshot) string {
	if len(snapshot.Tables) == 0 || len(snapshot.Tables[0].Columns) == 0 {
		return "value"
	}

	for _, column := range snapshot.Tables[0].Columns {
		if isMetricColumn(column) {
			return column.Name
		}
	}
	return snapshot.Tables[0].Columns[0].Name
}

func isMetricColumn(column schemaColumn) bool {
	if column.Semantic == "metric" {
		return true
	}

	name := strings.ToLower(column.Name)
	return strings.Contains(name, "amount") ||
		strings.Contains(name, "revenue") ||
		strings.Contains(name, "price") ||
		strings.Contains(name, "total") ||
		strings.Contains(name, "count")
}

func isTimeColumn(column schemaColumn) bool {
	if column.Semantic == "time" {
		return true
	}

	name := strings.ToLower(column.Name)
	return strings.Contains(name, "date") ||
		strings.Contains(name, "time") ||
		strings.Contains(name, "created_at") ||
		strings.Contains(name, "month")
}

func placeholderDimensionValue(column, question string) string {
	column = strings.ToLower(column)
	switch {
	case strings.Contains(column, "category"):
		return "sample_category"
	case strings.Contains(column, "region"):
		return "east"
	case strings.Contains(column, "customer"):
		return "sample_customer"
	case strings.Contains(column, "product"):
		return "sample_product"
	default:
		if strings.TrimSpace(question) == "" {
			return "sample_value"
		}
		return "sample_value"
	}
}

func suggestVisualization(question string) string {
	q := strings.ToLower(question)
	switch {
	case strings.Contains(q, "trend"), strings.Contains(q, "按月"), strings.Contains(q, "按天"), strings.Contains(q, "time"):
		return "line"
	case strings.Contains(q, "top"), strings.Contains(q, "排名"), strings.Contains(q, "最高"), strings.Contains(q, "lowest"):
		return "bar"
	case strings.Contains(q, "占比"), strings.Contains(q, "比例"), strings.Contains(q, "share"):
		return "pie"
	default:
		return "table"
	}
}

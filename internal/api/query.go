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
	"sort"
	"strings"
	"strconv"
	"time"
)

type queryRequest struct {
	Question  string `json:"question"`
	ChartMode string `json:"chart_mode"`
}

type schemaSnapshot struct {
	Tables []tableSchema `json:"tables"`
}

type queryPlan struct {
	SourceTable     string   `json:"source_table"`
	SourceFile      string   `json:"source_file"`
	SourceSheet     string   `json:"source_sheet"`
	SelectedColumns []string `json:"selected_columns"`
	Filters         []string `json:"filters"`
	Question        string   `json:"question"`
	ChartType       string   `json:"chart_type"`
	Mode            string   `json:"mode"`
	SQL             string   `json:"sql"`
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
	settings, err := h.readModelSettings()
	if err != nil {
		http.Error(w, "failed to read model settings", http.StatusInternalServerError)
		return
	}
	chartMode := normalizeChartMode(req.ChartMode)
	if chartMode == "" {
		chartMode = normalizeChartMode(settings.DefaultChartMode)
	}
	if chartMode == "" {
		chartMode = "data"
	}

	snapshot, err := loadSchemaForQuery(sessionDir, meta.DatabasePath)
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

	plan := buildQueryPlan(snapshot, req.Question)
	rows, columns, executed := executeQueryIfPossible(meta.DatabasePath, plan.SQL, plan.SelectedColumns)
	if !executed || len(columns) == 0 || len(rows) == 0 {
		rows = buildPlaceholderRows(snapshot, req.Question)
		columns = buildQueryColumns(snapshot)
		executed = false
	}
	visualization := map[string]any{
		"type":             suggestVisualization(req.Question),
		"title":            req.Question,
		"x":                pickVisualizationX(snapshot),
		"y":                pickVisualizationY(snapshot),
		"series":           pickVisualizationSeries(plan, snapshot),
		"preferred_format": preferredVisualizationFormat(plan),
		"source_table":     plan.SourceTable,
		"tables":           meta.Tables,
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"question":   req.Question,
		"sql":        plan.SQL,
		"rows":       rows,
		"columns":    columns,
		"row_count":  len(rows),
		"executed":   executed,
		"chart_mode": chartMode,
		"summary":    buildQuerySummary(plan, executed, len(rows)),
		"query_plan": plan,
		"visualization": visualization,
		"chart":         buildChartOutput(chartMode, settings, plan, visualization, columns, rows),
		"warnings": queryWarnings(plan, executed),
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

func loadSchemaForQuery(sessionDir, databasePath string) (schemaSnapshot, error) {
	catalog, err := readSchemaCatalogFromDatabase(databasePath)
	if err == nil && len(catalog) > 0 {
		tables := make([]tableSchema, 0, len(catalog))
		for _, table := range catalog {
			tables = append(tables, tableSchema{
				TableName:   table.TableName,
				SourceFile:  table.SourceFile,
				SourceSheet: table.SourceSheet,
				Columns:     table.Columns,
			})
		}
		return schemaSnapshot{Tables: tables}, nil
	}

	return readSchemaSnapshot(sessionDir)
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

func buildQuerySummary(plan queryPlan, executed bool, rowCount int) string {
	if plan.SourceTable == "" {
		return "No imported tables are available for answering the question yet."
	}
	status := "used placeholder rows"
	if executed {
		status = "executed against SQLite"
	}
	source := plan.SourceTable
	if plan.SourceFile != "" {
		source += " from " + plan.SourceFile
	}
	if plan.SourceSheet != "" {
		source += " (" + plan.SourceSheet + ")"
	}
	return "Query on " + source + " ran in " + plan.Mode + " mode and " + status + ", returning " + strconv.Itoa(rowCount) + " row(s)."
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

func executeQueryIfPossible(databasePath, sql string, orderedColumns []string) ([]map[string]any, []string, bool) {
	output, err := exec.Command(
		"sqlite3",
		"-cmd", ".timeout 2000",
		"-header",
		"-json",
		databasePath,
		sql,
	).Output()
	if err != nil {
		return nil, nil, false
	}

	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 {
		return []map[string]any{}, []string{}, true
	}

	var rows []map[string]any
	if err := json.Unmarshal(trimmed, &rows); err != nil {
		return nil, nil, false
	}

	columns := append([]string{}, orderedColumns...)
	if len(columns) == 0 && len(rows) > 0 {
		for key := range rows[0] {
			columns = append(columns, key)
		}
		sort.Strings(columns)
	}
	return rows, columns, true
}

func queryWarning(executed bool) string {
	if executed {
		return "Query executed against the local SQLite session database."
	}
	return "Query execution fell back to placeholder response data."
}

func queryWarnings(plan queryPlan, executed bool) []string {
	warnings := []string{
		queryWarning(executed),
		"AI text-to-SQL generation is not implemented yet.",
	}
	if strings.EqualFold(filepath.Ext(plan.SourceFile), ".xls") {
		warnings = append(warnings, "This query is based on placeholder schema for a legacy .xls upload.")
	}
	return warnings
}

func buildQueryPlan(snapshot schemaSnapshot, question string) queryPlan {
	if len(snapshot.Tables) == 0 {
		return queryPlan{
			SourceTable:     "",
			SourceFile:      "",
			SourceSheet:     "",
			SelectedColumns: []string{},
			Filters:         []string{},
			Question:        question,
			SQL:             "-- no imported tables available",
		}
	}

	table := snapshot.Tables[0]
	mode := detectQueryMode(question, table)
	selectedColumns := selectedColumnsForMode(table, mode)
	sql := buildSQLForQuestion(snapshot, question)

	return queryPlan{
		SourceTable:     table.TableName,
		SourceFile:      table.SourceFile,
		SourceSheet:     table.SourceSheet,
		SelectedColumns: selectedColumns,
		Filters:         []string{},
		Question:        question,
		ChartType:       suggestVisualization(question),
		Mode:            mode,
		SQL:             sql,
	}
}

func buildSQLForQuestion(snapshot schemaSnapshot, question string) string {
	if len(snapshot.Tables) == 0 {
		return "-- no imported tables available"
	}

	table := snapshot.Tables[0]
	dimension := firstDimensionColumn(table)
	metric := firstMetricColumn(table)
	mode := detectQueryMode(question, table)

	switch mode {
	case "count":
		return "SELECT COUNT(*) AS total_count FROM " + table.TableName + ";"
	case "trend":
		timeColumn := firstTimeColumn(table)
		if timeColumn != "" && metric != "" {
			return "SELECT substr(" + timeColumn + ", 1, 7) AS time_bucket, SUM(" + metric + ") AS total_value FROM " + table.TableName +
				" GROUP BY time_bucket ORDER BY time_bucket ASC;"
		}
	case "topn":
		if dimension != "" && metric != "" {
			return "SELECT " + dimension + ", SUM(" + metric + ") AS total_value FROM " + table.TableName +
				" GROUP BY " + dimension + " ORDER BY total_value DESC LIMIT 10;"
		}
	case "aggregate":
		if metric != "" {
			return "SELECT SUM(" + metric + ") AS total_value FROM " + table.TableName + ";"
		}
	}

	if dimension != "" && metric != "" {
		return "SELECT " + dimension + ", " + metric + " FROM " + table.TableName + " LIMIT 100;"
	}
	return buildPlaceholderSQL(snapshot)
}

func selectedColumnsForMode(table tableSchema, mode string) []string {
	dimension := firstDimensionColumn(table)
	metric := firstMetricColumn(table)

	switch mode {
	case "count":
		return []string{"total_count"}
	case "trend":
		if firstTimeColumn(table) != "" && metric != "" {
			return []string{"time_bucket", "total_value"}
		}
	case "topn":
		if dimension != "" && metric != "" {
			return []string{dimension, "total_value"}
		}
	case "aggregate":
		if metric != "" {
			return []string{"total_value"}
		}
	}

	if dimension != "" && metric != "" {
		return []string{dimension, metric}
	}
	return columnNames(table.Columns)
}

func columnNames(columns []schemaColumn) []string {
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, column.Name)
	}
	return names
}

func detectQueryMode(question string, table tableSchema) string {
	q := strings.ToLower(strings.TrimSpace(question))
	switch {
	case strings.Contains(q, "trend"),
		strings.Contains(q, "over time"),
		strings.Contains(q, "by month"),
		strings.Contains(q, "monthly"),
		strings.Contains(q, "按月"),
		strings.Contains(q, "趋势"):
		if firstTimeColumn(table) != "" && firstMetricColumn(table) != "" {
			return "trend"
		}
	case strings.Contains(q, "top"),
		strings.Contains(q, "rank"),
		strings.Contains(q, "highest"),
		strings.Contains(q, "排名"),
		strings.Contains(q, "最高"),
		strings.Contains(q, "最多"):
		return "topn"
	case strings.Contains(q, "count"),
		strings.Contains(q, "how many"),
		strings.Contains(q, "number of"),
		strings.Contains(q, "多少"),
		strings.Contains(q, "几个"),
		strings.Contains(q, "几条"):
		return "count"
	case strings.Contains(q, "sum"),
		strings.Contains(q, "total"),
		strings.Contains(q, "amount"),
		strings.Contains(q, "总"),
		strings.Contains(q, "合计"):
		if firstMetricColumn(table) != "" {
			return "aggregate"
		}
	}
	return "detail"
}

func firstMetricColumn(table tableSchema) string {
	for _, column := range table.Columns {
		if isMetricColumn(column) {
			return column.Name
		}
	}
	return ""
}

func firstDimensionColumn(table tableSchema) string {
	for _, column := range table.Columns {
		if !isMetricColumn(column) && !isTimeColumn(column) {
			return column.Name
		}
	}
	for _, column := range table.Columns {
		if !isMetricColumn(column) {
			return column.Name
		}
	}
	return ""
}

func firstTimeColumn(table tableSchema) string {
	for _, column := range table.Columns {
		if isTimeColumn(column) {
			return column.Name
		}
	}
	return ""
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

func pickVisualizationSeries(plan queryPlan, snapshot schemaSnapshot) []string {
	if len(plan.SelectedColumns) > 1 {
		return []string{plan.SelectedColumns[len(plan.SelectedColumns)-1]}
	}
	y := pickVisualizationY(snapshot)
	if y == "" {
		return []string{}
	}
	return []string{y}
}

func preferredVisualizationFormat(plan queryPlan) string {
	switch plan.Mode {
	case "trend", "topn", "aggregate", "count":
		return "chart"
	default:
		return "table"
	}
}

func buildChartOutput(chartMode string, settings modelSettings, plan queryPlan, visualization map[string]any, columns []string, rows []map[string]any) map[string]any {
	switch chartMode {
	case "mermaid":
		return map[string]any{
			"mode":    "mermaid",
			"syntax":  "mermaid",
			"content": buildMermaidChart(visualization, columns, rows),
		}
	case "mcp":
		return map[string]any{
			"mode":        "mcp",
			"deployment":  "@antv/mcp-server-chart",
			"endpoint":    settings.MCPServerURL,
			"tool":        "render_chart",
			"payload": map[string]any{
				"title":         visualization["title"],
				"visualization": visualization,
				"columns":       columns,
				"rows":          rows,
				"query_plan":    plan,
			},
		}
	default:
		return map[string]any{
			"mode":          "data",
			"columns":       columns,
			"rows":          rows,
			"visualization": visualization,
		}
	}
}

func buildMermaidChart(visualization map[string]any, columns []string, rows []map[string]any) string {
	chartType, _ := visualization["type"].(string)
	title, _ := visualization["title"].(string)
	x, _ := visualization["x"].(string)
	y, _ := visualization["y"].(string)

	if len(rows) == 0 {
		return "%% no chart data"
	}

	if chartType == "pie" && len(rows) > 0 {
		var b strings.Builder
		b.WriteString("pie showData\n")
		b.WriteString("    title " + title + "\n")
		labelKey := x
		valueKey := y
		for _, row := range rows {
			label := fmt.Sprint(row[labelKey])
			value := asChartNumber(row[valueKey])
			b.WriteString(fmt.Sprintf("    %q : %v\n", label, value))
		}
		return b.String()
	}

	labels := make([]string, 0, len(rows))
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		labels = append(labels, fmt.Sprintf("%q", fmt.Sprint(row[x])))
		values = append(values, fmt.Sprintf("%v", asChartNumber(row[y])))
	}

	return "xychart-beta\n" +
		"    title " + fmt.Sprintf("%q", title) + "\n" +
		"    x-axis [" + strings.Join(labels, ", ") + "]\n" +
		"    y-axis " + fmt.Sprintf("%q", y) + " 0 --> " + fmt.Sprintf("%v", maxChartValue(rows, y)) + "\n" +
		"    bar [" + strings.Join(values, ", ") + "]"
}

func asChartNumber(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		f, _ := v.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	default:
		return 0
	}
}

func maxChartValue(rows []map[string]any, key string) float64 {
	max := 0.0
	for _, row := range rows {
		value := asChartNumber(row[key])
		if value > max {
			max = value
		}
	}
	if max <= 0 {
		return 1
	}
	return max
}

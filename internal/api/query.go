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
	"strconv"
	"strings"
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
	SourceTable        string          `json:"source_table"`
	SourceFile         string          `json:"source_file"`
	SourceSheet        string          `json:"source_sheet"`
	CandidateTables    []string        `json:"candidate_tables"`
	PlanningConfidence float64         `json:"planning_confidence"`
	SelectionReason    string          `json:"selection_reason"`
	DimensionColumn    string          `json:"dimension_column"`
	MetricColumn       string          `json:"metric_column"`
	TimeColumn         string          `json:"time_column"`
	SelectedColumns    []string        `json:"selected_columns"`
	Filters            []string        `json:"filters"`
	PlannedFilters     []plannedFilter `json:"planned_filters"`
	Question           string          `json:"question"`
	ChartType          string          `json:"chart_type"`
	Mode               string          `json:"mode"`
	SQL                string          `json:"sql"`
}

type queryExecutionResult struct {
	Rows       []map[string]any
	Cols       []string
	OK         bool
	Error      string
	Truncated  bool
	TotalCount int
}

const maxDetailRows = 200

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
	response, err := h.executeSessionQuery(sessionDir, meta, req)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "schema snapshot not found", http.StatusConflict)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) executeSessionQuery(sessionDir string, meta sessionMetadata, req queryRequest) (map[string]any, error) {
	settings, err := h.readModelSettings()
	if err != nil {
		return nil, errors.New("failed to read model settings")
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
		return nil, err
	}

	now := time.Now().UTC()
	meta.LastAccessedAt = now
	meta.UpdatedAt = now
	if err := writeSessionMetadata(sessionDir, meta); err != nil {
		return nil, errors.New("failed to update session metadata")
	}

	response, err := h.executeSingleQuery(meta, settings, snapshot, req.Question, chartMode)
	if err != nil {
		return nil, err
	}
	if shouldBuildAnalysisReport(req.Question) {
		if report := h.buildAnalysisReport(meta, settings, snapshot, chartMode); len(report) > 0 {
			response["analysis_report"] = report
		}
	}
	return response, nil
}

func (h *Handler) executeSingleQuery(meta sessionMetadata, settings modelSettings, snapshot schemaSnapshot, question, chartMode string) (map[string]any, error) {
	plan, plannerWarnings := h.buildQueryPlanWithFallback(settings, snapshot, question)
	execResult := executePlanQuery(meta.DatabasePath, plan)
	if shouldRetryLLMRepair(plannerWarnings, execResult) {
		repairedPlan, llmRepairWarnings, repaired := h.repairQueryPlanWithLLM(settings, snapshot, question, plan, execResult)
		plannerWarnings = append(plannerWarnings, llmRepairWarnings...)
		if repaired {
			plan = repairedPlan
			execResult = executePlanQuery(meta.DatabasePath, plan)
		}
	}
	if shouldRetryWithHeuristicPlan(plannerWarnings, execResult) {
		repairedPlan, heuristicWarnings := h.buildHeuristicQueryPlan(settings, snapshot, question)
		repairedResult := executePlanQuery(meta.DatabasePath, repairedPlan)
		if repairedResult.OK {
			plan = repairedPlan
			execResult = repairedResult
			plannerWarnings = append(plannerWarnings, heuristicWarnings...)
			plannerWarnings = append(plannerWarnings, "LLM SQL execution failed; the service repaired the query by falling back to the heuristic planner.")
		}
	}
	rows, columns, executed := execResult.Rows, execResult.Cols, execResult.OK
	if !executed || len(columns) == 0 || len(rows) == 0 {
		rows = buildPlaceholderRows(snapshot, question)
		columns = buildQueryColumns(snapshot)
		executed = false
	}
	visualization := map[string]any{
		"type":             plan.ChartType,
		"title":            question,
		"x":                pickVisualizationX(plan, snapshot),
		"y":                pickVisualizationY(plan, snapshot),
		"series":           pickVisualizationSeries(plan, snapshot),
		"preferred_format": preferredVisualizationFormat(plan),
		"source_table":     plan.SourceTable,
		"tables":           meta.Tables,
	}

	return map[string]any{
		"session_id":    meta.SessionID,
		"question":      question,
		"sql":           plan.SQL,
		"rows":          rows,
		"columns":       columns,
		"row_count":     effectiveRowCount(execResult, len(rows)),
		"executed":      executed,
		"chart_mode":    chartMode,
		"summary":       buildQuerySummary(plan, executed, effectiveRowCount(execResult, len(rows))),
		"query_plan":    plan,
		"visualization": visualization,
		"chart":         buildChartOutput(chartMode, settings, plan, visualization, columns, rows),
		"warnings":      append(plannerWarnings, queryWarnings(plan, execResult)...),
	}, nil
}

func (h *Handler) repairQueryPlanWithLLM(settings modelSettings, snapshot schemaSnapshot, question string, failedPlan queryPlan, execResult queryExecutionResult) (queryPlan, []string, bool) {
	if !llmEnabled(settings) {
		return failedPlan, nil, false
	}
	llmResp, err := h.generateSQLWithLLM(settings, llmSQLRequest{
		Question:       question,
		Schema:         snapshot,
		FailedSQL:      failedPlan.SQL,
		ExecutionError: execResult.Error,
		PlanningHints:  llmPlanningHints(failedPlan),
	})
	if err != nil {
		llmResp, err = h.generateSQLWithLLM(settings, llmSQLRequest{
			Question:       question,
			Schema:         snapshot,
			FailedSQL:      failedPlan.SQL,
			ExecutionError: execResult.Error,
			PlanningHints: append(
				llmPlanningHints(failedPlan),
				"Keep the query read-only and SQLite-compatible.",
				"Prefer a bounded detail query with LIMIT 200 if the result set may be large.",
			),
		})
		if err != nil {
			return failedPlan, []string{"LLM SQL repair failed after multiple attempts; falling back to heuristic repair if available."}, false
		}
	}
	safeSQL, validationErr := sanitizeReadOnlySQL(llmResp.SQL)
	if validationErr != nil {
		return failedPlan, []string{"LLM SQL repair returned an unsafe or unsupported SQL statement."}, false
	}

	repairedPlan := failedPlan
	repairedPlan.SQL = safeSQL
	if mode := normalizeIntentMode(llmResp.Mode); mode != "" {
		repairedPlan.Mode = mode
		table := findTableByName(snapshot, repairedPlan.SourceTable)
		if table.TableName == "" && len(snapshot.Tables) > 0 {
			table = snapshot.Tables[0]
		}
		if table.TableName != "" {
			repairedPlan.SelectedColumns = selectedColumnsForPlan(mode, repairedPlan.DimensionColumn, repairedPlan.MetricColumn, repairedPlan.TimeColumn)
		}
	}
	repairedPlan.SelectionReason = "repaired by the configured LLM provider after a SQLite execution error"
	return repairedPlan, []string{"LLM repaired the SQL after an execution error."}, true
}

func (h *Handler) buildQueryPlanWithFallback(settings modelSettings, snapshot schemaSnapshot, question string) (queryPlan, []string) {
	heuristicPlan, heuristicWarnings := h.buildHeuristicQueryPlan(settings, snapshot, question)
	if !llmEnabled(settings) {
		return heuristicPlan, heuristicWarnings
	}
	if len(snapshot.Tables) == 0 {
		return heuristicPlan, append(heuristicWarnings, "LLM SQL generation skipped because no imported tables are available.")
	}

	llmResp, err := h.generateSQLWithLLM(settings, llmSQLRequest{
		Question:      question,
		Schema:        snapshot,
		PlanningHints: llmPlanningHints(heuristicPlan),
	})
	if err != nil {
		llmResp, err = h.generateSQLWithLLM(settings, llmSQLRequest{
			Question: question,
			Schema:   snapshot,
			PlanningHints: append(
				llmPlanningHints(heuristicPlan),
				"Prefer an aggregated summary when the question asks for analysis or charts.",
				"Keep detail mode limited to 200 rows.",
			),
		})
		if err != nil {
			return heuristicPlan, append(heuristicWarnings, "LLM SQL generation failed after multiple attempts; using heuristic query planner.")
		}
	}
	safeSQL, validationErr := sanitizeReadOnlySQL(llmResp.SQL)
	if validationErr != nil {
		return heuristicPlan, append(heuristicWarnings, "LLM returned an unsafe or unsupported SQL statement; using heuristic query planner.")
	}

	table := snapshot.Tables[0]
	mode := normalizeIntentMode(llmResp.Mode)
	if mode == "" {
		mode = heuristicPlan.Mode
	}

	return queryPlan{
		SourceTable:        table.TableName,
		SourceFile:         table.SourceFile,
		SourceSheet:        table.SourceSheet,
		CandidateTables:    []string{table.TableName},
		PlanningConfidence: 1,
		SelectionReason:    "selected by LLM-generated SQL against the current schema snapshot",
		SelectedColumns:    selectedColumnsForPlan(mode, firstDimensionColumn(table), firstMetricColumn(table), firstTimeColumn(table)),
		Filters:            []string{},
		Question:           question,
		ChartType:          heuristicPlan.ChartType,
		Mode:               mode,
		SQL:                safeSQL,
	}, append(heuristicWarnings, "SQL was generated by the configured LLM provider.")
}

func (h *Handler) buildHeuristicQueryPlan(settings modelSettings, snapshot schemaSnapshot, question string) (queryPlan, []string) {
	if len(snapshot.Tables) == 0 {
		return buildQueryPlan(snapshot, question), nil
	}

	selection, warnings := h.choosePlanningSelection(settings, snapshot, question)
	intent := detectQueryIntent(question, selection.Table)
	sqlPlan := buildSQLPlanForSelection(snapshot, question, intent, selection)

	return queryPlan{
		SourceTable:        sqlPlan.SourceTable,
		SourceFile:         sqlPlan.SourceFile,
		SourceSheet:        sqlPlan.SourceSheet,
		CandidateTables:    sqlPlan.CandidateTables,
		PlanningConfidence: sqlPlan.PlanningConfidence,
		SelectionReason:    sqlPlan.SelectionReason,
		DimensionColumn:    sqlPlan.DimensionColumn,
		MetricColumn:       sqlPlan.MetricColumn,
		TimeColumn:         sqlPlan.TimeColumn,
		SelectedColumns:    sqlPlan.SelectedColumns,
		Filters:            sqlPlan.FilterHints,
		PlannedFilters:     sqlPlan.Filters,
		Question:           question,
		ChartType:          sqlPlan.ChartType,
		Mode:               sqlPlan.Mode,
		SQL:                sqlPlan.SQL,
	}, warnings
}

func (h *Handler) choosePlanningSelection(settings modelSettings, snapshot schemaSnapshot, question string) (planningSelection, []string) {
	if len(snapshot.Tables) == 0 {
		return planningSelection{
			Table:              tableSchema{},
			CandidateTables:    []string{},
			PlanningConfidence: 0,
			SelectionReason:    "no imported tables available",
		}, nil
	}

	table, candidates, confidence, reason := choosePlanningTable(snapshot, question)
	fallback := planningSelection{
		Table:              table,
		CandidateTables:    candidates,
		PlanningConfidence: confidence,
		SelectionReason:    reason,
	}

	docs := buildSchemaEmbeddingDocuments(snapshot)
	vectors, provider, err := embedTexts(settings, append([]string{question}, docs...))
	if err != nil && provider != embeddingProviderLocal {
		return fallback, []string{"Embedding retrieval failed; falling back to heuristic table matching."}
	}
	warnings := make([]string, 0, 1)
	if err != nil && provider == embeddingProviderLocal {
		warnings = append(warnings, "Remote embedding retrieval failed; using the local embedding fallback.")
	}

	candidatesByEmbedding := rankTablesByEmbedding(snapshot, vectors[0], vectors[1:])
	if len(candidatesByEmbedding) == 0 {
		return fallback, []string{"Embedding retrieval returned no table candidates; falling back to heuristic table matching."}
	}

	candidateNames := make([]string, 0, len(candidatesByEmbedding))
	for _, candidate := range candidatesByEmbedding {
		candidateNames = append(candidateNames, candidate.TableName)
	}

	selected := findTableByName(snapshot, candidatesByEmbedding[0].TableName)
	if selected.TableName == "" {
		return fallback, []string{"Embedding retrieval selected an unknown table; falling back to heuristic table matching."}
	}

	return planningSelection{
		Table:              selected,
		CandidateTables:    candidateNames,
		PlanningConfidence: normalizeSimilarityScore(candidatesByEmbedding[0].Score),
		SelectionReason:    "selected by embedding similarity against the imported schema catalog",
	}, warnings
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

func executeQueryIfPossible(databasePath, sql string, orderedColumns []string) queryExecutionResult {
	output, err := exec.Command(
		"sqlite3",
		"-cmd", ".timeout 2000",
		"-header",
		"-json",
		databasePath,
		sql,
	).Output()
	if err != nil {
		return queryExecutionResult{OK: false, Error: err.Error()}
	}

	trimmed := bytes.TrimSpace(output)
	if len(trimmed) == 0 {
		return queryExecutionResult{Rows: []map[string]any{}, Cols: []string{}, OK: true}
	}

	var rows []map[string]any
	if err := json.Unmarshal(trimmed, &rows); err != nil {
		return queryExecutionResult{OK: false, Error: err.Error()}
	}

	columns := append([]string{}, orderedColumns...)
	if len(columns) == 0 && len(rows) > 0 {
		for key := range rows[0] {
			columns = append(columns, key)
		}
		sort.Strings(columns)
	}
	return queryExecutionResult{Rows: rows, Cols: columns, OK: true}
}

func executePlanQuery(databasePath string, plan queryPlan) queryExecutionResult {
	sql := plan.SQL
	if plan.Mode == "detail" {
		sql = enforceDetailLimit(sql, maxDetailRows)
	}
	result := executeQueryIfPossible(databasePath, sql, plan.SelectedColumns)
	if !result.OK {
		return result
	}
	result.TotalCount = len(result.Rows)
	if plan.Mode != "detail" || len(result.Rows) < maxDetailRows {
		return result
	}

	countResult := executeQueryIfPossible(databasePath, buildCountOverQuerySQL(plan.SQL), []string{"total_count"})
	if !countResult.OK || len(countResult.Rows) == 0 {
		return result
	}
	if total, ok := readIntValue(countResult.Rows[0]["total_count"]); ok {
		result.TotalCount = total
		result.Truncated = total > len(result.Rows)
	}
	return result
}

func queryWarning(executed bool) string {
	if executed {
		return "Query executed against the local SQLite session database."
	}
	return "Query execution fell back to placeholder response data."
}

func queryWarnings(plan queryPlan, result queryExecutionResult) []string {
	warnings := []string{
		queryWarning(result.OK),
	}
	if result.Error != "" {
		warnings = append(warnings, "SQLite execution failed and fell back to placeholder data: "+result.Error)
	}
	if plan.PlanningConfidence > 0 && plan.PlanningConfidence < 0.5 {
		warnings = append(warnings, "Planner confidence is low; consider narrowing the question or selecting a table explicitly.")
	}
	if strings.EqualFold(filepath.Ext(plan.SourceFile), ".xls") {
		warnings = append(warnings, "This query is based on placeholder schema for a legacy .xls upload.")
	}
	if result.Truncated && plan.Mode == "detail" {
		warnings = append(warnings, "Detail results were capped to 200 rows; row_count reports the full matching row count.")
	}
	return warnings
}

func effectiveRowCount(result queryExecutionResult, fallback int) int {
	if result.TotalCount > 0 {
		return result.TotalCount
	}
	return fallback
}

func enforceDetailLimit(sql string, limit int) string {
	trimmed := strings.TrimSpace(strings.TrimSuffix(sql, ";"))
	if strings.Contains(strings.ToLower(trimmed), " limit ") {
		return trimmed + ";"
	}
	return trimmed + " LIMIT " + strconv.Itoa(limit) + ";"
}

func buildCountOverQuerySQL(sql string) string {
	trimmed := strings.TrimSpace(strings.TrimSuffix(sql, ";"))
	return "SELECT COUNT(*) AS total_count FROM (" + trimmed + ") AS base_query;"
}

func readIntValue(value any) (int, bool) {
	switch v := value.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case string:
		n, err := strconv.Atoi(v)
		return n, err == nil
	default:
		return 0, false
	}
}

func shouldRetryLLMRepair(plannerWarnings []string, result queryExecutionResult) bool {
	return llmWasUsed(plannerWarnings) && isLikelyRepairableSQLError(result.Error)
}

func shouldRetryWithHeuristicPlan(plannerWarnings []string, result queryExecutionResult) bool {
	if result.OK || result.Error == "" {
		return false
	}
	if !isLikelyRepairableSQLError(result.Error) {
		return false
	}
	return llmWasUsed(plannerWarnings)
}

func isLikelyRepairableSQLError(message string) bool {
	normalized := strings.ToLower(message)
	for _, fragment := range []string{
		"no such table",
		"no such column",
		"syntax error",
		"ambiguous column",
		"parse error",
	} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

func llmWasUsed(plannerWarnings []string) bool {
	for _, warning := range plannerWarnings {
		if strings.Contains(strings.ToLower(warning), "generated by the configured llm provider") {
			return true
		}
	}
	return false
}

func llmPlanningHints(plan queryPlan) []string {
	hints := make([]string, 0, 8)
	if plan.SourceTable != "" {
		hints = append(hints, "Preferred source table: "+plan.SourceTable)
	}
	if plan.DimensionColumn != "" {
		hints = append(hints, "Preferred dimension column: "+plan.DimensionColumn)
	}
	if plan.MetricColumn != "" {
		hints = append(hints, "Preferred metric column: "+plan.MetricColumn)
	}
	if plan.TimeColumn != "" {
		hints = append(hints, "Preferred time column: "+plan.TimeColumn)
	}
	if plan.Mode != "" {
		hints = append(hints, "Preferred mode: "+plan.Mode)
	}
	if len(plan.SelectedColumns) > 0 {
		hints = append(hints, "Preferred selected columns: "+strings.Join(plan.SelectedColumns, ", "))
	}
	return hints
}

func shouldBuildAnalysisReport(question string) bool {
	normalized := strings.ToLower(strings.TrimSpace(question))
	if normalized == "" {
		return false
	}
	return containsAny(normalized, []string{
		"分析", "analysis", "overview", "dashboard", "report", "行为分析", "上网行为分析", "画像",
	})
}

func (h *Handler) buildAnalysisReport(meta sessionMetadata, settings modelSettings, snapshot schemaSnapshot, chartMode string) []map[string]any {
	questions := buildAnalysisQuestions(snapshot)
	report := make([]map[string]any, 0, len(questions))
	seen := map[string]struct{}{}
	for _, item := range questions {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		response, err := h.executeSingleQuery(meta, settings, snapshot, item, chartMode)
		if err != nil {
			continue
		}
		report = append(report, map[string]any{
			"title":    item,
			"question": item,
			"response": response,
		})
		if len(report) >= 3 {
			break
		}
	}
	return report
}

func buildAnalysisQuestions(snapshot schemaSnapshot) []string {
	if len(snapshot.Tables) == 0 {
		return nil
	}
	table := snapshot.Tables[0]
	primaryCategory := firstMatchingDimensionColumn(table, []string{"分类", "category", "type", "group", "级别"})
	secondaryDimension := firstMatchingDimensionColumn(table, []string{"终端", "device", "用户组", "region", "级别", "用户", "user"})
	timeColumn := firstTimeColumn(table)

	questions := make([]string, 0, 4)
	if primaryCategory != "" {
		questions = append(questions, "按"+primaryCategory+"统计访问量分布饼图")
	}
	if secondaryDimension != "" && secondaryDimension != primaryCategory {
		questions = append(questions, "按"+secondaryDimension+"统计访问量柱状图")
	}
	if timeColumn != "" {
		questions = append(questions, "按时间统计访问趋势")
	}
	if primaryCategory == "" && secondaryDimension == "" {
		questions = append(questions, "按维度统计访问量分布饼图")
	}
	return questions
}

func firstMatchingDimensionColumn(table tableSchema, keywords []string) string {
	for _, keyword := range keywords {
		lowerKeyword := strings.ToLower(keyword)
		for _, column := range table.Columns {
			if isMetricColumn(column) || isTimeColumn(column) {
				continue
			}
			name := strings.ToLower(column.Name)
			if strings.Contains(name, lowerKeyword) {
				return column.Name
			}
		}
	}
	return ""
}

func buildQueryPlan(snapshot schemaSnapshot, question string) queryPlan {
	if len(snapshot.Tables) == 0 {
		return queryPlan{
			SourceTable:        "",
			SourceFile:         "",
			SourceSheet:        "",
			CandidateTables:    []string{},
			PlanningConfidence: 0,
			SelectionReason:    "no imported tables available",
			SelectedColumns:    []string{},
			Filters:            []string{},
			PlannedFilters:     nil,
			Question:           question,
			SQL:                "-- no imported tables available",
		}
	}

	table := snapshot.Tables[0]
	intent := detectQueryIntent(question, table)
	sqlPlan := buildSQLPlan(snapshot, question, intent)

	return queryPlan{
		SourceTable:        sqlPlan.SourceTable,
		SourceFile:         sqlPlan.SourceFile,
		SourceSheet:        sqlPlan.SourceSheet,
		CandidateTables:    sqlPlan.CandidateTables,
		PlanningConfidence: sqlPlan.PlanningConfidence,
		SelectionReason:    sqlPlan.SelectionReason,
		DimensionColumn:    sqlPlan.DimensionColumn,
		MetricColumn:       sqlPlan.MetricColumn,
		TimeColumn:         sqlPlan.TimeColumn,
		SelectedColumns:    sqlPlan.SelectedColumns,
		Filters:            sqlPlan.FilterHints,
		PlannedFilters:     sqlPlan.Filters,
		Question:           question,
		ChartType:          sqlPlan.ChartType,
		Mode:               sqlPlan.Mode,
		SQL:                sqlPlan.SQL,
	}
}

func findTableByName(snapshot schemaSnapshot, tableName string) tableSchema {
	for _, table := range snapshot.Tables {
		if table.TableName == tableName {
			return table
		}
	}
	return tableSchema{}
}

func normalizeSimilarityScore(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}

func selectedColumnsForPlan(mode, dimension, metric, timeColumn string) []string {
	switch mode {
	case "count":
		return []string{"total_count"}
	case "trend":
		if timeColumn != "" {
			return []string{"time_bucket", "total_value"}
		}
	case "share":
		if dimension != "" {
			return []string{dimension, "share_value"}
		}
	case "compare":
		if timeColumn != "" && metric != "" {
			return []string{"compare_period", "total_value"}
		}
		if dimension != "" {
			return []string{dimension, "total_value"}
		}
	case "topn":
		if dimension != "" {
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
	if dimension != "" {
		return []string{dimension, "total_count"}
	}
	return []string{}
}

func selectedColumnsForMode(table tableSchema, mode string) []string {
	return selectedColumnsForPlan(mode, firstDimensionColumn(table), firstMetricColumn(table), firstTimeColumn(table))
}

func columnNames(columns []schemaColumn) []string {
	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, column.Name)
	}
	return names
}

func isSafeReadOnlySQL(sql string) bool {
	_, err := sanitizeReadOnlySQL(sql)
	return err == nil
}

func sanitizeReadOnlySQL(sql string) (string, error) {
	trimmed := strings.TrimSpace(sql)
	if trimmed == "" {
		return "", errors.New("empty sql")
	}
	trimmed = strings.TrimSuffix(trimmed, ";")
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, ";") {
		return "", errors.New("multiple statements are not allowed")
	}
	if strings.Contains(lower, "--") || strings.Contains(lower, "/*") {
		return "", errors.New("sql comments are not allowed")
	}
	if !strings.HasPrefix(lower, "select ") && !strings.HasPrefix(lower, "with ") {
		return "", errors.New("sql must start with select or with")
	}
	for _, banned := range []string{" insert ", " update ", " delete ", " drop ", " alter ", " create ", " attach ", " detach ", " pragma ", " vacuum ", " reindex "} {
		if strings.Contains(" "+lower+" ", banned) {
			return "", errors.New("sql contains unsupported write keywords")
		}
	}
	return trimmed + ";", nil
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

func pickVisualizationX(plan queryPlan, snapshot schemaSnapshot) string {
	if strings.TrimSpace(plan.DimensionColumn) != "" {
		return plan.DimensionColumn
	}
	table := resolveVisualizationTable(snapshot, plan.SourceTable)
	if len(table.Columns) == 0 {
		return "dimension"
	}

	for _, column := range table.Columns {
		if !isMetricColumn(column) {
			return column.Name
		}
	}
	return table.Columns[0].Name
}

func pickVisualizationY(plan queryPlan, snapshot schemaSnapshot) string {
	switch plan.Mode {
	case "share":
		return "share_value"
	case "count":
		return "total_count"
	case "aggregate", "compare", "trend", "topn":
		return "total_value"
	}
	if strings.TrimSpace(plan.MetricColumn) != "" {
		return plan.MetricColumn
	}
	table := resolveVisualizationTable(snapshot, plan.SourceTable)
	if len(table.Columns) == 0 {
		return "value"
	}

	for _, column := range table.Columns {
		if isMetricColumn(column) {
			return column.Name
		}
	}
	return table.Columns[0].Name
}

func resolveVisualizationTable(snapshot schemaSnapshot, tableName string) tableSchema {
	if tableName != "" {
		if table := findTableByName(snapshot, tableName); table.TableName != "" {
			return table
		}
	}
	if len(snapshot.Tables) == 0 {
		return tableSchema{}
	}
	return snapshot.Tables[0]
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

func pickVisualizationSeries(plan queryPlan, snapshot schemaSnapshot) []string {
	if len(plan.SelectedColumns) > 1 {
		return []string{plan.SelectedColumns[len(plan.SelectedColumns)-1]}
	}
	y := pickVisualizationY(plan, snapshot)
	if y == "" {
		return []string{}
	}
	return []string{y}
}

func preferredVisualizationFormat(plan queryPlan) string {
	switch plan.Mode {
	case "trend", "topn", "aggregate", "count", "share", "compare":
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
		out := map[string]any{
			"mode":       "mcp",
			"deployment": "@antv/mcp-server-chart",
			"endpoint":   settings.MCPServerURL,
			"payload": map[string]any{
				"title":         visualization["title"],
				"visualization": visualization,
				"columns":       columns,
				"rows":          rows,
				"query_plan":    plan,
			},
		}
		result, err := executeChartMCP(settings.MCPServerURL, visualization, columns, rows)
		if err != nil {
			out["executed"] = false
			out["error"] = err.Error()
			return out
		}
		out["executed"] = true
		out["result"] = result
		if toolName, ok := result["tool_name"]; ok {
			out["tool"] = toolName
		}
		if url, ok := result["url"]; ok {
			out["url"] = url
		}
		return out
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

package api

import (
	"slices"
	"strings"
)

type plannedFilter struct {
	Column   string
	Operator string
	Value    string
}

type scoredTable struct {
	table tableSchema
	score int
}

type sqlPlan struct {
	SourceTable        string
	SourceFile         string
	SourceSheet        string
	Mode               string
	ChartType          string
	CandidateTables    []string
	PlanningConfidence float64
	SelectionReason    string
	DimensionColumn    string
	MetricColumn       string
	TimeColumn         string
	FilterHints        []string
	Filters            []plannedFilter
	SelectedColumns    []string
	SQL                string
}

type planningSelection struct {
	Table              tableSchema
	CandidateTables    []string
	PlanningConfidence float64
	SelectionReason    string
}

func buildSQLPlan(snapshot schemaSnapshot, question string, intent queryIntent) sqlPlan {
	if len(snapshot.Tables) == 0 {
		return sqlPlan{
			SourceTable:        "",
			SourceFile:         "",
			SourceSheet:        "",
			Mode:               intent.Mode,
			ChartType:          intent.ChartType,
			CandidateTables:    []string{},
			PlanningConfidence: 0,
			SelectionReason:    "no imported tables available",
			DimensionColumn:    "",
			MetricColumn:       "",
			TimeColumn:         "",
			FilterHints:        intent.FilterHints,
			Filters:            nil,
			SelectedColumns:    []string{},
			SQL:                "-- no imported tables available",
		}
	}

	table, candidates, confidence, reason := choosePlanningTable(snapshot, question)
	return buildSQLPlanForSelection(snapshot, question, intent, planningSelection{
		Table:              table,
		CandidateTables:    candidates,
		PlanningConfidence: confidence,
		SelectionReason:    reason,
	})
}

func buildSQLPlanForSelection(snapshot schemaSnapshot, question string, intent queryIntent, selection planningSelection) sqlPlan {
	if len(snapshot.Tables) == 0 {
		return sqlPlan{
			SourceTable:        "",
			SourceFile:         "",
			SourceSheet:        "",
			Mode:               intent.Mode,
			ChartType:          intent.ChartType,
			CandidateTables:    []string{},
			PlanningConfidence: 0,
			SelectionReason:    "no imported tables available",
			DimensionColumn:    "",
			MetricColumn:       "",
			TimeColumn:         "",
			FilterHints:        intent.FilterHints,
			Filters:            nil,
			SelectedColumns:    []string{},
			SQL:                "-- no imported tables available",
		}
	}

	table := selection.Table
	if table.TableName == "" {
		table = snapshot.Tables[0]
	}
	dimension := bestDimensionColumn(table, question, intent)
	metric := bestMetricColumn(table, question, intent)
	timeColumn := bestTimeColumn(table, question, intent)
	filters := planFilters(table, intent)
	return sqlPlan{
		SourceTable:        table.TableName,
		SourceFile:         table.SourceFile,
		SourceSheet:        table.SourceSheet,
		CandidateTables:    selection.CandidateTables,
		PlanningConfidence: selection.PlanningConfidence,
		SelectionReason:    selection.SelectionReason,
		Mode:               intent.Mode,
		ChartType:          intent.ChartType,
		DimensionColumn:    dimension,
		MetricColumn:       metric,
		TimeColumn:         timeColumn,
		FilterHints:        intent.FilterHints,
		Filters:            filters,
		SelectedColumns:    selectedColumnsForPlan(intent.Mode, dimension, metric, timeColumn),
		SQL:                buildSQLForIntent(table.TableName, intent, filters, dimension, metric, timeColumn),
	}
}

func choosePlanningTable(snapshot schemaSnapshot, question string) (tableSchema, []string, float64, string) {
	if len(snapshot.Tables) == 0 {
		return tableSchema{}, nil, 0, "no imported tables available"
	}

	scored := make([]scoredTable, 0, len(snapshot.Tables))
	for _, table := range snapshot.Tables {
		scored = append(scored, scoredTable{
			table: table,
			score: tableMatchScore(question, table),
		})
	}
	slices.SortStableFunc(scored, func(a, b scoredTable) int {
		if a.score == b.score {
			return 0
		}
		if a.score > b.score {
			return -1
		}
		return 1
	})

	best := scored[0]
	candidates := make([]string, 0, len(scored))
	for _, item := range scored {
		if item.score > 0 {
			candidates = append(candidates, item.table.TableName)
		}
	}
	if len(candidates) == 0 {
		for _, item := range scored {
			candidates = append(candidates, item.table.TableName)
		}
	}

	return best.table, candidates, planningConfidence(scored), selectionReason(scored)
}

func tableMatchScore(question string, table tableSchema) int {
	normalizedQuestion := strings.ToLower(question)
	score := 0

	score += countTokenMatches(normalizedQuestion, normalizeNameTokens(table.TableName)) * 5
	score += countTokenMatches(normalizedQuestion, normalizeNameTokens(stripExtension(table.SourceFile))) * 4
	score += countTokenMatches(normalizedQuestion, normalizeNameTokens(table.SourceSheet)) * 2
	score += businessConceptScore(normalizedQuestion, table) * 3

	for _, column := range table.Columns {
		columnWeight := 1
		switch column.Semantic {
		case "metric":
			columnWeight = 3
		case "dimension", "time":
			columnWeight = 2
		}
		score += countTokenMatches(normalizedQuestion, normalizeNameTokens(column.Name)) * columnWeight
		if semanticHintMatches(normalizedQuestion, column.Semantic) {
			score += 2
		}
	}

	return score
}

func businessConceptScore(question string, table tableSchema) int {
	score := 0
	for _, concept := range businessConcepts() {
		if !containsAny(question, concept.QuestionTerms) {
			continue
		}
		if containsAny(strings.ToLower(table.TableName), concept.TableTerms) ||
			containsAny(strings.ToLower(stripExtension(table.SourceFile)), concept.TableTerms) {
			score += 3
		}
		for _, column := range table.Columns {
			columnName := strings.ToLower(column.Name)
			if containsAny(columnName, concept.ColumnTerms) {
				score += 2
			}
		}
	}
	return score
}

type businessConcept struct {
	QuestionTerms []string
	TableTerms    []string
	ColumnTerms   []string
}

func businessConcepts() []businessConcept {
	return []businessConcept{
		{
			QuestionTerms: []string{"sales", "revenue", "gmv", "成交额", "销售额", "营收"},
			TableTerms:    []string{"sales", "revenue", "orders", "transactions"},
			ColumnTerms:   []string{"amount", "revenue", "gmv", "sales", "price", "total"},
		},
		{
			QuestionTerms: []string{"customer", "customers", "user", "users", "客户", "用户"},
			TableTerms:    []string{"customer", "customers", "user", "users", "accounts"},
			ColumnTerms:   []string{"customer", "user", "account", "member"},
		},
		{
			QuestionTerms: []string{"order", "orders", "订单"},
			TableTerms:    []string{"order", "orders", "sales", "transactions"},
			ColumnTerms:   []string{"order", "amount", "transaction"},
		},
		{
			QuestionTerms: []string{"product", "products", "sku", "商品", "产品"},
			TableTerms:    []string{"product", "products", "catalog", "inventory"},
			ColumnTerms:   []string{"product", "sku", "category", "brand"},
		},
	}
}

func semanticHintMatches(question, semantic string) bool {
	switch semantic {
	case "metric":
		return strings.Contains(question, "sum") ||
			strings.Contains(question, "total") ||
			strings.Contains(question, "count") ||
			strings.Contains(question, "amount") ||
			strings.Contains(question, "sales") ||
			strings.Contains(question, "revenue") ||
			strings.Contains(question, "数量")
	case "time":
		return strings.Contains(question, "trend") ||
			strings.Contains(question, "time") ||
			strings.Contains(question, "month") ||
			strings.Contains(question, "year") ||
			strings.Contains(question, "day") ||
			strings.Contains(question, "趋势")
	case "dimension":
		return strings.Contains(question, "by ") ||
			strings.Contains(question, "group") ||
			strings.Contains(question, "top") ||
			strings.Contains(question, "分类") ||
			strings.Contains(question, "分组")
	default:
		return false
	}
}

func countTokenMatches(question string, tokens []string) int {
	matches := 0
	for _, token := range tokens {
		if token == "" {
			continue
		}
		if strings.Contains(question, token) {
			matches++
		}
	}
	return matches
}

func containsAny(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func normalizeNameTokens(value string) []string {
	replacer := strings.NewReplacer("_", " ", "-", " ", ".", " ", "/", " ")
	parts := strings.Fields(strings.ToLower(replacer.Replace(value)))
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) <= 1 {
			continue
		}
		tokens = append(tokens, part)
	}
	return tokens
}

func stripExtension(name string) string {
	if idx := strings.LastIndex(name, "."); idx > 0 {
		return name[:idx]
	}
	return name
}

func planningConfidence(scored []scoredTable) float64 {
	if len(scored) == 0 {
		return 0
	}
	best := scored[0].score
	if best <= 0 {
		return 0.25
	}
	if len(scored) == 1 {
		return 1
	}
	second := scored[1].score
	if second <= 0 {
		return 1
	}
	confidence := float64(best-second) / float64(best)
	if confidence < 0.2 {
		return 0.35
	}
	if confidence > 1 {
		return 1
	}
	return confidence
}

func selectionReason(scored []scoredTable) string {
	if len(scored) == 0 {
		return "no scoring candidates were available"
	}
	if len(scored) == 1 {
		return "selected the only imported table"
	}
	if scored[0].score == scored[1].score {
		return "selected the first table among equally scored candidates"
	}
	return "selected the highest-scoring table based on question, file, and column matches"
}

func buildSQLForIntent(tableName string, intent queryIntent, filters []plannedFilter, dimension, metric, timeColumn string) string {
	whereClause := buildWhereClause(filters)

	switch intent.Mode {
	case "count":
		return "SELECT COUNT(*) AS total_count FROM " + tableName + whereClause + ";"
	case "trend":
		if timeColumn != "" && metric != "" {
			return buildTrendSQL(tableName, timeColumn, metric, intent.TimeGranularity, whereClause)
		}
		if timeColumn != "" {
			return buildTrendCountSQL(tableName, timeColumn, intent.TimeGranularity, whereClause)
		}
	case "share":
		if dimension != "" && metric != "" {
			return "SELECT " + dimension + ", ROUND(100.0 * SUM(" + metric + ") / SUM(SUM(" + metric + ")) OVER (), 2) AS share_value FROM " + tableName +
				whereClause + " GROUP BY " + dimension + " ORDER BY share_value DESC LIMIT 20;"
		}
		if dimension != "" {
			return "SELECT " + dimension + ", COUNT(*) AS total_count, ROUND(100.0 * COUNT(*) / SUM(COUNT(*)) OVER (), 2) AS share_value FROM " + tableName +
				whereClause + " GROUP BY " + dimension + " ORDER BY total_count DESC LIMIT 20;"
		}
	case "compare":
		if intent.ComparisonType != "" && timeColumn != "" && metric != "" {
			return buildTimeComparisonSQL(tableName, timeColumn, metric, intent.ComparisonType, whereClause)
		}
		if dimension != "" && metric != "" {
			return "SELECT " + dimension + ", SUM(" + metric + ") AS total_value FROM " + tableName +
				whereClause + " GROUP BY " + dimension + " ORDER BY " + dimension + " ASC LIMIT 20;"
		}
		if dimension != "" {
			return "SELECT " + dimension + ", COUNT(*) AS total_value FROM " + tableName +
				whereClause + " GROUP BY " + dimension + " ORDER BY total_value DESC LIMIT 20;"
		}
	case "topn":
		if dimension != "" && metric != "" {
			return "SELECT " + dimension + ", SUM(" + metric + ") AS total_value FROM " + tableName +
				whereClause + " GROUP BY " + dimension + " ORDER BY total_value DESC LIMIT 10;"
		}
		if dimension != "" {
			return "SELECT " + dimension + ", COUNT(*) AS total_value FROM " + tableName +
				whereClause + " GROUP BY " + dimension + " ORDER BY total_value DESC LIMIT 10;"
		}
	case "aggregate":
		if metric != "" {
			return "SELECT SUM(" + metric + ") AS total_value FROM " + tableName + whereClause + ";"
		}
	}

	if dimension != "" && metric != "" {
		return "SELECT " + dimension + ", " + metric + " FROM " + tableName + whereClause + " LIMIT 100;"
	}
	if dimension != "" {
		return "SELECT " + dimension + ", COUNT(*) AS total_count FROM " + tableName + whereClause + " GROUP BY " + dimension + " ORDER BY total_count DESC LIMIT 100;"
	}
	return buildPlaceholderSQL(schemaSnapshot{Tables: []tableSchema{{TableName: tableName}}})
}

func bestDimensionColumn(table tableSchema, question string, intent queryIntent) string {
	bestScore := -1
	bestColumn := ""
	for _, column := range table.Columns {
		if isMetricColumn(column) || isTimeColumn(column) {
			continue
		}
		score := dimensionColumnScore(question, intent, column)
		if score > bestScore {
			bestScore = score
			bestColumn = column.Name
		}
	}
	if bestColumn != "" {
		return bestColumn
	}
	return firstDimensionColumn(table)
}

func bestMetricColumn(table tableSchema, question string, intent queryIntent) string {
	bestScore := -1
	bestColumn := ""
	for _, column := range table.Columns {
		if !isMetricColumn(column) {
			continue
		}
		score := metricColumnScore(question, intent, column)
		if score > bestScore {
			bestScore = score
			bestColumn = column.Name
		}
	}
	if bestColumn != "" && bestScore > 0 {
		return bestColumn
	}
	if intent.Mode == "share" {
		return ""
	}
	return firstMetricColumn(table)
}

func bestTimeColumn(table tableSchema, _ string, _ queryIntent) string {
	return firstTimeColumn(table)
}

func dimensionColumnScore(question string, intent queryIntent, column schemaColumn) int {
	name := strings.ToLower(column.Name)
	score := countTokenMatches(strings.ToLower(question), normalizeNameTokens(column.Name)) * 3

	if intent.Share || intent.ChartType == "pie" {
		if containsAny(name, []string{"category", "type", "group", "class", "分类", "类型", "分组", "级别"}) {
			score += 8
		}
	}
	if containsAny(strings.ToLower(question), []string{"网页", "浏览", "url", "网站"}) {
		if containsAny(name, []string{"url分类", "url_分类", "分类"}) {
			score += 10
		}
		if containsAny(name, []string{"网页标题", "网页_标题", "title"}) {
			score += 5
		}
		if containsAny(name, []string{"url"}) {
			score += 4
		}
	}
	if containsAny(strings.ToLower(question), []string{"用户", "user"}) &&
		containsAny(name, []string{"用户", "user", "customer"}) {
		score += 5
	}
	if containsAny(strings.ToLower(question), []string{"终端", "device", "client"}) &&
		containsAny(name, []string{"终端", "device", "client"}) {
		score += 5
	}
	if containsAny(strings.ToLower(question), []string{"级别", "level"}) &&
		containsAny(name, []string{"级别", "level"}) {
		score += 5
	}
	if strings.Contains(name, "url") && !strings.Contains(name, "分类") && intent.Share {
		score -= 2
	}
	return score
}

func metricColumnScore(question string, intent queryIntent, column schemaColumn) int {
	name := strings.ToLower(column.Name)
	score := countTokenMatches(strings.ToLower(question), normalizeNameTokens(column.Name)) * 3
	if containsAny(strings.ToLower(question), []string{"sum", "total", "amount", "金额", "总额", "数量", "次数"}) &&
		containsAny(name, []string{"amount", "total", "count", "quantity", "数量", "次数", "金额"}) {
		score += 8
	}
	if containsAny(strings.ToLower(question), []string{"sales", "revenue", "gmv", "成交额", "销售额", "营收"}) &&
		containsAny(name, []string{"amount", "revenue", "gmv", "sales", "total", "金额", "销售额", "营收"}) {
		score += 8
	}
	if intent.Mode == "detail" && intent.ChartType == "pie" {
		score -= 2
	}
	if containsAny(name, []string{"port", "端口", "id", "编号", "用户组"}) {
		score -= 5
	}
	return score
}

func buildTimeComparisonSQL(tableName, timeColumn, metric, comparisonType, whereClause string) string {
	switch comparisonType {
	case "yoy":
		return "SELECT substr(" + timeColumn + ", 1, 4) AS compare_period, SUM(" + metric + ") AS total_value FROM " + tableName +
			whereClause + " GROUP BY compare_period ORDER BY compare_period ASC;"
	case "mom":
		return "SELECT substr(" + timeColumn + ", 1, 7) AS compare_period, SUM(" + metric + ") AS total_value FROM " + tableName +
			whereClause + " GROUP BY compare_period ORDER BY compare_period ASC;"
	default:
		return "SELECT substr(" + timeColumn + ", 1, 7) AS compare_period, SUM(" + metric + ") AS total_value FROM " + tableName +
			whereClause + " GROUP BY compare_period ORDER BY compare_period ASC;"
	}
}

func buildTrendSQL(tableName, timeColumn, metric, granularity, whereClause string) string {
	switch granularity {
	case "day":
		return "SELECT substr(" + timeColumn + ", 1, 10) AS time_bucket, SUM(" + metric + ") AS total_value FROM " + tableName +
			whereClause + " GROUP BY time_bucket ORDER BY time_bucket ASC;"
	case "year":
		return "SELECT substr(" + timeColumn + ", 1, 4) AS time_bucket, SUM(" + metric + ") AS total_value FROM " + tableName +
			whereClause + " GROUP BY time_bucket ORDER BY time_bucket ASC;"
	default:
		return "SELECT substr(" + timeColumn + ", 1, 7) AS time_bucket, SUM(" + metric + ") AS total_value FROM " + tableName +
			whereClause + " GROUP BY time_bucket ORDER BY time_bucket ASC;"
	}
}

func buildTrendCountSQL(tableName, timeColumn, granularity, whereClause string) string {
	switch granularity {
	case "day":
		return "SELECT substr(" + timeColumn + ", 1, 10) AS time_bucket, COUNT(*) AS total_value FROM " + tableName +
			whereClause + " GROUP BY time_bucket ORDER BY time_bucket ASC;"
	case "year":
		return "SELECT substr(" + timeColumn + ", 1, 4) AS time_bucket, COUNT(*) AS total_value FROM " + tableName +
			whereClause + " GROUP BY time_bucket ORDER BY time_bucket ASC;"
	default:
		return "SELECT substr(" + timeColumn + ", 1, 7) AS time_bucket, COUNT(*) AS total_value FROM " + tableName +
			whereClause + " GROUP BY time_bucket ORDER BY time_bucket ASC;"
	}
}

func planFilters(table tableSchema, intent queryIntent) []plannedFilter {
	filters := make([]plannedFilter, 0, len(intent.FilterHints))
	timeColumn := firstTimeColumn(table)
	dimensionColumn := firstDimensionColumn(table)

	for _, hint := range intent.FilterHints {
		switch strings.ToLower(strings.TrimSpace(hint)) {
		case "east", "华东":
			if dimensionColumn != "" {
				filters = append(filters, plannedFilter{Column: dimensionColumn, Operator: "=", Value: "east"})
			}
		case "west", "华西":
			if dimensionColumn != "" {
				filters = append(filters, plannedFilter{Column: dimensionColumn, Operator: "=", Value: "west"})
			}
		case "north", "华北":
			if dimensionColumn != "" {
				filters = append(filters, plannedFilter{Column: dimensionColumn, Operator: "=", Value: "north"})
			}
		case "south", "华南":
			if dimensionColumn != "" {
				filters = append(filters, plannedFilter{Column: dimensionColumn, Operator: "=", Value: "south"})
			}
		case "this year", "今年":
			if timeColumn != "" {
				filters = append(filters, plannedFilter{Column: timeColumn, Operator: "prefix", Value: "2025"})
			}
		case "this month", "本月":
			if timeColumn != "" {
				filters = append(filters, plannedFilter{Column: timeColumn, Operator: "prefix", Value: "2025-01"})
			}
		}
	}

	return filters
}

func buildWhereClause(filters []plannedFilter) string {
	if len(filters) == 0 {
		return ""
	}

	parts := make([]string, 0, len(filters))
	for _, filter := range filters {
		switch filter.Operator {
		case "=":
			parts = append(parts, filter.Column+" = "+sqliteQuote(filter.Value))
		case "prefix":
			parts = append(parts, filter.Column+" LIKE "+sqliteQuote(filter.Value+"%"))
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(parts, " AND ")
}

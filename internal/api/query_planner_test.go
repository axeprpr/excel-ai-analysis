package api

import (
	"strings"
	"testing"
)

func TestBuildTrendSQLByGranularity(t *testing.T) {
	monthSQL := buildTrendSQL("sales", "order_date", "amount", "month", "")
	daySQL := buildTrendSQL("sales", "order_date", "amount", "day", "")
	yearSQL := buildTrendSQL("sales", "order_date", "amount", "year", "")

	if !strings.Contains(monthSQL, "substr(order_date, 1, 7)") {
		t.Fatalf("expected month sql to bucket by month, got %q", monthSQL)
	}
	if !strings.Contains(daySQL, "substr(order_date, 1, 10)") {
		t.Fatalf("expected day sql to bucket by day, got %q", daySQL)
	}
	if !strings.Contains(yearSQL, "substr(order_date, 1, 4)") {
		t.Fatalf("expected year sql to bucket by year, got %q", yearSQL)
	}
}

func TestBuildSQLPlanCarriesStructuredSelections(t *testing.T) {
	snapshot := schemaSnapshot{
		Tables: []tableSchema{
			{
				TableName:   "sales",
				SourceFile:  "sales.csv",
				SourceSheet: "Sheet1",
				Columns: []schemaColumn{
					{Name: "order_date", Type: "DATE", Semantic: "time"},
					{Name: "category", Type: "TEXT", Semantic: "dimension"},
					{Name: "amount", Type: "REAL", Semantic: "metric"},
				},
			},
		},
	}

	intent := detectQueryIntent("show sales trend by month in east", snapshot.Tables[0])
	plan := buildSQLPlan(snapshot, "show sales trend by month in east", intent)

	if plan.SourceTable != "sales" {
		t.Fatalf("expected source table sales, got %q", plan.SourceTable)
	}
	if len(plan.CandidateTables) != 1 || plan.CandidateTables[0] != "sales" {
		t.Fatalf("expected candidate tables to include sales, got %#v", plan.CandidateTables)
	}
	if plan.PlanningConfidence <= 0 {
		t.Fatalf("expected positive planner confidence, got %v", plan.PlanningConfidence)
	}
	if plan.SelectionReason == "" {
		t.Fatalf("expected selection reason to be populated")
	}
	if plan.DimensionColumn != "category" {
		t.Fatalf("expected dimension column category, got %q", plan.DimensionColumn)
	}
	if plan.MetricColumn != "amount" {
		t.Fatalf("expected metric column amount, got %q", plan.MetricColumn)
	}
	if plan.TimeColumn != "order_date" {
		t.Fatalf("expected time column order_date, got %q", plan.TimeColumn)
	}
	if len(plan.FilterHints) == 0 {
		t.Fatalf("expected filter hints to be populated")
	}
	if len(plan.Filters) == 0 {
		t.Fatalf("expected planned filters to be populated")
	}
	if !strings.Contains(plan.SQL, "WHERE") {
		t.Fatalf("expected planned SQL to include WHERE clause, got %q", plan.SQL)
	}
}

func TestBuildSQLPlanSelectsBestMatchingTable(t *testing.T) {
	snapshot := schemaSnapshot{
		Tables: []tableSchema{
			{
				TableName:   "sales",
				SourceFile:  "sales.csv",
				SourceSheet: "Sheet1",
				Columns: []schemaColumn{
					{Name: "order_date", Type: "DATE", Semantic: "time"},
					{Name: "category", Type: "TEXT", Semantic: "dimension"},
					{Name: "amount", Type: "REAL", Semantic: "metric"},
				},
			},
			{
				TableName:   "customers",
				SourceFile:  "customers.csv",
				SourceSheet: "Sheet1",
				Columns: []schemaColumn{
					{Name: "created_at", Type: "DATE", Semantic: "time"},
					{Name: "region", Type: "TEXT", Semantic: "dimension"},
					{Name: "customer_count", Type: "INTEGER", Semantic: "metric"},
				},
			},
		},
	}

	intent := detectQueryIntent("show customer trend by month", snapshot.Tables[0])
	plan := buildSQLPlan(snapshot, "show customer trend by month", intent)

	if plan.SourceTable != "customers" {
		t.Fatalf("expected source table customers, got %q", plan.SourceTable)
	}
	if len(plan.CandidateTables) == 0 || plan.CandidateTables[0] != "customers" {
		t.Fatalf("expected candidate tables to prioritize customers, got %#v", plan.CandidateTables)
	}
	if plan.PlanningConfidence < 0.35 {
		t.Fatalf("expected meaningful planner confidence, got %v", plan.PlanningConfidence)
	}
	if !strings.Contains(plan.SQL, "FROM customers") {
		t.Fatalf("expected SQL to target customers table, got %q", plan.SQL)
	}
}

func TestBuildSQLPlanSupportsShareAndComparisonModes(t *testing.T) {
	table := tableSchema{
		TableName:   "sales",
		SourceFile:  "sales.csv",
		SourceSheet: "Sheet1",
		Columns: []schemaColumn{
			{Name: "order_date", Type: "DATE", Semantic: "time"},
			{Name: "category", Type: "TEXT", Semantic: "dimension"},
			{Name: "amount", Type: "REAL", Semantic: "metric"},
		},
	}
	snapshot := schemaSnapshot{Tables: []tableSchema{table}}

	shareIntent := detectQueryIntent("show category share", table)
	sharePlan := buildSQLPlan(snapshot, "show category share", shareIntent)
	if sharePlan.Mode != "share" {
		t.Fatalf("expected share mode, got %q", sharePlan.Mode)
	}
	if !strings.Contains(sharePlan.SQL, "share_value") {
		t.Fatalf("expected share sql to expose share_value, got %q", sharePlan.SQL)
	}

	compareIntent := detectQueryIntent("compare category revenue", table)
	comparePlan := buildSQLPlan(snapshot, "compare category revenue", compareIntent)
	if comparePlan.Mode != "compare" {
		t.Fatalf("expected compare mode, got %q", comparePlan.Mode)
	}
	if !strings.Contains(comparePlan.SQL, "GROUP BY category") {
		t.Fatalf("expected compare sql to group by category, got %q", comparePlan.SQL)
	}
	if !strings.Contains(comparePlan.SQL, "total_value") {
		t.Fatalf("expected compare sql to expose total_value, got %q", comparePlan.SQL)
	}

	yoyIntent := detectQueryIntent("同比销售额", table)
	yoyPlan := buildSQLPlan(snapshot, "同比销售额", yoyIntent)
	if yoyIntent.ComparisonType != "yoy" {
		t.Fatalf("expected yoy comparison type, got %q", yoyIntent.ComparisonType)
	}
	if !strings.Contains(yoyPlan.SQL, "compare_period") || !strings.Contains(yoyPlan.SQL, "substr(order_date, 1, 4)") {
		t.Fatalf("expected yoy sql to bucket by year, got %q", yoyPlan.SQL)
	}

	momIntent := detectQueryIntent("mom revenue", table)
	momPlan := buildSQLPlan(snapshot, "mom revenue", momIntent)
	if momIntent.ComparisonType != "mom" {
		t.Fatalf("expected mom comparison type, got %q", momIntent.ComparisonType)
	}
	if !strings.Contains(momPlan.SQL, "compare_period") || !strings.Contains(momPlan.SQL, "substr(order_date, 1, 7)") {
		t.Fatalf("expected mom sql to bucket by month, got %q", momPlan.SQL)
	}
}

func TestBuildSQLPlanUsesBusinessConceptSynonyms(t *testing.T) {
	snapshot := schemaSnapshot{
		Tables: []tableSchema{
			{
				TableName:   "transactions",
				SourceFile:  "transactions.csv",
				SourceSheet: "Sheet1",
				Columns: []schemaColumn{
					{Name: "created_at", Type: "DATE", Semantic: "time"},
					{Name: "region", Type: "TEXT", Semantic: "dimension"},
					{Name: "gmv", Type: "REAL", Semantic: "metric"},
				},
			},
			{
				TableName:   "customers",
				SourceFile:  "customers.csv",
				SourceSheet: "Sheet1",
				Columns: []schemaColumn{
					{Name: "created_at", Type: "DATE", Semantic: "time"},
					{Name: "customer_name", Type: "TEXT", Semantic: "dimension"},
					{Name: "customer_count", Type: "INTEGER", Semantic: "metric"},
				},
			},
		},
	}

	intent := detectQueryIntent("show gmv trend by month", snapshot.Tables[0])
	plan := buildSQLPlan(snapshot, "show gmv trend by month", intent)
	if plan.SourceTable != "transactions" {
		t.Fatalf("expected gmv query to select transactions, got %q", plan.SourceTable)
	}

	intent = detectQueryIntent("show customer trend by month", snapshot.Tables[0])
	plan = buildSQLPlan(snapshot, "show customer trend by month", intent)
	if plan.SourceTable != "customers" {
		t.Fatalf("expected customer query to select customers, got %q", plan.SourceTable)
	}
}

func TestBuildCSVColumnsPreservesChineseHeadersAndSemantics(t *testing.T) {
	headers := []string{"时间", "网页标题", "URL分类", "目的端口"}
	rows := [][]string{
		{"2026/3/26 14:54", "高德地图", "旅游", "443"},
		{"2026/3/26 14:54", "支付宝", "网上交易", "443"},
	}

	columns := buildCSVColumns(headers, rows)
	if len(columns) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(columns))
	}
	if columns[0].Name != "时间" || columns[0].Semantic != "time" {
		t.Fatalf("expected 时间 to remain a time column, got %#v", columns[0])
	}
	if columns[2].Name != "url分类" || columns[2].Semantic != "dimension" {
		t.Fatalf("expected URL分类 to remain a dimension column, got %#v", columns[2])
	}
	if columns[3].Name != "目的端口" || columns[3].Semantic != "dimension" {
		t.Fatalf("expected 目的端口 to be treated as a dimension-like identifier, got %#v", columns[3])
	}
}

func TestBuildSQLPlanUsesCategoryCountsForChineseDistributionQuestions(t *testing.T) {
	table := tableSchema{
		TableName:   "web_browsing",
		SourceFile:  "web_browsing_complex.xlsx",
		SourceSheet: "浏览明细",
		Columns: []schemaColumn{
			{Name: "时间", Type: "TEXT", Semantic: "time"},
			{Name: "用户", Type: "TEXT", Semantic: "dimension"},
			{Name: "网页标题", Type: "TEXT", Semantic: "dimension"},
			{Name: "url分类", Type: "TEXT", Semantic: "dimension"},
			{Name: "目的端口", Type: "INTEGER", Semantic: "dimension"},
		},
	}
	snapshot := schemaSnapshot{Tables: []tableSchema{table}}

	intent := detectQueryIntent("帮我做个网页浏览的分布饼图", table)
	plan := buildSQLPlan(snapshot, "帮我做个网页浏览的分布饼图", intent)

	if plan.Mode != "share" {
		t.Fatalf("expected share mode for distribution pie chart, got %q", plan.Mode)
	}
	if plan.ChartType != "pie" {
		t.Fatalf("expected pie chart type, got %q", plan.ChartType)
	}
	if plan.DimensionColumn != "url分类" {
		t.Fatalf("expected planner to choose url分类, got %q", plan.DimensionColumn)
	}
	if strings.Contains(plan.SQL, "目的端口") {
		t.Fatalf("expected planner to avoid 目的端口 as metric, got %q", plan.SQL)
	}
	if !strings.Contains(plan.SQL, "COUNT(*) AS total_count") {
		t.Fatalf("expected share sql to fall back to row counts, got %q", plan.SQL)
	}
	if !strings.Contains(plan.SQL, "GROUP BY url分类") {
		t.Fatalf("expected share sql to group by url分类, got %q", plan.SQL)
	}
}

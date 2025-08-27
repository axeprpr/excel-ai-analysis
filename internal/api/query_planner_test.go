package api

import (
	"strings"
	"testing"
)

func TestBuildTrendSQLByGranularity(t *testing.T) {
	monthSQL := buildTrendSQL("sales", "order_date", "amount", "month")
	daySQL := buildTrendSQL("sales", "order_date", "amount", "day")
	yearSQL := buildTrendSQL("sales", "order_date", "amount", "year")

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
}

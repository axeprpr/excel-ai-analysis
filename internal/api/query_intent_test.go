package api

import "testing"

func TestDetectQueryIntent(t *testing.T) {
	table := tableSchema{
		TableName: "sales",
		Columns: []schemaColumn{
			{Name: "order_date", Type: "DATE", Semantic: "time"},
			{Name: "category", Type: "TEXT", Semantic: "dimension"},
			{Name: "amount", Type: "REAL", Semantic: "metric"},
		},
	}

	cases := []struct {
		question        string
		wantMode        string
		wantChart       string
		wantFilter      bool
		wantCompareType string
	}{
		{"show sales trend by month", "trend", "line", false, ""},
		{"top categories by revenue", "topn", "bar", false, ""},
		{"count rows this month", "count", "table", true, ""},
		{"show category share", "share", "pie", false, ""},
		{"compare category revenue", "compare", "bar", false, ""},
		{"同比销售额", "compare", "line", false, "yoy"},
		{"mom revenue", "compare", "line", false, "mom"},
		{"detail rows for east", "detail", "table", true, ""},
	}

	for _, tc := range cases {
		intent := detectQueryIntent(tc.question, table)
		if intent.Mode != tc.wantMode {
			t.Fatalf("question %q: expected mode %q, got %q", tc.question, tc.wantMode, intent.Mode)
		}
		if intent.ChartType != tc.wantChart {
			t.Fatalf("question %q: expected chart %q, got %q", tc.question, tc.wantChart, intent.ChartType)
		}
		if tc.wantFilter && len(intent.FilterHints) == 0 {
			t.Fatalf("question %q: expected filter hints", tc.question)
		}
		if intent.ComparisonType != tc.wantCompareType {
			t.Fatalf("question %q: expected comparison type %q, got %q", tc.question, tc.wantCompareType, intent.ComparisonType)
		}
	}
}

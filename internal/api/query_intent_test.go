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
		{"show sales trend by month", "trend", "table", false, ""},
		{"top categories by revenue", "topn", "table", false, ""},
		{"count rows this month", "count", "table", true, ""},
		{"show category share", "share", "table", false, ""},
		{"compare category revenue", "compare", "table", false, ""},
		{"可以生成一个柱状图吗", "topn", "bar", false, ""},
		{"请画一个折线图", "trend", "line", false, ""},
		{"给我一个饼图", "share", "pie", false, ""},
		{"同比销售额", "compare", "table", false, "yoy"},
		{"mom revenue", "compare", "table", false, "mom"},
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

func TestDetectTrendIntentWithoutMetricColumn(t *testing.T) {
	table := tableSchema{
		TableName: "webaccess",
		Columns: []schemaColumn{
			{Name: "时间", Type: "TEXT", Semantic: "time"},
			{Name: "终端类型", Type: "TEXT", Semantic: "dimension"},
			{Name: "URL分类", Type: "TEXT", Semantic: "dimension"},
		},
	}

	intent := detectQueryIntent("按时间统计访问趋势折线图", table)
	if intent.Mode != "trend" {
		t.Fatalf("expected trend mode, got %q", intent.Mode)
	}
	if intent.ChartType != "line" {
		t.Fatalf("expected line chart, got %q", intent.ChartType)
	}
}

func TestDetectQueryIntentSupportsTrendWithoutMetric(t *testing.T) {
	table := tableSchema{
		TableName: "webaccess",
		Columns: []schemaColumn{
			{Name: "时间", Type: "TEXT", Semantic: "time"},
			{Name: "终端类型", Type: "TEXT", Semantic: "dimension"},
		},
	}

	intent := detectQueryIntent("按时间统计访问趋势", table)
	if intent.Mode != "trend" {
		t.Fatalf("expected trend mode, got %q", intent.Mode)
	}
	if intent.ChartType != "table" {
		t.Fatalf("expected table chart type without explicit chart request, got %q", intent.ChartType)
	}
	if !intent.HasTimeReference {
		t.Fatalf("expected time reference to be set")
	}
}

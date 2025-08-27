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

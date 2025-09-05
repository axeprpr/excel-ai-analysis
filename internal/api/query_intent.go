package api

import "strings"

type queryIntent struct {
	Mode             string
	ChartType        string
	TimeGranularity  string
	Comparison       bool
	Share            bool
	Ranking          bool
	HasTimeReference bool
	FilterHints      []string
}

func detectQueryIntent(question string, table tableSchema) queryIntent {
	q := strings.ToLower(strings.TrimSpace(question))
	intent := queryIntent{
		Mode:        "detail",
		ChartType:   "table",
		FilterHints: detectFilterHints(q),
	}

	if hasAny(q, "同比", "环比", "compare", "comparison", "versus", "vs", "对比") {
		intent.Comparison = true
		intent.Mode = "compare"
		intent.ChartType = "bar"
	}
	if hasAny(q, "占比", "比例", "share", "distribution", "composition") {
		intent.Share = true
		intent.Mode = "share"
		intent.ChartType = "pie"
	}
	if hasAny(q, "top", "rank", "highest", "排名", "最高", "最多", "lowest", "最低") {
		intent.Ranking = true
		intent.Mode = "topn"
		intent.ChartType = "bar"
	}
	if hasAny(q, "count", "how many", "number of", "多少", "几个", "几条") {
		intent.Mode = "count"
	}
	if hasAny(q, "sum", "total", "amount", "总", "合计") && firstMetricColumn(table) != "" {
		intent.Mode = "aggregate"
	}
	if hasAny(q, "trend", "over time", "by month", "monthly", "by day", "daily", "按月", "按天", "趋势") &&
		firstTimeColumn(table) != "" && firstMetricColumn(table) != "" {
		intent.Mode = "trend"
		intent.ChartType = "line"
		intent.HasTimeReference = true
	}

	switch {
	case hasAny(q, "by month", "monthly", "month", "按月"):
		intent.TimeGranularity = "month"
	case hasAny(q, "by day", "daily", "day", "按天"):
		intent.TimeGranularity = "day"
	case hasAny(q, "by year", "yearly", "year", "按年"):
		intent.TimeGranularity = "year"
	}

	return intent
}

func normalizeIntentMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "detail", "aggregate", "topn", "trend", "count", "share", "compare":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return ""
	}
}

func detectFilterHints(question string) []string {
	hints := make([]string, 0, 4)
	for _, token := range []string{
		"today", "yesterday", "this month", "last month", "this year", "last year",
		"今年", "本月", "上月", "最近",
		"east", "west", "north", "south",
		"华东", "华南", "华北", "华西",
	} {
		if strings.Contains(question, token) {
			hints = append(hints, token)
		}
	}
	return hints
}

func hasAny(text string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

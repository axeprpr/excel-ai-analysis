package api

import "strings"

type plannedFilter struct {
	Column   string
	Operator string
	Value    string
}

type sqlPlan struct {
	SourceTable      string
	SourceFile       string
	SourceSheet      string
	Mode             string
	ChartType        string
	DimensionColumn string
	MetricColumn    string
	TimeColumn      string
	FilterHints     []string
	Filters         []plannedFilter
	SelectedColumns []string
	SQL             string
}

func buildSQLPlan(snapshot schemaSnapshot, question string, intent queryIntent) sqlPlan {
	if len(snapshot.Tables) == 0 {
		return sqlPlan{
			SourceTable:      "",
			SourceFile:       "",
			SourceSheet:      "",
			Mode:            intent.Mode,
			ChartType:       intent.ChartType,
			DimensionColumn: "",
			MetricColumn:    "",
			TimeColumn:      "",
			FilterHints:     intent.FilterHints,
			Filters:         nil,
			SelectedColumns: []string{},
			SQL:             "-- no imported tables available",
		}
	}

	table := snapshot.Tables[0]
	dimension := firstDimensionColumn(table)
	metric := firstMetricColumn(table)
	timeColumn := firstTimeColumn(table)
	filters := planFilters(table, intent)
	return sqlPlan{
		SourceTable:      table.TableName,
		SourceFile:       table.SourceFile,
		SourceSheet:      table.SourceSheet,
		Mode:             intent.Mode,
		ChartType:        intent.ChartType,
		DimensionColumn: dimension,
		MetricColumn:    metric,
		TimeColumn:      timeColumn,
		FilterHints:     intent.FilterHints,
		Filters:         filters,
		SelectedColumns: selectedColumnsForMode(table, intent.Mode),
		SQL:             buildSQLForIntent(table, intent, filters),
	}
}

func buildSQLForIntent(table tableSchema, intent queryIntent, filters []plannedFilter) string {
	dimension := firstDimensionColumn(table)
	metric := firstMetricColumn(table)
	whereClause := buildWhereClause(filters)

	switch intent.Mode {
	case "count":
		return "SELECT COUNT(*) AS total_count FROM " + table.TableName + whereClause + ";"
	case "trend":
		timeColumn := firstTimeColumn(table)
		if timeColumn != "" && metric != "" {
			return buildTrendSQL(table.TableName, timeColumn, metric, intent.TimeGranularity, whereClause)
		}
	case "topn":
		if dimension != "" && metric != "" {
			return "SELECT " + dimension + ", SUM(" + metric + ") AS total_value FROM " + table.TableName +
				whereClause + " GROUP BY " + dimension + " ORDER BY total_value DESC LIMIT 10;"
		}
	case "aggregate":
		if metric != "" {
			return "SELECT SUM(" + metric + ") AS total_value FROM " + table.TableName + whereClause + ";"
		}
	}

	if dimension != "" && metric != "" {
		return "SELECT " + dimension + ", " + metric + " FROM " + table.TableName + whereClause + " LIMIT 100;"
	}
	return buildPlaceholderSQL(schemaSnapshot{Tables: []tableSchema{table}})
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

package api

type sqlPlan struct {
	Mode            string
	SelectedColumns []string
	SQL             string
}

func buildSQLPlan(snapshot schemaSnapshot, question string, intent queryIntent) sqlPlan {
	if len(snapshot.Tables) == 0 {
		return sqlPlan{
			Mode:            intent.Mode,
			SelectedColumns: []string{},
			SQL:             "-- no imported tables available",
		}
	}

	table := snapshot.Tables[0]
	return sqlPlan{
		Mode:            intent.Mode,
		SelectedColumns: selectedColumnsForMode(table, intent.Mode),
		SQL:             buildSQLForIntent(table, intent),
	}
}

func buildSQLForIntent(table tableSchema, intent queryIntent) string {
	dimension := firstDimensionColumn(table)
	metric := firstMetricColumn(table)

	switch intent.Mode {
	case "count":
		return "SELECT COUNT(*) AS total_count FROM " + table.TableName + ";"
	case "trend":
		timeColumn := firstTimeColumn(table)
		if timeColumn != "" && metric != "" {
			return buildTrendSQL(table.TableName, timeColumn, metric, intent.TimeGranularity)
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
	return buildPlaceholderSQL(schemaSnapshot{Tables: []tableSchema{table}})
}

func buildTrendSQL(tableName, timeColumn, metric, granularity string) string {
	switch granularity {
	case "day":
		return "SELECT substr(" + timeColumn + ", 1, 10) AS time_bucket, SUM(" + metric + ") AS total_value FROM " + tableName +
			" GROUP BY time_bucket ORDER BY time_bucket ASC;"
	case "year":
		return "SELECT substr(" + timeColumn + ", 1, 4) AS time_bucket, SUM(" + metric + ") AS total_value FROM " + tableName +
			" GROUP BY time_bucket ORDER BY time_bucket ASC;"
	default:
		return "SELECT substr(" + timeColumn + ", 1, 7) AS time_bucket, SUM(" + metric + ") AS total_value FROM " + tableName +
			" GROUP BY time_bucket ORDER BY time_bucket ASC;"
	}
}

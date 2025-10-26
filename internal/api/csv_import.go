package api

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const csvInsertBatchSize = 500
const csvInsertMaxStatementBytes = 64 << 10

func importCSVIntoSQLite(databasePath, filePath, tableName string) (tableSchema, error) {
	file, err := openCSVFile(filePath)
	if err != nil {
		return tableSchema{}, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	headers, err := reader.Read()
	if err != nil {
		if err == io.EOF {
			return tableSchema{}, fmt.Errorf("csv file is empty")
		}
		return tableSchema{}, err
	}

	sampleRows, err := readCSVSampleRows(reader, 1000)
	if err != nil {
		return tableSchema{}, err
	}

	columns := buildCSVColumns(headers, sampleRows)
	createSQL := buildCreateTableSQL(tableName, columns)
	if err := execSQLite(databasePath, createSQL); err != nil {
		return tableSchema{}, err
	}
	if err := insertCSVRowsInBatches(databasePath, filePath, tableName, columns, csvInsertBatchSize); err != nil {
		return tableSchema{}, err
	}

	return tableSchema{
		TableName:   tableName,
		SourceFile:  filepath.Base(filePath),
		SourceSheet: "csv",
		Columns:     columns,
	}, nil
}

func readCSVSampleRows(reader *csv.Reader, limit int) ([][]string, error) {
	samples := make([][]string, 0, limit)
	for len(samples) < limit {
		record, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		samples = append(samples, record)
	}
	return samples, nil
}

func insertCSVRowsInBatches(databasePath, filePath, tableName string, columns []schemaColumn, batchSize int) error {
	file, err := openCSVFile(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	if _, err := reader.Read(); err != nil {
		return err
	}

	batch := make([][]string, 0, batchSize)
	batchBytes := 0
	for {
		record, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		recordBytes := estimatedCSVRecordStatementBytes(record)
		if len(batch) > 0 && (len(batch) >= batchSize || batchBytes+recordBytes > csvInsertMaxStatementBytes) {
			if err := execSQLite(databasePath, buildInsertRowsSQL(tableName, columns, batch)); err != nil {
				return err
			}
			batch = batch[:0]
			batchBytes = 0
		}
		batch = append(batch, record)
		batchBytes += recordBytes
	}

	if len(batch) == 0 {
		return nil
	}
	return execSQLite(databasePath, buildInsertRowsSQL(tableName, columns, batch))
}

func estimatedCSVRecordStatementBytes(record []string) int {
	size := 64
	for _, value := range record {
		size += len(value) + 8
	}
	return size
}

func openCSVFile(filePath string) (io.ReadCloser, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}

	reader := bufio.NewReader(file)
	peek, err := reader.Peek(4096)
	if err != nil && err != io.EOF {
		_ = file.Close()
		return nil, err
	}

	if hasUTF8BOM(peek) {
		if _, err := reader.Discard(3); err != nil {
			_ = file.Close()
			return nil, err
		}
		return &wrappedReadCloser{Reader: reader, Closer: file}, nil
	}

	if utf8.Valid(peek) {
		return &wrappedReadCloser{Reader: reader, Closer: file}, nil
	}

	decoder := transform.NewReader(reader, simplifiedchinese.GB18030.NewDecoder())
	return &wrappedReadCloser{Reader: decoder, Closer: file}, nil
}

type wrappedReadCloser struct {
	io.Reader
	io.Closer
}

func hasUTF8BOM(peek []byte) bool {
	return len(peek) >= 3 && bytes.Equal(peek[:3], []byte{0xEF, 0xBB, 0xBF})
}

func buildCSVColumns(headers []string, rows [][]string) []schemaColumn {
	used := map[string]int{}
	columns := make([]schemaColumn, 0, len(headers))
	for idx, header := range headers {
		name := normalizeColumnName(header, idx, used)
		colType := inferCSVColumnType(idx, rows)
		columns = append(columns, schemaColumn{
			Name:     name,
			Type:     colType,
			Semantic: inferColumnSemantic(name, colType),
		})
	}
	return columns
}

func normalizeColumnName(header string, idx int, used map[string]int) string {
	base := strings.TrimSpace(strings.ToLower(header))
	if base == "" {
		base = fmt.Sprintf("column_%d", idx+1)
	}

	var b strings.Builder
	lastUnderscore := false
	for _, r := range base {
		switch {
		case unicode.IsLetter(r):
			b.WriteRune(r)
			lastUnderscore = false
		case unicode.IsDigit(r):
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}

	name := strings.Trim(b.String(), "_")
	if name == "" {
		name = fmt.Sprintf("column_%d", idx+1)
	}

	used[name]++
	if used[name] > 1 {
		name = fmt.Sprintf("%s_%d", name, used[name])
	}
	return name
}

func inferCSVColumnType(idx int, rows [][]string) string {
	seen := false
	allInt := true
	allNumber := true

	for _, row := range rows {
		if idx >= len(row) {
			continue
		}
		value := strings.TrimSpace(row[idx])
		if isMissingCellValue(value) {
			continue
		}
		seen = true
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			allInt = false
		}
		if _, err := strconv.ParseFloat(value, 64); err != nil {
			allNumber = false
		}
	}

	switch {
	case !seen:
		return "TEXT"
	case allInt:
		return "INTEGER"
	case allNumber:
		return "REAL"
	default:
		return "TEXT"
	}
}

func inferColumnSemantic(name, colType string) string {
	lower := strings.ToLower(name)
	switch {
	case containsAny(lower, []string{
		"date", "time", "month", "year", "day",
		"日期", "时间", "年月", "月份", "年度", "天",
	}):
		return "time"
	case colType == "INTEGER" || colType == "REAL":
		if containsAny(lower, []string{
			"id", "code", "port", "user_group",
			"编号", "代码", "端口", "用户组", "序号",
		}) {
			return "dimension"
		}
		if containsAny(lower, []string{
			"amount", "revenue", "price", "total", "count", "qty", "quantity", "score", "value",
			"金额", "价格", "总额", "总量", "数量", "次数", "分数", "值", "占比",
		}) {
			return "metric"
		}
		if strings.HasPrefix(lower, "column_") {
			return "metric"
		}
		return "metric"
	default:
		return "dimension"
	}
}

func buildCreateTableSQL(tableName string, columns []schemaColumn) string {
	defs := make([]string, 0, len(columns))
	for _, column := range columns {
		defs = append(defs, sqliteIdent(column.Name)+" "+column.Type)
	}
	return "DROP TABLE IF EXISTS " + sqliteIdent(tableName) + ";\n" +
		"CREATE TABLE " + sqliteIdent(tableName) + " (" + strings.Join(defs, ", ") + ");"
}

func buildInsertRowsSQL(tableName string, columns []schemaColumn, rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}

	names := make([]string, 0, len(columns))
	for _, column := range columns {
		names = append(names, sqliteIdent(column.Name))
	}

	statements := []string{"BEGIN;"}
	for _, row := range rows {
		values := make([]string, 0, len(columns))
		for idx, column := range columns {
			value := ""
			if idx < len(row) {
				value = strings.TrimSpace(row[idx])
			}
			values = append(values, formatSQLiteValue(value, column.Type))
		}
		statements = append(statements,
			"INSERT INTO "+sqliteIdent(tableName)+" ("+strings.Join(names, ", ")+") VALUES ("+strings.Join(values, ", ")+");",
		)
	}
	statements = append(statements, "COMMIT;")
	return strings.Join(statements, "\n")
}

func formatSQLiteValue(value, colType string) string {
	if isMissingCellValue(value) {
		return "NULL"
	}

	switch colType {
	case "INTEGER", "REAL":
		return value
	default:
		return sqliteQuote(value)
	}
}

func isMissingCellValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "n/a", "na", "null", "-", "--":
		return true
	default:
		return false
	}
}

func sqliteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

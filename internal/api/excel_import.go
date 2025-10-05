package api

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

const (
	xlsxTypeInferenceSampleRows = 1000
	xlsxInsertBatchSize         = 500
)

func importXLSXIntoSQLite(databasePath, filePath, tableName string) ([]tableSchema, error) {
	workbook, err := excelize.OpenFile(filePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = workbook.Close() }()

	sheets := workbook.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("xlsx file has no sheets")
	}

	schemas := make([]tableSchema, 0, len(sheets))
	for _, sheet := range sheets {
		headers, sampleRows, err := sampleXLSXSheetRows(workbook, sheet, xlsxTypeInferenceSampleRows)
		if err != nil {
			return nil, err
		}
		if len(headers) == 0 {
			continue
		}

		sheetTableName := deriveSheetTableName(tableName, sheet, len(schemas))
		columns := buildCSVColumns(headers, sampleRows)
		if err := execSQLite(databasePath, buildCreateTableSQL(sheetTableName, columns)); err != nil {
			return nil, err
		}
		if err := insertXLSXSheetRowsInBatches(workbook, databasePath, sheet, sheetTableName, columns, headers, xlsxInsertBatchSize); err != nil {
			return nil, err
		}

		schemas = append(schemas, tableSchema{
			TableName:   sheetTableName,
			SourceFile:  filepath.Base(filePath),
			SourceSheet: sheet,
			Columns:     columns,
		})
	}
	if len(schemas) == 0 {
		return nil, fmt.Errorf("xlsx workbook has no non-empty sheets")
	}
	return schemas, nil
}

func sampleXLSXSheetRows(workbook *excelize.File, sheet string, limit int) ([]string, [][]string, error) {
	rows, err := workbook.Rows(sheet)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	var headers []string
	var pendingTitleRows [][]string
	for rows.Next() {
		columns, err := rows.Columns()
		if err != nil {
			return nil, nil, err
		}
		if isEmptySpreadsheetRow(columns) {
			continue
		}
		if !looksLikeHeaderRow(columns) {
			pendingTitleRows = append(pendingTitleRows, columns)
			continue
		}
		headers = columns
		break
	}
	if len(headers) == 0 {
		return nil, nil, nil
	}

	samples := make([][]string, 0, limit)
	for rows.Next() && len(samples) < limit {
		columns, err := rows.Columns()
		if err != nil {
			return nil, nil, err
		}
		if isEmptySpreadsheetRow(columns) {
			continue
		}
		if spreadsheetRowsEqual(columns, headers) {
			continue
		}
		samples = append(samples, columns)
	}
	return headers, samples, nil
}

func insertXLSXSheetRowsInBatches(workbook *excelize.File, databasePath, sheet, tableName string, columns []schemaColumn, headers []string, batchSize int) error {
	rows, err := workbook.Rows(sheet)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	foundHeader := false
	for rows.Next() {
		rowValues, err := rows.Columns()
		if err != nil {
			return err
		}
		if isEmptySpreadsheetRow(rowValues) {
			continue
		}
		if !looksLikeHeaderRow(rowValues) {
			continue
		}
		if !spreadsheetRowsEqual(rowValues, headers) {
			continue
		}
		foundHeader = true
		break
	}
	if !foundHeader {
		return nil
	}

	batch := make([][]string, 0, batchSize)
	for rows.Next() {
		values, err := rows.Columns()
		if err != nil {
			return err
		}
		if isEmptySpreadsheetRow(values) {
			continue
		}
		if spreadsheetRowsEqual(values, headers) {
			continue
		}
		batch = append(batch, values)
		if len(batch) >= batchSize {
			if err := execSQLite(databasePath, buildInsertRowsSQL(tableName, columns, batch)); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
	if len(batch) == 0 {
		return nil
	}
	return execSQLite(databasePath, buildInsertRowsSQL(tableName, columns, batch))
}

func looksLikeHeaderRow(values []string) bool {
	nonEmpty := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			nonEmpty++
		}
	}
	return nonEmpty >= 2
}

func isEmptySpreadsheetRow(values []string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}

func spreadsheetRowsEqual(left, right []string) bool {
	maxLen := len(left)
	if len(right) > maxLen {
		maxLen = len(right)
	}
	for i := 0; i < maxLen; i++ {
		if normalizedSpreadsheetCell(left, i) != normalizedSpreadsheetCell(right, i) {
			return false
		}
	}
	return true
}

func normalizedSpreadsheetCell(values []string, index int) string {
	if index >= len(values) {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(values[index]))
}

func deriveSheetTableName(baseTableName, sheet string, importedCount int) string {
	sheetToken := deriveTableName(sheet)
	if importedCount == 0 && (sheetToken == "sheet1" || sheetToken == "") {
		return baseTableName
	}
	if sheetToken == "" {
		sheetToken = fmt.Sprintf("sheet_%d", importedCount+1)
	}
	if strings.HasSuffix(baseTableName, "_"+sheetToken) {
		return baseTableName
	}
	return baseTableName + "_" + sheetToken
}

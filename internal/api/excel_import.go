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
		if err := insertXLSXSheetRowsInBatches(workbook, databasePath, sheet, sheetTableName, columns, xlsxInsertBatchSize); err != nil {
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

	if !rows.Next() {
		return nil, nil, nil
	}
	headers, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}

	samples := make([][]string, 0, limit)
	for rows.Next() && len(samples) < limit {
		columns, err := rows.Columns()
		if err != nil {
			return nil, nil, err
		}
		samples = append(samples, columns)
	}
	return headers, samples, nil
}

func insertXLSXSheetRowsInBatches(workbook *excelize.File, databasePath, sheet, tableName string, columns []schemaColumn, batchSize int) error {
	rows, err := workbook.Rows(sheet)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return nil
	}
	if _, err := rows.Columns(); err != nil {
		return err
	}

	batch := make([][]string, 0, batchSize)
	for rows.Next() {
		values, err := rows.Columns()
		if err != nil {
			return err
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

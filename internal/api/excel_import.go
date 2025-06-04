package api

import (
	"fmt"
	"path/filepath"

	"github.com/xuri/excelize/v2"
)

func importXLSXIntoSQLite(databasePath, filePath, tableName string) (tableSchema, error) {
	workbook, err := excelize.OpenFile(filePath)
	if err != nil {
		return tableSchema{}, err
	}
	defer func() { _ = workbook.Close() }()

	sheets := workbook.GetSheetList()
	if len(sheets) == 0 {
		return tableSchema{}, fmt.Errorf("xlsx file has no sheets")
	}

	sheet := sheets[0]
	rows, err := workbook.GetRows(sheet)
	if err != nil {
		return tableSchema{}, err
	}
	if len(rows) == 0 {
		return tableSchema{}, fmt.Errorf("xlsx sheet is empty")
	}

	headers := rows[0]
	dataRows := [][]string{}
	if len(rows) > 1 {
		dataRows = rows[1:]
	}

	columns := buildCSVColumns(headers, dataRows)
	createSQL := buildCreateTableSQL(tableName, columns)
	insertSQL := buildInsertRowsSQL(tableName, columns, dataRows)
	if err := execSQLite(databasePath, createSQL+"\n"+insertSQL); err != nil {
		return tableSchema{}, err
	}

	return tableSchema{
		TableName:   tableName,
		SourceFile:  filepath.Base(filePath),
		SourceSheet: sheet,
		Columns:     columns,
	}, nil
}

package api

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
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
		rows, err := workbook.GetRows(sheet)
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			continue
		}

		headers := rows[0]
		dataRows := [][]string{}
		if len(rows) > 1 {
			dataRows = rows[1:]
		}

		sheetTableName := deriveSheetTableName(tableName, sheet, len(schemas))
		columns := buildCSVColumns(headers, dataRows)
		createSQL := buildCreateTableSQL(sheetTableName, columns)
		insertSQL := buildInsertRowsSQL(sheetTableName, columns, dataRows)
		if err := execSQLite(databasePath, createSQL+"\n"+insertSQL); err != nil {
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

package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func TestCreateAndListSessions(t *testing.T) {
	dataDir := t.TempDir()
	handler := NewHandler(dataDir)

	createReq := httptest.NewRequest(http.MethodPost, "/api/sessions", nil)
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, createRec.Code)
	}

	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("expected session_id in create response")
	}

	sessionDB := filepath.Join(dataDir, "sessions", sessionID, "session.db")
	if _, err := os.Stat(sessionDB); err != nil {
		t.Fatalf("expected session db to exist: %v", err)
	}

	cmd := exec.Command(
		"sqlite3", "-cmd", ".timeout 2000", sessionDB,
		"SELECT name FROM sqlite_master WHERE type='table' AND name='session_meta';",
	)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to inspect sqlite db: %v", err)
	}
	if string(bytes.TrimSpace(output)) != "session_meta" {
		t.Fatalf("expected session_meta table to exist, got %q", string(bytes.TrimSpace(output)))
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)

	if listRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, listRec.Code)
	}

	var listed struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("failed to decode list response: %v", err)
	}

	if len(listed.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(listed.Sessions))
	}
	if listed.Sessions[0]["uploaded_file_count"] != float64(0) {
		t.Fatalf("expected uploaded_file_count to be 0, got %v", listed.Sessions[0]["uploaded_file_count"])
	}
	if listed.Sessions[0]["table_count"] != float64(0) {
		t.Fatalf("expected table_count to be 0, got %v", listed.Sessions[0]["table_count"])
	}
	if listed.Sessions[0]["import_task_count"] != float64(0) {
		t.Fatalf("expected import_task_count to be 0, got %v", listed.Sessions[0]["import_task_count"])
	}
	if listed.Sessions[0]["total_row_count"] != float64(0) {
		t.Fatalf("expected total_row_count to be 0, got %v", listed.Sessions[0]["total_row_count"])
	}
}

func TestUploadRejectsUnsupportedFileType(t *testing.T) {
	dataDir := t.TempDir()
	handler := NewHandler(dataDir)

	createReq := httptest.NewRequest(http.MethodPost, "/api/sessions", nil)
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, createRec.Code)
	}

	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("expected session_id in create response")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "notes.txt")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := part.Write([]byte("not a spreadsheet")); err != nil {
		t.Fatalf("failed to write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/files/upload", &body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	handler.ServeHTTP(uploadRec, uploadReq)

	if uploadRec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, uploadRec.Code)
	}
}

func TestXLSUploadReturnsPlaceholderWarning(t *testing.T) {
	dataDir := t.TempDir()
	handler := NewHandler(dataDir)

	createReq := httptest.NewRequest(http.MethodPost, "/api/sessions", nil)
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, createRec.Code)
	}

	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("expected session_id in create response")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "legacy.xls")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := part.Write([]byte("legacy xls placeholder")); err != nil {
		t.Fatalf("failed to write xls payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/files/upload", &body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	handler.ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, uploadRec.Code)
	}

	var uploadResp map[string]any
	if err := json.Unmarshal(uploadRec.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("failed to decode upload response: %v", err)
	}

	warnings, ok := uploadResp["warnings"].([]any)
	if !ok || len(warnings) == 0 {
		t.Fatalf("expected upload response warnings for xls import, got %v", uploadResp["warnings"])
	}

	taskID, _ := uploadResp["task_id"].(string)
	if taskID == "" {
		t.Fatalf("expected task_id in upload response")
	}
	files, ok := uploadResp["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("expected upload response files metadata, got %v", uploadResp["files"])
	}
	firstFile, ok := files[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first upload file entry to be an object")
	}
	if firstFile["extension"] != ".xls" {
		t.Fatalf("expected upload file extension to be .xls, got %v", firstFile["extension"])
	}

	waitForImportTaskStatus(t, handler, sessionID, taskID, "completed")

	importReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID+"/imports/"+taskID, nil)
	importRec := httptest.NewRecorder()
	handler.ServeHTTP(importRec, importReq)
	if importRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, importRec.Code)
	}

	var importResp map[string]any
	if err := json.Unmarshal(importRec.Body.Bytes(), &importResp); err != nil {
		t.Fatalf("failed to decode import response: %v", err)
	}

	taskWarnings, ok := importResp["warnings"].([]any)
	if !ok || len(taskWarnings) == 0 {
		t.Fatalf("expected import task warnings for xls import, got %v", importResp["warnings"])
	}
	if importResp["warning_count"] != float64(1) {
		t.Fatalf("expected import warning_count to be 1, got %v", importResp["warning_count"])
	}
	if importResp["duration_ms"] == nil {
		t.Fatalf("expected import duration_ms to be populated")
	}

	importsReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID+"/imports", nil)
	importsRec := httptest.NewRecorder()
	handler.ServeHTTP(importsRec, importsReq)
	if importsRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, importsRec.Code)
	}

	var importsResp map[string]any
	if err := json.Unmarshal(importsRec.Body.Bytes(), &importsResp); err != nil {
		t.Fatalf("failed to decode imports response: %v", err)
	}
	if importsResp["task_count"] != float64(1) {
		t.Fatalf("expected imports task_count to be 1, got %v", importsResp["task_count"])
	}
	if importsResp["warning_count_total"] != float64(1) {
		t.Fatalf("expected imports warning_count_total to be 1, got %v", importsResp["warning_count_total"])
	}
	statusCounts, ok := importsResp["status_counts"].(map[string]any)
	if !ok {
		t.Fatalf("expected imports status_counts to be an object, got %v", importsResp["status_counts"])
	}
	if statusCounts["completed"] != float64(1) {
		t.Fatalf("expected imports completed count to be 1, got %v", statusCounts["completed"])
	}
	tasks, ok := importsResp["tasks"].([]any)
	if !ok || len(tasks) == 0 {
		t.Fatalf("expected tasks in imports response, got %v", importsResp["tasks"])
	}
	firstTask, ok := tasks[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first imports entry to be an object")
	}
	if firstTask["warning_count"] != float64(1) {
		t.Fatalf("expected imports warning_count to be 1, got %v", firstTask["warning_count"])
	}
	if firstTask["duration_ms"] == nil {
		t.Fatalf("expected imports duration_ms to be populated")
	}

	queryBody := bytes.NewBufferString(`{"question":"How many rows are in the legacy file?"}`)
	queryReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/query", queryBody)
	queryReq.Header.Set("Content-Type", "application/json")
	queryRec := httptest.NewRecorder()
	handler.ServeHTTP(queryRec, queryReq)
	if queryRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, queryRec.Code)
	}

	var queryResp map[string]any
	if err := json.Unmarshal(queryRec.Body.Bytes(), &queryResp); err != nil {
		t.Fatalf("failed to decode query response: %v", err)
	}

	queryWarnings, ok := queryResp["warnings"].([]any)
	if !ok || len(queryWarnings) < 2 {
		t.Fatalf("expected query warnings for xls import, got %v", queryResp["warnings"])
	}
	foundPlaceholderWarning := false
	for _, item := range queryWarnings {
		text, _ := item.(string)
		if strings.Contains(text, "legacy .xls") {
			foundPlaceholderWarning = true
			break
		}
	}
	if !foundPlaceholderWarning {
		t.Fatalf("expected xls-specific placeholder warning in query response, got %v", queryResp["warnings"])
	}
}

func TestUploadCreatesImportTaskAndSchema(t *testing.T) {
	dataDir := t.TempDir()
	handler := NewHandler(dataDir)

	createReq := httptest.NewRequest(http.MethodPost, "/api/sessions", nil)
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, createRec.Code)
	}

	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("expected session_id in create response")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "sales.xlsx")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := part.Write([]byte("placeholder excel content")); err != nil {
		t.Fatalf("failed to write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/files/upload", &body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	handler.ServeHTTP(uploadRec, uploadReq)

	if uploadRec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, uploadRec.Code)
	}

	var uploadResp map[string]any
	if err := json.Unmarshal(uploadRec.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("failed to decode upload response: %v", err)
	}

	taskID, _ := uploadResp["task_id"].(string)
	if taskID == "" {
		t.Fatalf("expected task_id in upload response")
	}
	files, ok := uploadResp["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("expected upload response files metadata, got %v", uploadResp["files"])
	}
	firstFile, ok := files[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first upload file entry to be an object")
	}
	if firstFile["extension"] != ".xlsx" {
		t.Fatalf("expected upload file extension to be .xlsx, got %v", firstFile["extension"])
	}

	waitForImportTaskStatus(t, handler, sessionID, taskID, "completed")

	schemaReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID+"/schema", nil)
	schemaRec := httptest.NewRecorder()
	handler.ServeHTTP(schemaRec, schemaReq)

	if schemaRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, schemaRec.Code)
	}

	var schemaResp struct {
		Tables []map[string]any `json:"tables"`
	}
	if err := json.Unmarshal(schemaRec.Body.Bytes(), &schemaResp); err != nil {
		t.Fatalf("failed to decode schema response: %v", err)
	}

	if len(schemaResp.Tables) == 0 {
		t.Fatalf("expected at least one table in schema response")
	}
	firstTable := schemaResp.Tables[0]
	if firstTable["row_count"] == nil {
		t.Fatalf("expected schema response to include row_count from sqlite catalog")
	}
	columns, ok := firstTable["columns"].([]any)
	if !ok || len(columns) == 0 {
		t.Fatalf("expected structured columns in schema response")
	}

	sessionDB := filepath.Join(dataDir, "sessions", sessionID, "session.db")
	statusOutput, err := exec.Command(
		"sqlite3", "-cmd", ".timeout 2000", sessionDB,
		"SELECT value FROM session_meta WHERE key='status';",
	).Output()
	if err != nil {
		t.Fatalf("failed to read session status from sqlite: %v", err)
	}
	if string(bytes.TrimSpace(statusOutput)) != "ready" {
		t.Fatalf("expected sqlite session status to be ready, got %q", string(bytes.TrimSpace(statusOutput)))
	}

	tablesOutput, err := exec.Command(
		"sqlite3", "-cmd", ".timeout 2000", sessionDB,
		"SELECT value FROM session_meta WHERE key='tables';",
	).Output()
	if err != nil {
		t.Fatalf("failed to read session tables from sqlite: %v", err)
	}
	if string(bytes.TrimSpace(tablesOutput)) == "" {
		t.Fatalf("expected sqlite session tables to be populated")
	}

	taskStatusOutput, err := sqliteQueryWithRetry(sessionDB, "SELECT status FROM import_tasks WHERE task_id="+sqliteQuote(taskID)+";")
	if err != nil {
		t.Fatalf("failed to read import task status from sqlite: %v", err)
	}
	if string(bytes.TrimSpace(taskStatusOutput)) != "completed" {
		t.Fatalf("expected sqlite import task status to be completed, got %q", string(bytes.TrimSpace(taskStatusOutput)))
	}

	taskFilesOutput, err := sqliteQueryWithRetry(sessionDB, "SELECT file_names FROM import_tasks WHERE task_id="+sqliteQuote(taskID)+";")
	if err != nil {
		t.Fatalf("failed to read import task files from sqlite: %v", err)
	}
	if string(bytes.TrimSpace(taskFilesOutput)) == "" {
		t.Fatalf("expected sqlite import task file names to be populated")
	}
}

func TestQueryReturnsSchemaAwarePlaceholderResponse(t *testing.T) {
	dataDir := t.TempDir()
	handler := NewHandler(dataDir)

	createReq := httptest.NewRequest(http.MethodPost, "/api/sessions", nil)
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, createRec.Code)
	}

	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("expected session_id in create response")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "sales.xlsx")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := part.Write([]byte("placeholder excel content")); err != nil {
		t.Fatalf("failed to write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/files/upload", &body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	handler.ServeHTTP(uploadRec, uploadReq)

	if uploadRec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, uploadRec.Code)
	}

	var uploadResp map[string]any
	if err := json.Unmarshal(uploadRec.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("failed to decode upload response: %v", err)
	}

	taskID, _ := uploadResp["task_id"].(string)
	if taskID == "" {
		t.Fatalf("expected task_id in upload response")
	}

	waitForImportTaskStatus(t, handler, sessionID, taskID, "completed")

	queryBody := bytes.NewBufferString(`{"question":"What is the top sales category?"}`)
	queryReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/query", queryBody)
	queryReq.Header.Set("Content-Type", "application/json")
	queryRec := httptest.NewRecorder()
	handler.ServeHTTP(queryRec, queryReq)

	if queryRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, queryRec.Code)
	}

	var queryResp map[string]any
	if err := json.Unmarshal(queryRec.Body.Bytes(), &queryResp); err != nil {
		t.Fatalf("failed to decode query response: %v", err)
	}

	sql, _ := queryResp["sql"].(string)
	if sql == "" {
		t.Fatalf("expected sql in query response")
	}

	columns, ok := queryResp["columns"].([]any)
	if !ok || len(columns) == 0 {
		t.Fatalf("expected non-empty columns in query response")
	}

	rows, ok := queryResp["rows"].([]any)
	if !ok || len(rows) == 0 {
		t.Fatalf("expected non-empty rows in query response")
	}

	visualization, ok := queryResp["visualization"].(map[string]any)
	if !ok {
		t.Fatalf("expected visualization object in query response")
	}

	if visualization["type"] == "" {
		t.Fatalf("expected visualization type in query response")
	}

	queryPlan, ok := queryResp["query_plan"].(map[string]any)
	if !ok {
		t.Fatalf("expected query_plan object in query response")
	}

	sourceTable, _ := queryPlan["source_table"].(string)
	if sourceTable == "" {
		t.Fatalf("expected source_table in query_plan")
	}
	sourceFile, _ := queryPlan["source_file"].(string)
	if sourceFile == "" {
		t.Fatalf("expected source_file in query_plan")
	}
	sourceSheet, _ := queryPlan["source_sheet"].(string)
	if sourceSheet == "" {
		t.Fatalf("expected source_sheet in query_plan")
	}

	selectedColumns, ok := queryPlan["selected_columns"].([]any)
	if !ok || len(selectedColumns) == 0 {
		t.Fatalf("expected selected_columns in query_plan")
	}
}

func TestDatabaseInspectionReturnsSQLiteTables(t *testing.T) {
	dataDir := t.TempDir()
	handler := NewHandler(dataDir)

	createReq := httptest.NewRequest(http.MethodPost, "/api/sessions", nil)
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, createRec.Code)
	}

	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("expected session_id in create response")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "sales.xlsx")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := part.Write([]byte("placeholder excel content")); err != nil {
		t.Fatalf("failed to write form file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/files/upload", &body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	handler.ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, uploadRec.Code)
	}

	var uploadResp map[string]any
	if err := json.Unmarshal(uploadRec.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("failed to decode upload response: %v", err)
	}

	taskID, _ := uploadResp["task_id"].(string)
	if taskID == "" {
		t.Fatalf("expected task_id in upload response")
	}

	waitForImportTaskStatus(t, handler, sessionID, taskID, "completed")

	dbReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID+"/database", nil)
	dbRec := httptest.NewRecorder()
	handler.ServeHTTP(dbRec, dbReq)
	if dbRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, dbRec.Code)
	}

	var dbResp map[string]any
	if err := json.Unmarshal(dbRec.Body.Bytes(), &dbResp); err != nil {
		t.Fatalf("failed to decode database response: %v", err)
	}

	sqliteTables, ok := dbResp["sqlite_tables"].([]any)
	if !ok || len(sqliteTables) == 0 {
		t.Fatalf("expected sqlite_tables in database response")
	}

	catalog, ok := dbResp["catalog"].([]any)
	if !ok || len(catalog) == 0 {
		t.Fatalf("expected catalog in database response")
	}

	importTasks, ok := dbResp["import_tasks"].([]any)
	if !ok || len(importTasks) == 0 {
		t.Fatalf("expected import_tasks in database response")
	}
	if dbResp["table_count"] == nil {
		t.Fatalf("expected table_count in database response")
	}
	if dbResp["total_row_count"] == nil {
		t.Fatalf("expected total_row_count in database response")
	}
	if dbResp["import_task_count"] == nil {
		t.Fatalf("expected import_task_count in database response")
	}

	hasSessionMeta := false
	hasImportTasks := false
	hasImportedTables := false
	hasImportedColumns := false
	for _, table := range sqliteTables {
		name, _ := table.(string)
		if name == "session_meta" {
			hasSessionMeta = true
		}
		if name == "import_tasks" {
			hasImportTasks = true
		}
		if name == "imported_tables" {
			hasImportedTables = true
		}
		if name == "imported_columns" {
			hasImportedColumns = true
		}
	}

	if !hasSessionMeta || !hasImportTasks || !hasImportedTables || !hasImportedColumns {
		t.Fatalf("expected sqlite tables to include session_meta, import_tasks, imported_tables, and imported_columns, got %v", sqliteTables)
	}

	firstTable, ok := catalog[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first catalog entry to be an object")
	}
	if firstTable["table_name"] == "" {
		t.Fatalf("expected catalog table_name to be populated")
	}
	if firstTable["row_count"] == nil {
		t.Fatalf("expected catalog row_count to be populated")
	}
	if _, ok := firstTable["preview_rows"].([]any); !ok {
		t.Fatalf("expected catalog preview_rows to be populated")
	}
	columns, ok := firstTable["columns"].([]any)
	if !ok || len(columns) == 0 {
		t.Fatalf("expected catalog columns to be populated")
	}

	firstTask, ok := importTasks[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first import task entry to be an object")
	}
	if firstTask["status"] == "" {
		t.Fatalf("expected import task status to be populated")
	}
}

func TestCSVUploadImportsRowsIntoSQLite(t *testing.T) {
	dataDir := t.TempDir()
	handler := NewHandler(dataDir)

	createReq := httptest.NewRequest(http.MethodPost, "/api/sessions", nil)
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, createRec.Code)
	}

	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("expected session_id in create response")
	}

	csvData := "order_date,category,amount\n2025-01-01,A,10\n2025-01-02,B,20\n"
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "sales.csv")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := part.Write([]byte(csvData)); err != nil {
		t.Fatalf("failed to write csv data: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/files/upload", &body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	handler.ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, uploadRec.Code)
	}

	var uploadResp map[string]any
	if err := json.Unmarshal(uploadRec.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("failed to decode upload response: %v", err)
	}
	taskID, _ := uploadResp["task_id"].(string)
	if taskID == "" {
		t.Fatalf("expected task_id in upload response")
	}

	waitForImportTaskStatus(t, handler, sessionID, taskID, "completed")

	filesReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID+"/files", nil)
	filesRec := httptest.NewRecorder()
	handler.ServeHTTP(filesRec, filesReq)
	if filesRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, filesRec.Code)
	}

	var filesResp map[string]any
	if err := json.Unmarshal(filesRec.Body.Bytes(), &filesResp); err != nil {
		t.Fatalf("failed to decode files response: %v", err)
	}
	if filesResp["file_count"] != float64(1) {
		t.Fatalf("expected file_count to be 1, got %v", filesResp["file_count"])
	}
	filesList, ok := filesResp["files"].([]any)
	if !ok || len(filesList) != 1 {
		t.Fatalf("expected one file in files response, got %v", filesResp["files"])
	}
	firstListedFile, ok := filesList[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first files entry to be an object")
	}
	if firstListedFile["extension"] != ".csv" {
		t.Fatalf("expected listed file extension to be .csv, got %v", firstListedFile["extension"])
	}
	if filesResp["total_size"] == nil {
		t.Fatalf("expected total_size in files response")
	}

	sessionDB := filepath.Join(dataDir, "sessions", sessionID, "session.db")
	rowCountOutput, err := sqliteQueryWithRetry(sessionDB, `SELECT COUNT(*) FROM "sales";`)
	if err != nil {
		t.Fatalf("failed to count imported csv rows: %v", err)
	}
	if string(bytes.TrimSpace(rowCountOutput)) != "2" {
		t.Fatalf("expected 2 imported csv rows, got %q", string(bytes.TrimSpace(rowCountOutput)))
	}

	amountOutput, err := sqliteQueryWithRetry(sessionDB, `SELECT SUM(amount) FROM "sales";`)
	if err != nil {
		t.Fatalf("failed to sum imported csv amounts: %v", err)
	}
	if string(bytes.TrimSpace(amountOutput)) != "30" {
		t.Fatalf("expected imported csv amount sum to be 30, got %q", string(bytes.TrimSpace(amountOutput)))
	}

	queryBody := bytes.NewBufferString(`{"question":"Show me the imported sales rows"}`)
	queryReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/query", queryBody)
	queryReq.Header.Set("Content-Type", "application/json")
	queryRec := httptest.NewRecorder()
	handler.ServeHTTP(queryRec, queryReq)
	if queryRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, queryRec.Code)
	}

	var queryResp map[string]any
	if err := json.Unmarshal(queryRec.Body.Bytes(), &queryResp); err != nil {
		t.Fatalf("failed to decode csv query response: %v", err)
	}

	rows, ok := queryResp["rows"].([]any)
	if !ok || len(rows) != 2 {
		t.Fatalf("expected 2 real csv rows in query response, got %v", queryResp["rows"])
	}

	aggBody := bytes.NewBufferString(`{"question":"What is the total sales amount?"}`)
	aggReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/query", aggBody)
	aggReq.Header.Set("Content-Type", "application/json")
	aggRec := httptest.NewRecorder()
	handler.ServeHTTP(aggRec, aggReq)
	if aggRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, aggRec.Code)
	}

	var aggResp map[string]any
	if err := json.Unmarshal(aggRec.Body.Bytes(), &aggResp); err != nil {
		t.Fatalf("failed to decode aggregate query response: %v", err)
	}

	sql, _ := aggResp["sql"].(string)
	if !strings.Contains(strings.ToUpper(sql), "SUM(") {
		t.Fatalf("expected aggregate query sql to contain SUM, got %q", sql)
	}

	aggRows, ok := aggResp["rows"].([]any)
	if !ok || len(aggRows) != 1 {
		t.Fatalf("expected one aggregate row, got %v", aggResp["rows"])
	}

	aggPlan, ok := aggResp["query_plan"].(map[string]any)
	if !ok || aggPlan["mode"] != "aggregate" {
		t.Fatalf("expected aggregate query plan mode, got %v", aggResp["query_plan"])
	}
	aggSummary, _ := aggResp["summary"].(string)
	if !strings.Contains(aggSummary, "aggregate mode") {
		t.Fatalf("expected aggregate summary to mention aggregate mode, got %q", aggSummary)
	}
	if !strings.Contains(aggSummary, "sales.csv") {
		t.Fatalf("expected aggregate summary to include source file, got %q", aggSummary)
	}

	countBody := bytes.NewBufferString(`{"question":"How many sales records are there?"}`)
	countReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/query", countBody)
	countReq.Header.Set("Content-Type", "application/json")
	countRec := httptest.NewRecorder()
	handler.ServeHTTP(countRec, countReq)
	if countRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, countRec.Code)
	}

	var countResp map[string]any
	if err := json.Unmarshal(countRec.Body.Bytes(), &countResp); err != nil {
		t.Fatalf("failed to decode count query response: %v", err)
	}

	countSQL, _ := countResp["sql"].(string)
	if !strings.Contains(strings.ToUpper(countSQL), "COUNT(*)") {
		t.Fatalf("expected count query sql to contain COUNT(*), got %q", countSQL)
	}

	countPlan, ok := countResp["query_plan"].(map[string]any)
	if !ok || countPlan["mode"] != "count" {
		t.Fatalf("expected count query plan mode, got %v", countResp["query_plan"])
	}

	countColumns, ok := countResp["columns"].([]any)
	if !ok || len(countColumns) != 1 || countColumns[0] != "total_count" {
		t.Fatalf("expected count query columns to be [total_count], got %v", countResp["columns"])
	}

	topBody := bytes.NewBufferString(`{"question":"What are the top categories by sales?"}`)
	topReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/query", topBody)
	topReq.Header.Set("Content-Type", "application/json")
	topRec := httptest.NewRecorder()
	handler.ServeHTTP(topRec, topReq)
	if topRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, topRec.Code)
	}

	var topResp map[string]any
	if err := json.Unmarshal(topRec.Body.Bytes(), &topResp); err != nil {
		t.Fatalf("failed to decode top query response: %v", err)
	}

	topSQL, _ := topResp["sql"].(string)
	if !strings.Contains(strings.ToUpper(topSQL), "GROUP BY") {
		t.Fatalf("expected top query sql to contain GROUP BY, got %q", topSQL)
	}

	topPlan, ok := topResp["query_plan"].(map[string]any)
	if !ok || topPlan["mode"] != "topn" {
		t.Fatalf("expected top query plan mode, got %v", topResp["query_plan"])
	}

	topColumns, ok := topResp["columns"].([]any)
	if !ok || len(topColumns) != 2 {
		t.Fatalf("expected 2 ordered columns for top query, got %v", topResp["columns"])
	}

	trendBody := bytes.NewBufferString(`{"question":"Show the sales trend by month"}`)
	trendReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/query", trendBody)
	trendReq.Header.Set("Content-Type", "application/json")
	trendRec := httptest.NewRecorder()
	handler.ServeHTTP(trendRec, trendReq)
	if trendRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, trendRec.Code)
	}

	var trendResp map[string]any
	if err := json.Unmarshal(trendRec.Body.Bytes(), &trendResp); err != nil {
		t.Fatalf("failed to decode trend query response: %v", err)
	}

	trendSQL, _ := trendResp["sql"].(string)
	if !strings.Contains(strings.ToLower(trendSQL), "time_bucket") {
		t.Fatalf("expected trend query sql to include time_bucket, got %q", trendSQL)
	}

	trendPlan, ok := trendResp["query_plan"].(map[string]any)
	if !ok || trendPlan["mode"] != "trend" {
		t.Fatalf("expected trend query plan mode, got %v", trendResp["query_plan"])
	}

	trendColumns, ok := trendResp["columns"].([]any)
	if !ok || len(trendColumns) != 2 {
		t.Fatalf("expected 2 ordered columns for trend query, got %v", trendResp["columns"])
	}

	dbReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID+"/database", nil)
	dbRec := httptest.NewRecorder()
	handler.ServeHTTP(dbRec, dbReq)
	if dbRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, dbRec.Code)
	}

	var dbResp map[string]any
	if err := json.Unmarshal(dbRec.Body.Bytes(), &dbResp); err != nil {
		t.Fatalf("failed to decode database response: %v", err)
	}

	catalog, ok := dbResp["catalog"].([]any)
	if !ok || len(catalog) == 0 {
		t.Fatalf("expected catalog in database response")
	}

	firstCatalog, ok := catalog[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first catalog entry to be an object")
	}
	previewRows, ok := firstCatalog["preview_rows"].([]any)
	if !ok || len(previewRows) != 2 {
		t.Fatalf("expected 2 preview rows for imported csv table, got %v", firstCatalog["preview_rows"])
	}
	firstPreviewRow, ok := previewRows[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first preview row to be an object")
	}
	if firstPreviewRow["category"] != "A" {
		t.Fatalf("expected first preview row category to be A, got %v", firstPreviewRow["category"])
	}
	if dbResp["table_count"] != float64(1) {
		t.Fatalf("expected table_count to be 1, got %v", dbResp["table_count"])
	}
	if dbResp["total_row_count"] != float64(2) {
		t.Fatalf("expected total_row_count to be 2, got %v", dbResp["total_row_count"])
	}
	if dbResp["import_task_count"] != float64(1) {
		t.Fatalf("expected import_task_count to be 1, got %v", dbResp["import_task_count"])
	}

	sessionReq := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID, nil)
	sessionRec := httptest.NewRecorder()
	handler.ServeHTTP(sessionRec, sessionReq)
	if sessionRec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, sessionRec.Code)
	}

	var sessionResp map[string]any
	if err := json.Unmarshal(sessionRec.Body.Bytes(), &sessionResp); err != nil {
		t.Fatalf("failed to decode session response: %v", err)
	}

	if sessionResp["uploaded_file_count"] != float64(1) {
		t.Fatalf("expected uploaded_file_count to be 1, got %v", sessionResp["uploaded_file_count"])
	}
	if sessionResp["table_count"] != float64(1) {
		t.Fatalf("expected session table_count to be 1, got %v", sessionResp["table_count"])
	}
	if sessionResp["import_task_count"] != float64(1) {
		t.Fatalf("expected session import_task_count to be 1, got %v", sessionResp["import_task_count"])
	}
	if sessionResp["total_row_count"] != float64(2) {
		t.Fatalf("expected session total_row_count to be 2, got %v", sessionResp["total_row_count"])
	}
}

func TestXLSXUploadImportsRowsIntoSQLite(t *testing.T) {
	dataDir := t.TempDir()
	handler := NewHandler(dataDir)

	createReq := httptest.NewRequest(http.MethodPost, "/api/sessions", nil)
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, createRec.Code)
	}

	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	sessionID, _ := created["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("expected session_id in create response")
	}

	workbook := excelize.NewFile()
	workbook.SetCellValue("Sheet1", "A1", "order_date")
	workbook.SetCellValue("Sheet1", "B1", "category")
	workbook.SetCellValue("Sheet1", "C1", "amount")
	workbook.SetCellValue("Sheet1", "A2", "2025-01-01")
	workbook.SetCellValue("Sheet1", "B2", "A")
	workbook.SetCellValue("Sheet1", "C2", 10)
	workbook.SetCellValue("Sheet1", "A3", "2025-01-02")
	workbook.SetCellValue("Sheet1", "B3", "B")
	workbook.SetCellValue("Sheet1", "C3", 20)

	var xlsx bytes.Buffer
	if err := workbook.Write(&xlsx); err != nil {
		t.Fatalf("failed to write xlsx workbook: %v", err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "sales.xlsx")
	if err != nil {
		t.Fatalf("failed to create form file: %v", err)
	}
	if _, err := part.Write(xlsx.Bytes()); err != nil {
		t.Fatalf("failed to write xlsx bytes: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	uploadReq := httptest.NewRequest(http.MethodPost, "/api/sessions/"+sessionID+"/files/upload", &body)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	handler.ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, uploadRec.Code)
	}

	var uploadResp map[string]any
	if err := json.Unmarshal(uploadRec.Body.Bytes(), &uploadResp); err != nil {
		t.Fatalf("failed to decode upload response: %v", err)
	}
	taskID, _ := uploadResp["task_id"].(string)
	if taskID == "" {
		t.Fatalf("expected task_id in upload response")
	}

	waitForImportTaskStatus(t, handler, sessionID, taskID, "completed")

	sessionDB := filepath.Join(dataDir, "sessions", sessionID, "session.db")
	rowCountOutput, err := sqliteQueryWithRetry(sessionDB, `SELECT COUNT(*) FROM "sales";`)
	if err != nil {
		t.Fatalf("failed to count imported xlsx rows: %v", err)
	}
	if string(bytes.TrimSpace(rowCountOutput)) != "2" {
		t.Fatalf("expected 2 imported xlsx rows, got %q", string(bytes.TrimSpace(rowCountOutput)))
	}
}

func sqliteQueryWithRetry(databasePath, sql string) ([]byte, error) {
	var lastErr error
	for i := 0; i < 5; i++ {
		output, err := exec.Command(
			"sqlite3",
			"-cmd", ".timeout 2000",
			databasePath,
			sql,
		).Output()
		if err == nil {
			return output, nil
		}
		lastErr = err
		time.Sleep(25 * time.Millisecond)
	}
	return nil, fmt.Errorf("sqlite query failed after retries: %w", lastErr)
}

func waitForImportTaskStatus(t *testing.T, handler http.Handler, sessionID, taskID, wantStatus string) {
	t.Helper()

	var lastStatus string
	for i := 0; i < 60; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionID+"/imports/"+taskID, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code == http.StatusInternalServerError {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
		}

		var importResp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &importResp); err != nil {
			t.Fatalf("failed to decode import response: %v", err)
		}

		lastStatus, _ = importResp["status"].(string)
		if lastStatus == wantStatus {
			return
		}

		if lastStatus == "failed" {
			t.Fatalf("import task failed: %v", importResp["error"])
		}

		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("import task did not reach %q in time, last status %q", wantStatus, lastStatus)
}

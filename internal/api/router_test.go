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

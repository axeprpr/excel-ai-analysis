package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
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
		"sqlite3",
		sessionDB,
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

	var importRec *httptest.ResponseRecorder
	for i := 0; i < 20; i++ {
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

		status, _ := importResp["status"].(string)
		if status == "completed" {
			importRec = rec
			break
		}

		time.Sleep(10 * time.Millisecond)
	}

	if importRec == nil {
		t.Fatalf("import task did not complete in time")
	}

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

	sessionDB := filepath.Join(dataDir, "sessions", sessionID, "session.db")
	statusOutput, err := exec.Command(
		"sqlite3",
		sessionDB,
		"SELECT value FROM session_meta WHERE key='status';",
	).Output()
	if err != nil {
		t.Fatalf("failed to read session status from sqlite: %v", err)
	}
	if string(bytes.TrimSpace(statusOutput)) != "ready" {
		t.Fatalf("expected sqlite session status to be ready, got %q", string(bytes.TrimSpace(statusOutput)))
	}

	tablesOutput, err := exec.Command(
		"sqlite3",
		sessionDB,
		"SELECT value FROM session_meta WHERE key='tables';",
	).Output()
	if err != nil {
		t.Fatalf("failed to read session tables from sqlite: %v", err)
	}
	if string(bytes.TrimSpace(tablesOutput)) == "" {
		t.Fatalf("expected sqlite session tables to be populated")
	}

	taskStatusOutput, err := exec.Command(
		"sqlite3",
		sessionDB,
		"SELECT status FROM import_tasks WHERE task_id="+sqliteQuote(taskID)+";",
	).Output()
	if err != nil {
		t.Fatalf("failed to read import task status from sqlite: %v", err)
	}
	if string(bytes.TrimSpace(taskStatusOutput)) != "completed" {
		t.Fatalf("expected sqlite import task status to be completed, got %q", string(bytes.TrimSpace(taskStatusOutput)))
	}

	taskFilesOutput, err := exec.Command(
		"sqlite3",
		sessionDB,
		"SELECT file_names FROM import_tasks WHERE task_id="+sqliteQuote(taskID)+";",
	).Output()
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

	for i := 0; i < 20; i++ {
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

		if status, _ := importResp["status"].(string); status == "completed" {
			break
		}

		if i == 19 {
			t.Fatalf("import task did not complete in time")
		}
		time.Sleep(10 * time.Millisecond)
	}

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

	for i := 0; i < 20; i++ {
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

		if status, _ := importResp["status"].(string); status == "completed" {
			break
		}

		if i == 19 {
			t.Fatalf("import task did not complete in time")
		}
		time.Sleep(10 * time.Millisecond)
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

	sqliteTables, ok := dbResp["sqlite_tables"].([]any)
	if !ok || len(sqliteTables) == 0 {
		t.Fatalf("expected sqlite_tables in database response")
	}

	hasSessionMeta := false
	hasImportTasks := false
	for _, table := range sqliteTables {
		name, _ := table.(string)
		if name == "session_meta" {
			hasSessionMeta = true
		}
		if name == "import_tasks" {
			hasImportTasks = true
		}
	}

	if !hasSessionMeta || !hasImportTasks {
		t.Fatalf("expected sqlite tables to include session_meta and import_tasks, got %v", sqliteTables)
	}
}

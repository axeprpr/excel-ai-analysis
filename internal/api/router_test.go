package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
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

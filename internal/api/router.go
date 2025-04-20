package api

import (
	"net/http"
	"strings"
)

type Handler struct {
	dataDir string
}

func NewHandler(dataDir string) http.Handler {
	return &Handler{dataDir: dataDir}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/sessions":
		h.handleSessionsRoot(w, r)
		return
	case strings.HasSuffix(r.URL.Path, "/imports") && strings.HasPrefix(r.URL.Path, "/api/sessions/"):
		h.handleImports(w, r)
		return
	case strings.HasSuffix(r.URL.Path, "/schema") && strings.HasPrefix(r.URL.Path, "/api/sessions/"):
		h.handleSessionSchema(w, r)
		return
	case strings.HasSuffix(r.URL.Path, "/query") && strings.HasPrefix(r.URL.Path, "/api/sessions/"):
		h.handleSessionQuery(w, r)
		return
	case strings.Contains(r.URL.Path, "/imports/") && strings.HasPrefix(r.URL.Path, "/api/sessions/"):
		h.handleImportByID(w, r)
		return
	case strings.HasSuffix(r.URL.Path, "/files/upload") && strings.HasPrefix(r.URL.Path, "/api/sessions/"):
		h.handleSessionUpload(w, r)
		return
	case len(r.URL.Path) > len("/api/sessions/") && r.URL.Path[:len("/api/sessions/")] == "/api/sessions/":
		h.handleSessionByID(w, r)
		return
	default:
		http.NotFound(w, r)
	}
}

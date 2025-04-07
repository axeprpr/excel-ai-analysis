package api

import "net/http"

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
	case len(r.URL.Path) > len("/api/sessions/") && r.URL.Path[:len("/api/sessions/")] == "/api/sessions/":
		h.handleSessionByID(w, r)
		return
	default:
		http.NotFound(w, r)
	}
}

// Package server exposes the second brain repository over HTTP: nodes, search
// and the graph. It is meant to be reached over the tailnet only (or the MCP
// server), so it carries no authentication of its own.
package server

import (
	"encoding/json"
	"log"
	"net/http"

	"desrosiers.org/brain/internal/pwa"
	"desrosiers.org/brain/internal/store"
)

// Server is the HTTP API frontend over a store.
type Server struct {
	store *store.Store
	log   *log.Logger
}

// New returns a Server backed by the given store.
func New(s *store.Store) *Server {
	return &Server{store: s, log: log.Default()}
}

// Handler wires the API routes (Go 1.22+ pattern routing).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/nodes", s.handleListNodes)
	mux.HandleFunc("GET /api/nodes/{id}", s.handleGetNode)
	mux.HandleFunc("PUT /api/nodes/{id}", s.handlePutNode)
	mux.HandleFunc("DELETE /api/nodes/{id}", s.handleDeleteNode)
	mux.HandleFunc("GET /api/search", s.handleSearch)
	mux.HandleFunc("GET /api/graph/neighbors/{id}", s.handleNeighbors)
	mux.HandleFunc("GET /api/graph/related/{id}", s.handleRelated)
	mux.HandleFunc("GET /api/graph/path/{from}/{to}", s.handlePath)
	mux.HandleFunc("GET /api/graph/resolve", s.handleResolve)
	mux.Handle("GET /", http.FileServerFS(pwa.FS()))
	return s.logMiddleware(mux)
}

func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

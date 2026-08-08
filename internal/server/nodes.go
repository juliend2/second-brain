package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"desrosiers.org/brain/internal/store"
)

// nodeJSON is the wire representation of a node.
type nodeJSON struct {
	ID        string            `json:"id"`
	Source    string            `json:"source"`
	Title     string            `json:"title"`
	Markdown  string            `json:"markdown,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
	Summary   string            `json:"summary,omitempty"`
	Status    string            `json:"status"`
	Meta      map[string]string `json:"meta,omitempty"`
	CreatedAt string            `json:"created_at"`
	UpdatedAt string            `json:"updated_at"`
}

func toNodeJSON(n *store.Node, includeMarkdown bool) nodeJSON {
	j := nodeJSON{
		ID:        n.ID,
		Source:    n.Source,
		Title:     n.Title,
		Tags:      n.Tags,
		Summary:   n.Summary,
		Status:    n.Status,
		Meta:      n.Meta,
		CreatedAt: n.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: n.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if includeMarkdown {
		j.Markdown = n.Markdown
	}
	return j
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.store.ListNodes()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	source := r.URL.Query().Get("source")
	status := r.URL.Query().Get("status")
	filtered := nodes[:0:0]
	for _, n := range nodes {
		if source != "" && n.Source != source {
			continue
		}
		if status != "" && n.Status != status {
			continue
		}
		filtered = append(filtered, n)
	}
	nodes = filtered

	limit, offset := intParam(r, "limit", 100, 500), intParam(r, "offset", 0, -1)
	total := len(nodes)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}

	items := make([]nodeJSON, 0, end-offset)
	for _, n := range nodes[offset:end] {
		items = append(items, toNodeJSON(n, false))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": total,
		"items": items,
	})
}

func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.GetNode(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toNodeJSON(n, true))
}

func (s *Server) handlePutNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in struct {
		Source   string            `json:"source"`
		Title    string            `json:"title"`
		Markdown string            `json:"markdown"`
		Tags     []string          `json:"tags"`
		Summary  string            `json:"summary"`
		Status   string            `json:"status"`
		Meta     map[string]string `json:"meta"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if in.Source == "" {
		in.Source = "local"
	}
	if in.Title == "" {
		in.Title = id
	}
	if in.Status == "" {
		in.Status = store.StatusDraft
	}

	n := &store.Node{
		ID:       id,
		Source:   in.Source,
		Title:    in.Title,
		Markdown: in.Markdown,
		Tags:     in.Tags,
		Summary:  in.Summary,
		Status:   in.Status,
		Meta:     in.Meta,
	}
	if err := s.store.PutNode(n); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// Deterministic local operation: wikilinks written into the markdown
	// immediately become graph edges. (LLM-level linking happens in enrich.)
	if in.Markdown != "" {
		if _, err := s.store.ExtractEdgesFromMarkdown(id, in.Markdown); err != nil {
			s.log.Printf("extract edges for %s: %v", id, err)
		}
	}
	got, err := s.store.GetNode(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toNodeJSON(got, true))
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	err := s.store.DeleteNode(r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// intParam reads an integer query parameter with default and optional max.
func intParam(r *http.Request, key string, def, max int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return def
	}
	if max > 0 && v > max {
		return max
	}
	return v
}

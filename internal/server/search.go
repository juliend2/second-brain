package server

import (
	"net/http"
)

// searchHitJSON is the wire representation of a search result.
type searchHitJSON struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeErr(w, http.StatusBadRequest, "missing q parameter")
		return
	}
	limit := intParam(r, "limit", 20, 100)

	hits, err := s.store.Search(q, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]searchHitJSON, 0, len(hits))
	for _, h := range hits {
		items = append(items, searchHitJSON{
			ID: h.ID, Source: h.Source, Title: h.Title,
			Summary: h.Summary, Snippet: h.Snippet,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

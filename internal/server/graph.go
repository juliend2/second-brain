package server

import (
	"errors"
	"net/http"

	"desrosiers.org/brain/internal/store"
)

// edgeJSON is one edge with the target node's title resolved for display.
type edgeJSON struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Kind   string `json:"kind"`
	Reason string `json:"reason,omitempty"`
	Title  string `json:"title,omitempty"`
}

func (s *Server) handleNeighbors(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	edges, err := s.store.Neighbors(id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	out := make([]edgeJSON, 0, len(edges))
	for _, e := range edges {
		ej := edgeJSON{From: e.From, To: e.To, Kind: e.Kind, Reason: e.Reason}
		other := e.From
		if other == id {
			other = e.To
		}
		if n, err := s.store.GetNode(other); err == nil {
			ej.Title = n.Title
		}
		out = append(out, ej)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":    id,
		"edges": out,
	})
}

// relatedJSON is a nearest-neighbor with display info.
type relatedJSON struct {
	ID    string  `json:"id"`
	Title string  `json:"title"`
	Score float64 `json:"score"`
}

func (s *Server) handleRelated(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	k := intParam(r, "k", 5, 20)

	sim, err := s.store.NearestNeighbors(id, k)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "node not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := make([]relatedJSON, 0, len(sim))
	for _, s2 := range sim {
		rj := relatedJSON{ID: s2.ID, Score: s2.Score}
		if n, err := s.store.GetNode(s2.ID); err == nil {
			rj.Title = n.Title
		}
		items = append(items, rj)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":    id,
		"items": items,
	})
}

func (s *Server) handlePath(w http.ResponseWriter, r *http.Request) {
	from, to := r.PathValue("from"), r.PathValue("to")
	path, err := s.store.ShortestPath(from, to)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if path == nil {
		writeJSON(w, http.StatusOK, map[string]any{"path": []string{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": path})
}

package store

import (
	"strings"
	"testing"
)

func TestSearch(t *testing.T) {
	s := newTestStore(t)
	nodes := []*Node{
		mustNode("notion:a", "Apple Pie", "# Apple Pie\n\nrecipe for apple pie with cinnamon"),
		mustNode("notion:o", "Orange Juice", "# Orange Juice\n\ncitrus drink for breakfast"),
	}
	for _, n := range nodes {
		if err := s.PutNode(n); err != nil {
			t.Fatalf("PutNode: %v", err)
		}
	}

	hits, err := s.Search("apple", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("got %d hits, want 1: %+v", len(hits), hits)
	}
	if hits[0].ID != "notion:a" || hits[0].Title != "Apple Pie" {
		t.Errorf("hit = %+v", hits[0])
	}
	if !strings.Contains(hits[0].Snippet, "[apple]") {
		t.Errorf("snippet = %q, want highlighted match", hits[0].Snippet)
	}

	// Title is searchable too.
	hits, err = s.Search("orange", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "notion:o" {
		t.Errorf("hits = %+v", hits)
	}

	// Empty query returns nothing.
	if hits, err := s.Search("  ", 10); err != nil || hits != nil {
		t.Errorf("empty query: hits=%v err=%v", hits, err)
	}
}

func TestSearchTracksNodeLifecycle(t *testing.T) {
	s := newTestStore(t)
	if err := s.PutNode(mustNode("notion:x", "Quantum", "quantum mechanics notes")); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	hits, _ := s.Search("quantum", 10)
	if len(hits) != 1 {
		t.Fatalf("quantum: got %d hits", len(hits))
	}

	// Title change propagates to the index (metadata-only update).
	if err := s.PutNode(&Node{ID: "notion:x", Source: "notion", Title: "Quantum Physics"}); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	hits, _ = s.Search("physics", 10)
	if len(hits) != 1 {
		t.Errorf("physics: got %d hits, want 1", len(hits))
	}
	hits, _ = s.Search("quantum", 10)
	if len(hits) != 1 {
		t.Errorf("quantum (content) should still hit, got %d", len(hits))
	}

	// Delete removes from the index.
	if err := s.DeleteNode("notion:x"); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}
	hits, _ = s.Search("quantum", 10)
	if len(hits) != 0 {
		t.Errorf("after delete: got %d hits, want 0", len(hits))
	}
}

func TestRebuildIndex(t *testing.T) {
	s := newTestStore(t)
	if err := s.PutNode(mustNode("notion:r", "Recipes", "secret recipe of the year")); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	// Simulate a wiped or out-of-sync index.
	if _, err := s.db.Exec(`DELETE FROM node_fts`); err != nil {
		t.Fatalf("wipe: %v", err)
	}
	if err := s.RebuildIndex(); err != nil {
		t.Fatalf("RebuildIndex: %v", err)
	}
	hits, err := s.Search("recipe", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].ID != "notion:r" {
		t.Errorf("hits = %+v", hits)
	}
}

func TestShortestPath(t *testing.T) {
	s := newTestStore(t)
	for _, id := range []string{"notion:a", "notion:b", "notion:c", "notion:z"} {
		if err := s.PutNode(mustNode(id, id, "# "+id)); err != nil {
			t.Fatalf("PutNode: %v", err)
		}
	}
	s.AddEdge("notion:a", "notion:b", EdgeWikilink, "")
	s.AddEdge("notion:b", "notion:c", EdgeWikilink, "")

	path, err := s.ShortestPath("notion:a", "notion:c")
	if err != nil {
		t.Fatalf("ShortestPath: %v", err)
	}
	want := []string{"notion:a", "notion:b", "notion:c"}
	if len(path) != len(want) {
		t.Fatalf("path = %v, want %v", path, want)
	}
	for i := range want {
		if path[i] != want[i] {
			t.Errorf("path = %v, want %v", path, want)
		}
	}

	// Disconnected pair returns nil.
	path, err = s.ShortestPath("notion:a", "notion:z")
	if err != nil {
		t.Fatalf("ShortestPath: %v", err)
	}
	if path != nil {
		t.Errorf("disconnected path = %v, want nil", path)
	}
}

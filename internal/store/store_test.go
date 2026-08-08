package store

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "brain.db"), filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustNode(id, title, md string) *Node {
	return &Node{ID: id, Source: "nt", Title: title, Markdown: md, CreatedAt: time.Now().UTC()}
}

func TestPutGetNode(t *testing.T) {
	s := newTestStore(t)
	n := mustNode("notion:abc123", "Hello Brain", "# Hello Brain\n\nsome **content**")
	if err := s.PutNode(n); err != nil {
		t.Fatalf("PutNode: %v", err)
	}

	got, err := s.GetNode("notion:abc123")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Title != "Hello Brain" {
		t.Errorf("title = %q, want %q", got.Title, "Hello Brain")
	}
	if got.Markdown != n.Markdown {
		t.Errorf("markdown mismatch:\n got %q\nwant %q", got.Markdown, n.Markdown)
	}
	if got.Status != StatusDraft {
		t.Errorf("status = %q, want %q", got.Status, StatusDraft)
	}
	if got.RelPath != "notion/abc123.md" {
		t.Errorf("relPath = %q, want %q", got.RelPath, "notion/abc123.md")
	}

	if _, err := os.Stat(filepath.Join(s.NotesDir(), "notion/abc123.md")); err != nil {
		t.Errorf("mirror file missing: %v", err)
	}
}

func TestGetNodeNotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.GetNode("notion:nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestPutNodeUpsertPreservesContent(t *testing.T) {
	s := newTestStore(t)
	if err := s.PutNode(mustNode("notion:x", "T", "original content")); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	// Metadata-only update with empty markdown must not clobber the file.
	if err := s.PutNode(&Node{ID: "notion:x", Source: "notion", Title: "T2", Status: StatusEnriched}); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	got, err := s.GetNode("notion:x")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Markdown != "original content" {
		t.Errorf("markdown = %q, want preserved content", got.Markdown)
	}
	if got.Title != "T2" || got.Status != StatusEnriched {
		t.Errorf("metadata not updated: title=%q status=%q", got.Title, got.Status)
	}
}

func TestDropboxPathNode(t *testing.T) {
	s := newTestStore(t)
	n := &Node{ID: "dropbox:Documents/notes/foo.md", Source: "dropbox", Title: "foo", Markdown: "# foo"}
	if err := s.PutNode(n); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	got, err := s.GetNode("dropbox:Documents/notes/foo.md")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.RelPath != "dropbox/Documents/notes/foo.md" {
		t.Errorf("relPath = %q", got.RelPath)
	}
}

func TestDeleteNode(t *testing.T) {
	s := newTestStore(t)
	if err := s.PutNode(mustNode("notion:d", "D", "# D")); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	if err := s.DeleteNode("notion:d"); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}
	if _, err := s.GetNode("notion:d"); !errors.Is(err, ErrNotFound) {
		t.Errorf("node still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.NotesDir(), "notion/d.md")); !os.IsNotExist(err) {
		t.Errorf("mirror file still present")
	}
}

func TestScan(t *testing.T) {
	s := newTestStore(t)
	dir := filepath.Join(s.NotesDir(), "notion")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scanned.md"), []byte("# scanned"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := s.Scan(); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	got, err := s.GetNode("notion:scanned")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Markdown != "# scanned" || got.Title != "scanned" {
		t.Errorf("got markdown=%q title=%q", got.Markdown, got.Title)
	}
	// Second scan must be idempotent.
	if err := s.Scan(); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	nodes, err := s.ListNodes()
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if len(nodes) != 1 {
		t.Errorf("ListNodes = %d nodes, want 1", len(nodes))
	}
}

func TestParseWikilinks(t *testing.T) {
	md := "See [[alpha]] and [[beta|the beta doc]] and [[gamma ]]."
	links := ParseWikilinks(md)
	if len(links) != 3 {
		t.Fatalf("got %d links, want 3: %+v", len(links), links)
	}
	if links[0].Target != "alpha" || links[1].Target != "beta" || links[1].Display != "the beta doc" {
		t.Errorf("unexpected links: %+v", links)
	}
	if links[2].Target != "gamma" {
		t.Errorf("target = %q, want %q (trimmed)", links[2].Target, "gamma")
	}
}

func TestExtractEdgesFromMarkdown(t *testing.T) {
	s := newTestStore(t)
	a := mustNode("notion:a", "Alpha", "Connects to [[Beta]] and [[missing]] and [[Alpha]].")
	b := mustNode("notion:b", "Beta", "the beta note")
	if err := s.PutNode(a); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	if err := s.PutNode(b); err != nil {
		t.Fatalf("PutNode: %v", err)
	}

	edges, err := s.ExtractEdgesFromMarkdown("notion:a", a.Markdown)
	if err != nil {
		t.Fatalf("ExtractEdges: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1 (missing unresolved, self skipped): %+v", len(edges), edges)
	}
	if edges[0].To != "notion:b" {
		t.Errorf("edge To = %q, want %q", edges[0].To, "notion:b")
	}

	neighbors, err := s.Neighbors("notion:b")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if len(neighbors) != 1 || neighbors[0].From != "notion:a" {
		t.Errorf("neighbors = %+v, want incoming edge from notion:a", neighbors)
	}

	// Duplicate extraction is idempotent.
	if _, err := s.ExtractEdgesFromMarkdown("notion:a", a.Markdown); err != nil {
		t.Fatalf("ExtractEdges: %v", err)
	}
	neighbors, _ = s.Neighbors("notion:b")
	if len(neighbors) != 1 {
		t.Errorf("duplicate edges created: %+v", neighbors)
	}
}

func TestAddEdgeValidations(t *testing.T) {
	s := newTestStore(t)
	if err := s.PutNode(mustNode("notion:a", "Alpha", "x")); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	if err := s.AddEdge("notion:a", "notion:a", EdgeWikilink, ""); err == nil {
		t.Errorf("self-link should fail")
	}
	if err := s.AddEdge("notion:a", "notion:ghost", EdgeWikilink, ""); err == nil {
		t.Errorf("edge to missing node should fail")
	}
	if err := s.AddEdge("notion:a", "notion:b", EdgeWikilink, ""); err == nil {
		t.Errorf("edge between existing source and missing target should fail")
	}
}

func TestConnectsEdgeWithReason(t *testing.T) {
	s := newTestStore(t)
	if err := s.PutNode(mustNode("notion:a", "A", "x")); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	if err := s.PutNode(mustNode("notion:b", "B", "y")); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	reason := "both cover X, from different angles"
	if err := s.AddEdge("notion:a", "notion:b", EdgeConnects, reason); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}
	ns, err := s.Neighbors("notion:a")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	if len(ns) != 1 || ns[0].Reason != reason || ns[0].Kind != EdgeConnects {
		t.Errorf("neighbors = %+v", ns)
	}
}

func TestEmbeddingsAndNeighbors(t *testing.T) {
	s := newTestStore(t)
	for _, title := range []string{"apple", "banana", "car", "fruit"} {
		n := &Node{ID: "notion:" + title, Source: "notion", Title: title, Markdown: "# " + title}
		if err := s.PutNode(n); err != nil {
			t.Fatalf("PutNode: %v", err)
		}
	}
	// "apple" and "banana" are fruit; "car" is not.
	s.SetEmbedding("notion:apple", []float32{1, 0, 0, 1})
	s.SetEmbedding("notion:banana", []float32{1, 0, 0, 1})
	s.SetEmbedding("notion:car", []float32{0, 1, 0, 0})
	s.SetEmbedding("notion:fruit", []float32{1, 0, 0, 1})

	sim, err := s.NearestNeighbors("notion:apple", 2)
	if err != nil {
		t.Fatalf("NearestNeighbors: %v", err)
	}
	if len(sim) != 2 {
		t.Fatalf("got %d neighbors, want 2: %+v", len(sim), sim)
	}
	// banana and fruit must both rank above car.
	if sim[0].ID == "notion:car" || sim[1].ID == "notion:car" {
		t.Errorf("car should not be in top-2: %+v", sim)
	}
	for _, r := range sim {
		if r.ID != "notion:banana" && r.ID != "notion:fruit" {
			t.Errorf("unexpected neighbor %q: %+v", r.ID, sim)
		}
		if math.Abs(r.Score-1.0) > 1e-9 {
			t.Errorf("neighbor %q score = %v, want ~1.0", r.ID, r.Score)
		}
	}

	if !s.HasEmbedding("notion:car") || s.HasEmbedding("notion:missing") {
		t.Errorf("HasEmbedding misbehaving")
	}
}

func TestMirrorPathFor(t *testing.T) {
	cases := []struct {
		id  string
		rel string
		err bool
	}{
		{"notion:abc123", "notion/abc123.md", false},
		{"dropbox:Documents/foo.md", "dropbox/Documents/foo.md", false},
		{"dropbox:Documents/foo", "dropbox/Documents/foo.md", false},
		{"nt:../escape", "", true},
		{"/abs/path", "", true},
		{"noseparator", "", true},
	}
	for _, c := range cases {
		rel, err := mirrorPathFor(c.id)
		if c.err {
			if err == nil {
				t.Errorf("mirrorPathFor(%q) = %q, want error", c.id, rel)
			}
			continue
		}
		if err != nil {
			t.Errorf("mirrorPathFor(%q): %v", c.id, err)
			continue
		}
		if rel != c.rel {
			t.Errorf("mirrorPathFor(%q) = %q, want %q", c.id, rel, c.rel)
		}
	}
}

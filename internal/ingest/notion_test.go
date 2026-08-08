package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"desrosiers.org/brain/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "brain.db"), filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

type blockRef struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

// fakeNotion is an in-memory stand-in for the Notion API.
type fakeNotion struct {
	mu         sync.Mutex
	pages      map[string]notionPage
	children   map[string][]blockRef
	markdowns  map[string]string
	mdRequests int
}

func fakePage(id, title, edited string, archived bool) notionPage {
	p := notionPage{
		ID:             id,
		URL:            "https://www.notion.so/" + id,
		LastEditedTime: edited,
		Archived:       archived,
	}
	p.Properties.Title.Title = []titleItem{{PlainText: title}}
	return p
}

func (f *fakeNotion) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		p := r.URL.Path
		switch {
		case strings.HasSuffix(p, "/markdown"):
			pageID := strings.TrimPrefix(strings.TrimSuffix(p, "/markdown"), "/v1/pages/")
			f.mdRequests++
			json.NewEncoder(w).Encode(map[string]string{"markdown": f.markdowns[pageID]})
		case strings.HasPrefix(p, "/v1/blocks/") && strings.HasSuffix(p, "/children"):
			parentID := strings.TrimPrefix(strings.TrimSuffix(p, "/children"), "/v1/blocks/")
			json.NewEncoder(w).Encode(map[string]any{
				"results":  f.children[parentID],
				"has_more": false,
			})
		case strings.HasPrefix(p, "/v1/pages/"):
			pageID := strings.TrimPrefix(p, "/v1/pages/")
			json.NewEncoder(w).Encode(f.pages[pageID])
		default:
			http.NotFound(w, r)
		}
	})
}

func newFakeNotion() *fakeNotion {
	return &fakeNotion{
		pages: map[string]notionPage{
			"root": fakePage("root", "Root Page", "t0", false),
			"p1":   fakePage("p1", "Child One", "t0", false),
			"p2":   fakePage("p2", "Archived", "t0", true),
		},
		children: map[string][]blockRef{
			"root": {{ID: "p1", Type: "child_page"}, {ID: "p2", Type: "child_page"}, {ID: "blk", Type: "paragraph"}},
			"p1":   {},
		},
		markdowns: map[string]string{"root": "root body", "p1": "child body"},
	}
}

func newNotionFor(t *testing.T, f *fakeNotion, s *store.Store) *Notion {
	t.Helper()
	server := httptest.NewServer(f.handler())
	t.Cleanup(server.Close)
	n := NewNotion(s, "secret", "root")
	n.SetInterval(0)
	n.UseBaseURL(server.URL)
	return n
}

func TestNotionSync(t *testing.T) {
	s := newTestStore(t)
	f := newFakeNotion()
	n := newNotionFor(t, f, s)

	stats, err := n.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if stats.New != 2 || stats.Skipped != 1 {
		t.Errorf("stats = %s, want 2 new, 1 skipped", stats)
	}

	root, err := s.GetNode("notion:root")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if root.Title != "Root Page" || root.Markdown != "# Root Page\n\nroot body" {
		t.Errorf("root = %q / %q", root.Title, root.Markdown)
	}
	if root.Meta["last_edited_time"] != "t0" || root.Meta["url"] == "" {
		t.Errorf("root meta = %v", root.Meta)
	}

	if _, err := s.GetNode("notion:p2"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("archived page should not be stored")
	}
}

func TestNotionSyncSkipsUnchanged(t *testing.T) {
	s := newTestStore(t)
	f := newFakeNotion()
	n := newNotionFor(t, f, s)

	if _, err := n.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	f.mu.Lock()
	before := f.mdRequests
	f.mu.Unlock()

	stats, err := n.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if stats.New != 0 || stats.Updated != 0 || stats.Skipped != 3 {
		t.Errorf("stats = %s, want 0 new, 0 updated, 3 skipped", stats)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mdRequests != before {
		t.Errorf("markdown fetched on unchanged sync (%d -> %d)", before, f.mdRequests)
	}
}

func TestNotionSyncUpdatesChangedPage(t *testing.T) {
	s := newTestStore(t)
	f := newFakeNotion()
	n := newNotionFor(t, f, s)

	if _, err := n.Sync(context.Background()); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	f.mu.Lock()
	f.pages["p1"] = fakePage("p1", "Child One", "t1", false)
	f.markdowns["p1"] = "child body v2"
	f.mu.Unlock()

	stats, err := n.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if stats.Updated != 1 {
		t.Errorf("stats = %s, want 1 updated", stats)
	}

	child, err := s.GetNode("notion:p1")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if child.Markdown != "# Child One\n\nchild body v2" {
		t.Errorf("markdown = %q", child.Markdown)
	}
	if child.Meta["last_edited_time"] != "t1" {
		t.Errorf("meta = %v", child.Meta)
	}
}

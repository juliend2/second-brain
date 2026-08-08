package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
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

func newTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	s := newTestStore(t)
	ts := httptest.NewServer(New(s).Handler())
	t.Cleanup(ts.Close)
	return ts, s
}

func getJSON(t *testing.T, ts *httptest.Server, path string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("GET %s: bad json %q: %v", path, body, err)
	}
	return resp.StatusCode, m
}

func seed(t *testing.T, s *store.Store) {
	t.Helper()
	for _, n := range []*store.Node{
		{ID: "notion:alpha", Source: "notion", Title: "Alpha Notes", Markdown: "# Alpha Notes\n\nabout the alpha project and apple pie"},
		{ID: "notion:beta", Source: "notion", Title: "Beta Notes", Markdown: "# Beta Notes\n\nabout the beta project, links to [[Alpha Notes]]"},
		{ID: "dropbox:recipes/pie.md", Source: "dropbox", Title: "Apple Pie", Markdown: "# Apple Pie\n\na dessert recipe"},
	} {
		if err := s.PutNode(n); err != nil {
			t.Fatalf("PutNode: %v", err)
		}
	}
	s.AddEdge("notion:beta", "notion:alpha", store.EdgeWikilink, "")
	s.SetEmbedding("notion:alpha", []float32{1, 0})
	s.SetEmbedding("notion:beta", []float32{1, 0})
	s.SetEmbedding("dropbox:recipes/pie.md", []float32{0, 1})
}

func TestHealth(t *testing.T) {
	ts, _ := newTestServer(t)
	code, m := getJSON(t, ts, "/api/health")
	if code != http.StatusOK || m["status"] != "ok" {
		t.Errorf("code=%d body=%v", code, m)
	}
}

func TestListNodes(t *testing.T) {
	ts, s := newTestServer(t)
	seed(t, s)

	code, m := getJSON(t, ts, "/api/nodes")
	if code != http.StatusOK {
		t.Fatalf("code=%d", code)
	}
	if m["total"].(float64) != 3 {
		t.Errorf("total = %v, want 3", m["total"])
	}
	items := m["items"].([]any)
	first := items[0].(map[string]any)
	if _, ok := first["markdown"]; ok {
		t.Errorf("list must not include markdown")
	}

	// Source filter.
	code, m = getJSON(t, ts, "/api/nodes?source=notion")
	if m["total"].(float64) != 2 {
		t.Errorf("source filter total = %v, want 2", m["total"])
	}

	// Pagination.
	code, m = getJSON(t, ts, "/api/nodes?limit=1&offset=1")
	if len(m["items"].([]any)) != 1 {
		t.Errorf("pagination items = %v", len(m["items"].([]any)))
	}
}

func TestGetNode(t *testing.T) {
	ts, s := newTestServer(t)
	seed(t, s)

	code, m := getJSON(t, ts, "/api/nodes/notion%3Aalpha")
	if code != http.StatusOK {
		t.Fatalf("code=%d", code)
	}
	if m["title"] != "Alpha Notes" {
		t.Errorf("title = %v", m["title"])
	}
	if !strings.Contains(m["markdown"].(string), "alpha project") {
		t.Errorf("markdown = %v", m["markdown"])
	}

	// Slashes in an id work via URL escaping.
	code, _ = getJSON(t, ts, "/api/nodes/"+url.PathEscape("dropbox:recipes/pie.md"))
	if code != http.StatusOK {
		t.Errorf("escaped id code=%d", code)
	}

	code, _ = getJSON(t, ts, "/api/nodes/nope")
	if code != http.StatusNotFound {
		t.Errorf("missing node code=%d, want 404", code)
	}
}

func TestPutAndDeleteNode(t *testing.T) {
	ts, s := newTestServer(t)

	body := map[string]any{
		"source":   "local",
		"title":    "Fresh Idea",
		"markdown": "# Fresh Idea\n\nbrainstorm about the api",
		"tags":     []string{"idea", "api"},
		"meta":     map[string]string{"k": "v"},
	}
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/nodes/local%3Aidea", jsonBody(t, body))
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d", resp.StatusCode)
	}

	code, m := getJSON(t, ts, "/api/nodes/local%3Aidea")
	if code != http.StatusOK || m["title"] != "Fresh Idea" {
		t.Errorf("after PUT: code=%d m=%v", code, m)
	}

	// Searchable via the API.
	code, m = getJSON(t, ts, "/api/search?q=brainstorm")
	if code != http.StatusOK || len(m["items"].([]any)) != 1 {
		t.Errorf("search after PUT: code=%d m=%v", code, m)
	}

	// Delete.
	req, err = http.NewRequest(http.MethodDelete, ts.URL+"/api/nodes/local%3Aidea", nil)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	del, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	del.Body.Close()
	if del.StatusCode != http.StatusNoContent {
		t.Errorf("DELETE status = %d", del.StatusCode)
	}
	code, _ = getJSON(t, ts, "/api/nodes/local%3Aidea")
	if code != http.StatusNotFound {
		t.Errorf("after DELETE code=%d, want 404", code)
	}
	if _, err := s.GetNode("local:idea"); err != store.ErrNotFound {
		t.Errorf("node still in store")
	}
}

func TestSearch(t *testing.T) {
	ts, s := newTestServer(t)
	seed(t, s)

	code, m := getJSON(t, ts, "/api/search?q=apple")
	if code != http.StatusOK {
		t.Fatalf("code=%d", code)
	}
	items := m["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2 (alpha content + dropbox title)", len(items))
	}
	top := items[0].(map[string]any)
	if _, ok := top["snippet"]; !ok {
		t.Errorf("no snippet in %v", top)
	}

	code, m = getJSON(t, ts, "/api/search")
	if code != http.StatusBadRequest {
		t.Errorf("missing q: code=%d, want 400", code)
	}
}

func TestGraphEndpoints(t *testing.T) {
	ts, s := newTestServer(t)
	seed(t, s)

	// neighbors
	code, m := getJSON(t, ts, "/api/graph/neighbors/notion%3Abeta")
	if code != http.StatusOK {
		t.Fatalf("neighbors code=%d", code)
	}
	edges := m["edges"].([]any)
	if len(edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(edges))
	}
	first := edges[0].(map[string]any)
	if first["kind"] != "wikilink" || first["title"] != "Alpha Notes" {
		t.Errorf("edge = %v", first)
	}

	// related (embeddings)
	code, m = getJSON(t, ts, "/api/graph/related/notion%3Aalpha?k=1")
	if code != http.StatusOK {
		t.Fatalf("related code=%d", code)
	}
	rel := m["items"].([]any)
	if len(rel) != 1 || rel[0].(map[string]any)["id"] != "notion:beta" {
		t.Errorf("related = %v", rel)
	}

	// related on a node without embeddings is an empty list, not an error
	code, m = getJSON(t, ts, "/api/graph/related/nope?k=1")
	if code != http.StatusNotFound {
		t.Errorf("related on missing node: code=%d, want 404", code)
	}

	// path
	code, m = getJSON(t, ts, "/api/graph/path/notion%3Aalpha/notion%3Abeta")
	if code != http.StatusOK {
		t.Fatalf("path code=%d", code)
	}
	p := m["path"].([]any)
	if len(p) != 2 || p[0] != "notion:alpha" || p[1] != "notion:beta" {
		t.Errorf("path = %v", p)
	}

	// disconnected path is an empty list
	code, m = getJSON(t, ts, "/api/graph/path/notion%3Aalpha/"+url.PathEscape("dropbox:recipes/pie.md"))
	if len(m["path"].([]any)) != 0 {
		t.Errorf("disconnected path = %v", m["path"])
	}
}

func jsonBody(t *testing.T, v any) io.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(b)
}

func TestResolveTitle(t *testing.T) {
	ts, s := newTestServer(t)
	seed(t, s)

	code, m := getJSON(t, ts, "/api/graph/resolve?title="+url.QueryEscape("Alpha Notes"))
	if code != 200 || m["id"] != "notion:alpha" {
		t.Errorf("resolve: code=%d body=%v", code, m)
	}

	// fallback: mirror filename without extension
	code, m = getJSON(t, ts, "/api/graph/resolve?title="+url.QueryEscape("pie"))
	if code != 200 || m["id"] != "dropbox:recipes/pie.md" {
		t.Errorf("resolve by filename: code=%d body=%v", code, m)
	}

	code, _ = getJSON(t, ts, "/api/graph/resolve?title=No+Such+Title")
	if code != 404 {
		t.Errorf("resolve unknown title: code=%d, want 404", code)
	}

	code, _ = getJSON(t, ts, "/api/graph/resolve")
	if code != 400 {
		t.Errorf("resolve without title: code=%d, want 400", code)
	}
}

func TestPWAStaticServing(t *testing.T) {
	ts, _ := newTestServer(t)

	for _, p := range []string{"/", "/index.html", "/app.js", "/app.css", "/manifest.webmanifest", "/sw.js", "/icon-192.png"} {
		resp, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatalf("GET %s: %v", p, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("GET %s: code=%d", p, resp.StatusCode)
		}
		if len(body) == 0 {
			t.Errorf("GET %s: empty body", p)
		}
	}

	// index.html is served at the root
	resp, _ := http.Get(ts.URL + "/")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "Second brain") {
		t.Errorf("index.html missing title: %q", body)
	}

	// API still wins over the static catch-all
	code, _ := getJSON(t, ts, "/api/health")
	if code != 200 {
		t.Errorf("health code=%d, want 200", code)
	}
}

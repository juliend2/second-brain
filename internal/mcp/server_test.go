package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"desrosiers.org/brain/internal/server"
	"desrosiers.org/brain/internal/store"
)

// newTestEnv wires the real store + HTTP API + MCP server over an in-memory
// transport, so every test exercises the full tool → client → API path.
func newTestEnv(t *testing.T) (*store.Store, *sdkmcp.ClientSession) {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "brain.db"), filepath.Join(dir, "notes"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	api := httptest.NewServer(server.New(s).Handler())
	t.Cleanup(api.Close)

	client, err := NewClient(api.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	mcpServer := New(client)
	t1, t2 := sdkmcp.NewInMemoryTransports()
	if _, err := mcpServer.Connect(context.Background(), t1, nil); err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	cli := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	sess, err := cli.Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { sess.Close() })
	return s, sess
}

func call(t *testing.T, sess *sdkmcp.ClientSession, name string, args any) *sdkmcp.CallToolResult {
	t.Helper()
	res, err := sess.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		t.Fatalf("CallTool(%s): %v", name, err)
	}
	return res
}

func text(t *testing.T, res *sdkmcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) == 0 {
		return ""
	}
	tc, ok := res.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("content type %T", res.Content[0])
	}
	return tc.Text
}

func TestToolsListed(t *testing.T) {
	_, sess := newTestEnv(t)
	want := map[string]bool{
		"search_corpus": true, "get_node": true, "related": true,
		"find_path": true, "create_note": true,
	}
	got := map[string]bool{}
	for tool, err := range sess.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatalf("Tools: %v", err)
		}
		got[tool.Name] = true
	}
	for name := range want {
		if !got[name] {
			t.Errorf("tool %q not listed", name)
		}
	}
}

func TestSearchTool(t *testing.T) {
	s, sess := newTestEnv(t)
	if err := s.PutNode(&store.Node{ID: "notion:a", Source: "notion", Title: "Apples", Markdown: "How to grow crisp apples in the north."}); err != nil {
		t.Fatalf("PutNode: %v", err)
	}

	res := call(t, sess, "search_corpus", map[string]any{"query": "apples", "limit": 5})
	if res.IsError {
		t.Fatalf("unexpected error: %s", text(t, res))
	}
	var out searchOutput
	if err := json.Unmarshal([]byte(text(t, res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].ID != "notion:a" || out.Items[0].Title != "Apples" {
		t.Errorf("items = %+v", out.Items)
	}
	if !strings.Contains(out.Items[0].Snippet, "apples") {
		t.Errorf("snippet = %q", out.Items[0].Snippet)
	}
}

func TestSearchToolMissingQuery(t *testing.T) {
	_, sess := newTestEnv(t)
	res := call(t, sess, "search_corpus", map[string]any{})
	if !res.IsError {
		t.Fatalf("want tool error, got %q", text(t, res))
	}
}

func TestGetNodeTool(t *testing.T) {
	s, sess := newTestEnv(t)
	if err := s.PutNode(&store.Node{ID: "notion:abc", Source: "notion", Title: "Beta Notes", Markdown: "# Beta Notes\n\nPears."}); err != nil {
		t.Fatalf("PutNode: %v", err)
	}

	res := call(t, sess, "get_node", map[string]any{"id": "notion:abc"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", text(t, res))
	}
	var out nodeOutput
	if err := json.Unmarshal([]byte(text(t, res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ID != "notion:abc" || out.Title != "Beta Notes" || !strings.Contains(out.Markdown, "Pears.") {
		t.Errorf("out = %+v", out)
	}
}

func TestGetNodeToolMissingNode(t *testing.T) {
	_, sess := newTestEnv(t)
	res := call(t, sess, "get_node", map[string]any{"id": "notion:missing"})
	if !res.IsError {
		t.Fatalf("want tool error, got %q", text(t, res))
	}
	if !strings.Contains(text(t, res), "not found") {
		t.Errorf("error text = %q", text(t, res))
	}
}

func TestRelatedTool(t *testing.T) {
	s, sess := newTestEnv(t)
	for _, n := range []*store.Node{
		{ID: "notion:a", Source: "notion", Title: "Alpha", Markdown: "a"},
		{ID: "notion:b", Source: "notion", Title: "Beta", Markdown: "b"},
	} {
		if err := s.PutNode(n); err != nil {
			t.Fatalf("PutNode: %v", err)
		}
	}
	s.SetEmbedding("notion:a", []float32{1, 0, 0})
	s.SetEmbedding("notion:b", []float32{1, 0, 0})

	res := call(t, sess, "related", map[string]any{"id": "notion:a", "k": 5})
	if res.IsError {
		t.Fatalf("unexpected error: %s", text(t, res))
	}
	var out relatedOutput
	if err := json.Unmarshal([]byte(text(t, res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Items) != 1 || out.Items[0].ID != "notion:b" || out.Items[0].Score < 0.99 {
		t.Errorf("items = %+v", out.Items)
	}
}

func TestFindPathTool(t *testing.T) {
	s, sess := newTestEnv(t)
	if err := s.PutNode(&store.Node{ID: "notion:a", Source: "notion", Title: "A", Markdown: "a"}); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	if err := s.PutNode(&store.Node{ID: "notion:b", Source: "notion", Title: "B", Markdown: "b"}); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	if err := s.AddEdge("notion:a", "notion:b", store.EdgeWikilink, ""); err != nil {
		t.Fatalf("AddEdge: %v", err)
	}

	res := call(t, sess, "find_path", map[string]any{"from": "notion:a", "to": "notion:b"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", text(t, res))
	}
	var out pathOutput
	if err := json.Unmarshal([]byte(text(t, res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Path) != 2 || out.Path[0] != "notion:a" || out.Path[1] != "notion:b" {
		t.Errorf("path = %v", out.Path)
	}
}

func TestFindPathToolDisconnected(t *testing.T) {
	s, sess := newTestEnv(t)
	if err := s.PutNode(&store.Node{ID: "notion:a", Source: "notion", Title: "A", Markdown: "a"}); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	if err := s.PutNode(&store.Node{ID: "notion:b", Source: "notion", Title: "B", Markdown: "b"}); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	res := call(t, sess, "find_path", map[string]any{"from": "notion:a", "to": "notion:b"})
	var out pathOutput
	if err := json.Unmarshal([]byte(text(t, res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Path) != 0 {
		t.Errorf("path = %v, want empty", out.Path)
	}
}

func TestCreateNoteTool(t *testing.T) {
	s, sess := newTestEnv(t)
	if err := s.PutNode(&store.Node{ID: "notion:beta", Source: "notion", Title: "Beta", Markdown: "b"}); err != nil {
		t.Fatalf("PutNode: %v", err)
	}

	res := call(t, sess, "create_note", map[string]any{
		"id":       "mcp:ideas/foo",
		"title":    "Foo Idea",
		"markdown": "# Foo\n\nConnect this to [[Beta]].",
	})
	if res.IsError {
		t.Fatalf("unexpected error: %s", text(t, res))
	}
	var out nodeOutput
	if err := json.Unmarshal([]byte(text(t, res)), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.ID != "mcp:ideas/foo" || out.Source != "mcp" || out.Status != store.StatusDraft {
		t.Errorf("out = %+v", out)
	}

	// The [[Beta]] wikilink must have become a graph edge immediately.
	edges, err := s.Neighbors("notion:beta")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	found := false
	for _, e := range edges {
		if e.From == "mcp:ideas/foo" && e.To == "notion:beta" && e.Kind == store.EdgeWikilink {
			found = true
		}
	}
	if !found {
		t.Errorf("wikilink edge missing: %+v", edges)
	}

	// The mirror file must exist for the new node.
	n, err := s.GetNode("mcp:ideas/foo")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if !strings.Contains(n.Markdown, "[[Beta]]") {
		t.Errorf("markdown = %q", n.Markdown)
	}
}

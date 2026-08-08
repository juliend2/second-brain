package enrich

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

type fakeEmbedder struct {
	mu     sync.Mutex
	vec    []float32
	err    error
	inputs []string
}

func (f *fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inputs = append(f.inputs, text)
	return f.vec, f.err
}

type fakeLLM struct {
	mu    sync.Mutex
	res   any
	err   error
	users []string
}

func (f *fakeLLM) CompleteJSON(_ context.Context, _, user string, out any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users = append(f.users, user)
	if f.err != nil {
		return f.err
	}
	b, err := json.Marshal(f.res)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func TestOllamaEmbedder(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("path = %q", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]any{"embeddings": [][]float32{{0.1, 0.2}}})
	}))
	defer server.Close()

	e := NewOllamaEmbedder(server.URL, "nomic-embed-text")
	vec, err := e.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 2 || vec[0] != 0.1 {
		t.Errorf("vec = %v", vec)
	}
	if gotBody["model"] != "nomic-embed-text" || gotBody["input"] != "hello" {
		t.Errorf("body = %v", gotBody)
	}
}

func TestOpenAILLM(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		if req["response_format"] == nil {
			t.Errorf("expected response_format json_object")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{
					"content": "```json\n{\"summary\": \"done\"}\n```",
				}},
			},
		})
	}))
	defer server.Close()

	l := NewOpenAILLM("test-key", server.URL, "gpt-4o-mini")
	var out struct {
		Summary string `json:"summary"`
	}
	if err := l.CompleteJSON(context.Background(), "sys", "user", &out); err != nil {
		t.Fatalf("CompleteJSON: %v", err)
	}
	if out.Summary != "done" {
		t.Errorf("summary = %q", out.Summary)
	}
}

func TestEmbedAll(t *testing.T) {
	s := newTestStore(t)
	for _, n := range []*store.Node{
		{ID: "notion:a", Source: "notion", Title: "Alpha", Markdown: "# Alpha\n\ncontent"},
		{ID: "notion:b", Source: "notion", Title: "Beta", Markdown: "# Beta\n\ncontent"},
	} {
		if err := s.PutNode(n); err != nil {
			t.Fatalf("PutNode: %v", err)
		}
	}

	fe := &fakeEmbedder{vec: []float32{1, 0, 0}}
	e := New(s, fe, &fakeLLM{})
	e.SetEmbedWorkers(2)

	stats, err := e.EmbedAll(context.Background())
	if err != nil {
		t.Fatalf("EmbedAll: %v", err)
	}
	if stats.Embedded != 2 {
		t.Errorf("stats = %s, want 2 embedded", stats)
	}
	if !s.HasEmbedding("notion:a") || !s.HasEmbedding("notion:b") {
		t.Errorf("nodes not embedded")
	}

	// Second run is a no-op.
	stats, err = e.EmbedAll(context.Background())
	if err != nil {
		t.Fatalf("EmbedAll: %v", err)
	}
	if stats.Embedded != 0 {
		t.Errorf("stats = %s, want 0 re-embedded", stats)
	}
}

func TestEnrichAll(t *testing.T) {
	s := newTestStore(t)
	a := &store.Node{ID: "notion:a", Source: "notion", Title: "Alpha", Markdown: "# Alpha\n\nabout apples"}
	b := &store.Node{ID: "notion:b", Source: "notion", Title: "Node B", Markdown: "# Node B\n\nabout pears"}
	if err := s.PutNode(a); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	if err := s.PutNode(b); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	s.UpdateStatus("notion:b", store.StatusEnriched)
	s.SetEmbedding("notion:a", []float32{1, 0})
	s.SetEmbedding("notion:b", []float32{1, 0})

	fl := &fakeLLM{res: enrichResult{
		Summary:   "A one-sentence summary about apples.",
		Tags:      []string{"fruit", "orchard"},
		Wikilinks: []string{"Node B"},
		Connects:  []connectItem{{Title: "Node B", Reason: "both discuss fruit farming"}},
	}}
	e := New(s, &fakeEmbedder{}, fl)
	e.SetLLMWorkers(1)

	stats, err := e.EnrichAll(context.Background())
	if err != nil {
		t.Fatalf("EnrichAll: %v", err)
	}
	if stats.Enriched != 1 {
		t.Errorf("stats = %s, want 1 enriched", stats)
	}

	got, err := s.GetNode("notion:a")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.Status != store.StatusEnriched {
		t.Errorf("status = %q", got.Status)
	}
	if got.Summary != "A one-sentence summary about apples." {
		t.Errorf("summary = %q", got.Summary)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "fruit" {
		t.Errorf("tags = %v", got.Tags)
	}
	if !strings.Contains(got.Markdown, "## Related") || !strings.Contains(got.Markdown, "[[Node B]]") {
		t.Errorf("markdown = %q", got.Markdown)
	}

	edges, err := s.Neighbors("notion:b")
	if err != nil {
		t.Fatalf("Neighbors: %v", err)
	}
	kinds := map[string]string{}
	for _, e2 := range edges {
		kinds[e2.Kind] = e2.Reason
	}
	if _, ok := kinds[store.EdgeWikilink]; !ok {
		t.Errorf("missing wikilink edge: %+v", edges)
	}
	if reasons, ok := kinds[store.EdgeConnects]; !ok || reasons != "both discuss fruit farming" {
		t.Errorf("connects edge = %q / %q", store.EdgeConnects, kinds[store.EdgeConnects])
	}

	// B must remain untouched.
	b2, _ := s.GetNode("notion:b")
	if b2.Summary != "" || b2.Status != store.StatusEnriched {
		t.Errorf("B touched: %+v", b2)
	}
}

func TestEnrichAllRetriesOnBadLLM(t *testing.T) {
	s := newTestStore(t)
	if err := s.PutNode(&store.Node{ID: "notion:a", Source: "notion", Title: "Alpha", Markdown: "x"}); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	s.SetEmbedding("notion:a", []float32{1})

	fl := &fakeLLM{err: errors.New("provider returned invalid JSON")}
	e := New(s, &fakeEmbedder{}, fl)
	stats, err := e.EnrichAll(context.Background())
	if err != nil {
		t.Fatalf("EnrichAll: %v", err)
	}
	if stats.FailedLLM != 1 || stats.Enriched != 0 {
		t.Errorf("stats = %s", stats)
	}
	got, _ := s.GetNode("notion:a")
	if got.Status != store.StatusDraft {
		t.Errorf("status = %q, want draft (retried next run)", got.Status)
	}
}

func TestEnrichAllSkipsUnembedded(t *testing.T) {
	s := newTestStore(t)
	if err := s.PutNode(&store.Node{ID: "notion:a", Source: "notion", Title: "Alpha", Markdown: "x"}); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	e := New(s, &fakeEmbedder{}, &fakeLLM{res: enrichResult{}})
	stats, err := e.EnrichAll(context.Background())
	if err != nil {
		t.Fatalf("EnrichAll: %v", err)
	}
	if stats.Skipped != 1 {
		t.Errorf("stats = %s, want 1 skipped", stats)
	}
}

func TestCandidateListReachesLLM(t *testing.T) {
	s := newTestStore(t)
	if err := s.PutNode(&store.Node{ID: "notion:a", Source: "notion", Title: "Alpha", Markdown: "a"}); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	if err := s.PutNode(&store.Node{ID: "notion:b", Source: "notion", Title: "Beta", Markdown: "b"}); err != nil {
		t.Fatalf("PutNode: %v", err)
	}
	s.UpdateStatus("notion:b", store.StatusEnriched)
	s.SetEmbedding("notion:a", []float32{1})
	s.SetEmbedding("notion:b", []float32{1})

	fl := &fakeLLM{res: enrichResult{Summary: "s", Tags: []string{"t"}}}
	e := New(s, &fakeEmbedder{}, fl)
	if _, err := e.EnrichAll(context.Background()); err != nil {
		t.Fatalf("EnrichAll: %v", err)
	}

	fl.mu.Lock()
	defer fl.mu.Unlock()
	if len(fl.users) != 1 {
		t.Fatalf("llm calls = %d, want 1", len(fl.users))
	}
	if !strings.Contains(fl.users[0], "Title: Beta") {
		t.Errorf("candidate not passed to LLM:\n%s", fl.users[0])
	}
}

package enrich

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"desrosiers.org/brain/internal/store"
)

// Stats summarizes an enrichment run.
type Stats struct {
	Embedded    int
	Enriched    int
	Skipped     int
	FailedEmbed int
	FailedLLM   int
}

func (s Stats) String() string {
	return fmt.Sprintf("embedded=%d enriched=%d skipped=%d failed_embed=%d failed_llm=%d",
		s.Embedded, s.Enriched, s.Skipped, s.FailedEmbed, s.FailedLLM)
}

// Embedder interface (implemented by *Embedder, fakeable in tests).
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// LLM interface (implemented by *LLM, fakeable in tests).
type LLM interface {
	CompleteJSON(ctx context.Context, system, user string, out any) error
}

// Enricher coordinates embedding and the cloud enrichment pass.
type Enricher struct {
	store        *store.Store
	embedder     Embedder
	llm          LLM
	embedWorkers int
	llmWorkers   int
	candidates   int
}

// New returns an Enricher with sensible defaults (overridable via Setters).
func New(s *store.Store, embedder Embedder, llm LLM) *Enricher {
	return &Enricher{
		store:        s,
		embedder:     embedder,
		llm:          llm,
		embedWorkers: 4,
		llmWorkers:   2,
		candidates:   10,
	}
}

// SetEmbedWorkers sets embedding concurrency.
func (e *Enricher) SetEmbedWorkers(n int) { e.embedWorkers = n }

// SetLLMWorkers sets enrichment concurrency.
func (e *Enricher) SetLLMWorkers(n int) { e.llmWorkers = n }

// SetCandidates sets how many similar nodes are offered to the LLM.
func (e *Enricher) SetCandidates(n int) { e.candidates = n }

// EmbedAll embeds every node that lacks an embedding.
func (e *Enricher) EmbedAll(ctx context.Context) (Stats, error) {
	var stats Stats
	nodes, err := e.store.ListNodes()
	if err != nil {
		return stats, err
	}

	type job struct{ node *store.Node }
	jobs := make(chan job)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < e.embedWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				text := embedText(j.node.Title, j.node.Markdown)
				vec, err := e.embedder.Embed(ctx, text)
				mu.Lock()
				if err != nil {
					stats.FailedEmbed++
				} else {
					if err := e.store.SetEmbedding(j.node.ID, vec); err != nil {
						stats.FailedEmbed++
					} else {
						stats.Embedded++
					}
				}
				mu.Unlock()
			}
		}()
	}

	for _, n := range nodes {
		if e.store.HasEmbedding(n.ID) {
			continue
		}
		if strings.TrimSpace(n.Title+n.Markdown) == "" {
			continue
		}
		select {
		case jobs <- job{n}:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return stats, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	return stats, nil
}

// EnrichAll runs the cloud pass over every draft node that has an embedding.
func (e *Enricher) EnrichAll(ctx context.Context) (Stats, error) {
	var stats Stats
	nodes, err := e.store.ListNodes()
	if err != nil {
		return stats, err
	}

	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.Status != store.StatusDraft {
			continue
		}
		if !e.store.HasEmbedding(n.ID) {
			stats.Skipped++
			continue
		}
		ids = append(ids, n.ID)
	}

	jobs := make(chan string)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < e.llmWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				err := e.enrichOne(ctx, id)
				mu.Lock()
				switch {
				case err == nil:
					stats.Enriched++
				case errors.Is(err, errSkip):
					stats.Skipped++
				default:
					stats.FailedLLM++
				}
				mu.Unlock()
			}
		}()
	}

	for _, id := range ids {
		select {
		case jobs <- id:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return stats, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	return stats, nil
}

var errSkip = errors.New("enrich: skip")

// enrichOne enriches a single node: gather similar candidates, ask the LLM,
// then apply summary/tags/wikilinks/connects.
func (e *Enricher) enrichOne(ctx context.Context, id string) error {
	n, err := e.store.GetNode(id)
	if err != nil {
		return err
	}

	candidates, err := e.candidateList(ctx, id)
	if err != nil {
		return err
	}

	system := `You enrich notes in a personal knowledge base. Given a note and a list of candidate related notes, return ONLY a JSON object with these fields:
- "summary": one sentence describing the note.
- "tags": 3 to 6 short lowercase tags.
- "wikilinks": an array of candidate titles this note genuinely relates to and that are worth an explicit link. Empty if none. Use exact candidate titles.
- "connects": an array of objects {"title": string, "reason": string} for candidate notes that are thematically connected; "reason" is one short sentence explaining the connection. Empty if none. "title" must be an exact candidate title.`
	user := fmt.Sprintf("NOTE TITLE:\n%s\n\nNOTE CONTENT:\n%s\n\nCANDIDATE NOTES:\n%s",
		n.Title, truncateText(n.Markdown, 12000), candidates)

	var res enrichResult
	if err := e.llm.CompleteJSON(ctx, system, user, &res); err != nil {
		return err
	}
	return e.applyResult(id, n, res)
}

// candidateList renders the top similar nodes as an indented prompt block.
func (e *Enricher) candidateList(ctx context.Context, id string) (string, error) {
	sim, err := e.store.NearestNeighbors(id, e.candidates)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, s := range sim {
		c, err := e.store.GetNode(s.ID)
		if err != nil {
			continue
		}
		blurb := c.Summary
		if blurb == "" {
			blurb = truncateText(c.Markdown, 300)
		}
		if blurb == "" {
			blurb = "(no content)"
		}
		fmt.Fprintf(&b, "- Title: %s\n  Blurb: %s\n", c.Title, blurb)
	}
	return b.String(), nil
}

// applyResult persists an LLM result for a node.
func (e *Enricher) applyResult(id string, n *store.Node, res enrichResult) error {
	existing := map[string]bool{}
	for _, wl := range store.ParseWikilinks(n.Markdown) {
		existing[wl.Target] = true
	}

	var newLinks []string
	for _, t := range res.Wikilinks {
		t = strings.TrimSpace(t)
		if t != "" && !existing[t] {
			newLinks = append(newLinks, t)
			existing[t] = true
		}
	}

	md := n.Markdown
	if len(newLinks) > 0 {
		var b strings.Builder
		b.WriteString(md)
		if !strings.HasSuffix(md, "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n## Related\n")
		for _, t := range newLinks {
			b.WriteString("- [[" + t + "]]\n")
		}
		md = b.String()
	}

	n.Summary = res.Summary
	n.Tags = appendUnique(n.Tags, res.Tags...)
	n.Markdown = md
	n.Status = store.StatusEnriched
	if err := e.store.PutNode(n); err != nil {
		return err
	}
	if _, err := e.store.ExtractEdgesFromMarkdown(id, md); err != nil {
		return err
	}

	for _, c := range res.Connects {
		target, ok := e.store.ResolveTitle(strings.TrimSpace(c.Title))
		if !ok || target == id {
			continue
		}
		if err := e.store.AddEdge(id, target, store.EdgeConnects, c.Reason); err != nil {
			return err
		}
	}
	return nil
}

type enrichResult struct {
	Summary   string        `json:"summary"`
	Tags      []string      `json:"tags"`
	Wikilinks []string      `json:"wikilinks"`
	Connects  []connectItem `json:"connects"`
}

type connectItem struct {
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

func appendUnique(tags []string, more ...string) []string {
	seen := map[string]bool{}
	for _, t := range tags {
		seen[t] = true
	}
	for _, t := range more {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			tags = append(tags, t)
			seen[t] = true
		}
	}
	return tags
}

func embedText(title, md string) string {
	text := title + "\n" + md
	return truncateText(text, 8000)
}

func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

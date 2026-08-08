package store

import "time"

// Status values for a node's enrichment lifecycle.
const (
	StatusDraft    = "draft"    // raw content, not yet enriched
	StatusEnriched = "enriched" // tags/links/summary applied
)

// Edge kinds.
const (
	EdgeWikilink = "wikilink" // from a [[wikilink]] in the markdown
	EdgeConnects = "connects" // LLM-labeled "connects to" edge with a reason
)

// Node is a single item of the second brain: a source, a title and its
// markdown content, plus metadata maintained by the enrichment pipeline.
type Node struct {
	ID        string            // stable identity, e.g. "nt:abc123" or "db:Documents/foo.md"
	Source    string            // "notion", "dropbox", "local", ...
	Title     string            // human title (wikilink target)
	Markdown  string            // content (in-memory; persisted to the mirror)
	RelPath   string            // path of the mirror file, relative to notesDir
	Tags      []string          // LLM/maintained tags
	Summary   string            // one-line LLM summary
	Status    string            // StatusDraft or StatusEnriched
	Meta      map[string]string // arbitrary source metadata (URL, original path, ...)
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Edge is a directed relationship between two nodes.
type Edge struct {
	From      string
	To        string
	Kind      string
	Reason    string
	CreatedAt time.Time
}

package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrNotFound is returned when a node or edge does not exist.
var ErrNotFound = errors.New("store: not found")

// PutNode upserts a node: it writes the markdown content to the mirror (when
// non-empty) and the metadata to SQLite.
func (s *Store) PutNode(n *Node) error {
	rel, err := mirrorPathFor(n.ID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	if n.CreatedAt.IsZero() {
		n.CreatedAt = now
	}
	n.UpdatedAt = now
	n.Status = statusOrDefault(n.Status)

	tags, err := json.Marshal(n.Tags)
	if err != nil {
		return fmt.Errorf("store: marshal tags: %w", err)
	}
	meta, err := json.Marshal(n.Meta)
	if err != nil {
		return fmt.Errorf("store: marshal meta: %w", err)
	}

	// Content is authoritative on disk; only write when provided so that
	// metadata-only updates never clobber existing content.
	if n.Markdown != "" {
		if err := s.writeMirror(rel, n.Markdown); err != nil {
			return fmt.Errorf("store: write mirror: %w", err)
		}
	}

	_, err = s.db.Exec(`
INSERT INTO nodes (id, source, title, rel_path, tags, summary, status, meta, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    title = excluded.title,
    tags = excluded.tags,
    summary = excluded.summary,
    status = excluded.status,
    meta = excluded.meta,
    updated_at = excluded.updated_at`,
		n.ID, n.Source, n.Title, rel, string(tags), n.Summary, n.Status, string(meta),
		n.CreatedAt.Format(time.RFC3339Nano), n.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("store: upsert node: %w", err)
	}
	return nil
}

// GetNode returns a node with its markdown content read from the mirror.
func (s *Store) GetNode(id string) (*Node, error) {
	row := s.db.QueryRow(`
SELECT id, source, title, rel_path, tags, summary, status, meta, created_at, updated_at
FROM nodes WHERE id = ?`, id)

	n, err := scanNode(row)
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(filepath.Join(s.notesDir, n.RelPath))
	if err == nil {
		n.Markdown = string(content)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("store: read mirror: %w", err)
	}
	return n, nil
}

// ListNodes returns all nodes (metadata only, no content), most recently
// updated first.
func (s *Store) ListNodes() ([]*Node, error) {
	rows, err := s.db.Query(`
SELECT id, source, title, rel_path, tags, summary, status, meta, created_at, updated_at
FROM nodes ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("store: list nodes: %w", err)
	}
	defer rows.Close()

	var nodes []*Node
	for rows.Next() {
		n, err := scanNode(rows)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, rows.Err()
}

// DeleteNode removes a node (and its edges, via cascade) and its mirror file.
func (s *Store) DeleteNode(id string) error {
	var rel string
	err := s.db.QueryRow(`SELECT rel_path FROM nodes WHERE id = ?`, id).Scan(&rel)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: delete node: %w", err)
	}

	if _, err := s.db.Exec(`DELETE FROM nodes WHERE id = ?`, id); err != nil {
		return fmt.Errorf("store: delete node: %w", err)
	}
	os.Remove(filepath.Join(s.notesDir, rel))
	return nil
}

// UpdateStatus sets a node's enrichment status.
func (s *Store) UpdateStatus(id, status string) error {
	res, err := s.db.Exec(`UPDATE nodes SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("store: update status: %w", err)
	}
	return requireAffected(res, id)
}

// SetSummary stores a node's one-line summary.
func (s *Store) SetSummary(id, summary string) error {
	res, err := s.db.Exec(`UPDATE nodes SET summary = ?, updated_at = ? WHERE id = ?`,
		summary, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("store: set summary: %w", err)
	}
	return requireAffected(res, id)
}

// AddTags appends tags to a node, deduplicated.
func (s *Store) AddTags(id string, tags ...string) error {
	n, err := s.GetNode(id)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, t := range n.Tags {
		seen[t] = true
	}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			n.Tags = append(n.Tags, t)
			seen[t] = true
		}
	}
	encoded, err := json.Marshal(n.Tags)
	if err != nil {
		return fmt.Errorf("store: marshal tags: %w", err)
	}
	if _, err := s.db.Exec(`UPDATE nodes SET tags = ?, updated_at = ? WHERE id = ?`,
		encoded, time.Now().UTC().Format(time.RFC3339Nano), id); err != nil {
		return fmt.Errorf("store: add tags: %w", err)
	}
	return nil
}

// Scan walks the markdown mirror and inserts a draft node for any file not yet
// known to the database. Useful for first import or manual additions.
func (s *Store) Scan() error {
	return filepath.WalkDir(s.notesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(s.notesDir, path)
		if err != nil {
			return err
		}
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		if len(parts) != 2 {
			return nil // stray file at the mirror root: ignore
		}
		source, rest := parts[0], parts[1]

		var count int
		if err := s.db.QueryRow(`SELECT COUNT(1) FROM nodes WHERE rel_path = ?`, rel).Scan(&count); err != nil {
			return fmt.Errorf("store: scan: %w", err)
		}
		if count > 0 {
			return nil
		}

		id := source + ":" + strings.TrimSuffix(rest, filepath.Ext(rest))
		content, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("store: scan: %w", err)
		}
		n := &Node{
			ID:       id,
			Source:   source,
			Title:    baseTitle(rest),
			Markdown: string(content),
		}
		return s.PutNode(n)
	})
}

func baseTitle(rel string) string {
	base := filepath.Base(rel)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.ReplaceAll(base, "_", " ")
	base = strings.ReplaceAll(base, "-", " ")
	return base
}

// scanNode decodes one row into a Node.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanNode(row rowScanner) (*Node, error) {
	var (
		n                    Node
		tagsJSON, metaJSON   string
		createdAt, updatedAt string
	)
	if err := row.Scan(&n.ID, &n.Source, &n.Title, &n.RelPath, &tagsJSON, &n.Summary,
		&n.Status, &metaJSON, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: scan node: %w", err)
	}
	if err := json.Unmarshal([]byte(tagsJSON), &n.Tags); err != nil {
		return nil, fmt.Errorf("store: unmarshal tags: %w", err)
	}
	if err := json.Unmarshal([]byte(metaJSON), &n.Meta); err != nil {
		return nil, fmt.Errorf("store: unmarshal meta: %w", err)
	}
	n.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	n.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &n, nil
}

func requireAffected(res sql.Result, id string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func statusOrDefault(s string) string {
	if s == "" {
		return StatusDraft
	}
	return s
}

package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SearchHit is one full-text search result.
type SearchHit struct {
	ID      string
	Source  string
	Title   string
	Summary string
	Snippet string
}

// Search runs an FTS5 query over node titles and content, ranked by relevance.
func (s *Store) Search(query string, limit int) ([]SearchHit, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}

	rows, err := s.db.Query(`
SELECT f.node_id, n.source, f.title, n.summary,
       snippet(node_fts, 2, '[', ']', '…', 24)
FROM node_fts f JOIN nodes n ON n.id = f.node_id
WHERE f.node_fts MATCH ?
ORDER BY bm25(f.node_fts)
LIMIT ?`, ftsQuery(query), limit)
	if err != nil {
		return nil, fmt.Errorf("store: search: %w", err)
	}
	defer rows.Close()

	var hits []SearchHit
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.ID, &h.Source, &h.Title, &h.Summary, &h.Snippet); err != nil {
			return nil, fmt.Errorf("store: search: %w", err)
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

// RebuildIndex rebuilds the FTS index from the markdown mirror. Useful after
// manual edits to the mirror or a schema migration.
func (s *Store) RebuildIndex() error {
	if _, err := s.db.Exec(`DELETE FROM node_fts`); err != nil {
		return fmt.Errorf("store: rebuild index: %w", err)
	}

	nodes, err := s.ListNodes()
	if err != nil {
		return err
	}
	for _, n := range nodes {
		content, err := os.ReadFile(filepath.Join(s.notesDir, n.RelPath))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("store: rebuild index: %w", err)
		}
		if err := s.indexNode(n.ID, n.Title, string(content)); err != nil {
			return err
		}
	}
	return nil
}

// indexNode upserts a node's row in the FTS index.
func (s *Store) indexNode(id, title, content string) error {
	if _, err := s.db.Exec(`DELETE FROM node_fts WHERE node_id = ?`, id); err != nil {
		return fmt.Errorf("store: index node: %w", err)
	}
	if _, err := s.db.Exec(`INSERT INTO node_fts (node_id, title, content) VALUES (?, ?, ?)`,
		id, title, content); err != nil {
		return fmt.Errorf("store: index node: %w", err)
	}
	return nil
}

// ftsQuery turns free-form user input into a safe FTS5 query: each token is
// quoted (double quotes escaped) and ANDed together.
func ftsQuery(q string) string {
	tokens := strings.Fields(q)
	quoted := make([]string, 0, len(tokens))
	for _, t := range tokens {
		quoted = append(quoted, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
	}
	return strings.Join(quoted, " AND ")
}

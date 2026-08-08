package store

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var wikilinkRe = regexp.MustCompile(`\[\[([^\]]+)\]\]`)

// Wikilink is a parsed [[target]] or [[target|display]] reference.
type Wikilink struct {
	Target  string
	Display string
}

// ParseWikilinks extracts [[wikilinks]] from markdown, honoring Obsidian's
// optional "|alias" form.
func ParseWikilinks(md string) []Wikilink {
	var out []Wikilink
	for _, m := range wikilinkRe.FindAllStringSubmatch(md, -1) {
		inner := strings.TrimSpace(m[1])
		if inner == "" {
			continue
		}
		if i := strings.Index(inner, "|"); i >= 0 {
			out = append(out, Wikilink{Target: strings.TrimSpace(inner[:i]), Display: strings.TrimSpace(inner[i+1:])})
		} else {
			out = append(out, Wikilink{Target: inner})
		}
	}
	return out
}

// AddEdge records a directed edge between two nodes. Duplicate (from, to,
// kind) edges are ignored.
func (s *Store) AddEdge(from, to, kind, reason string) error {
	if from == to {
		return errors.New("store: cannot link a node to itself")
	}
	for _, id := range []string{from, to} {
		if !s.nodeExists(id) {
			return fmt.Errorf("store: add edge: node %q not found", id)
		}
	}
	_, err := s.db.Exec(`
INSERT INTO edges (from_id, to_id, kind, reason, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(from_id, to_id, kind) DO NOTHING`,
		from, to, kind, reason, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("store: add edge: %w", err)
	}
	return nil
}

// Neighbors returns the edges touching a node, in either direction.
func (s *Store) Neighbors(id string) ([]Edge, error) {
	rows, err := s.db.Query(`
SELECT from_id, to_id, kind, reason, created_at FROM edges
WHERE from_id = ? OR to_id = ?`, id, id)
	if err != nil {
		return nil, fmt.Errorf("store: neighbors: %w", err)
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		var (
			e         Edge
			createdAt string
		)
		if err := rows.Scan(&e.From, &e.To, &e.Kind, &e.Reason, &createdAt); err != nil {
			return nil, fmt.Errorf("store: neighbors: %w", err)
		}
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		edges = append(edges, e)
	}
	return edges, rows.Err()
}

// ResolveTitle maps a wikilink target to a node id. It matches on exact title,
// then on the mirror filename without extension (case-insensitive).
func (s *Store) ResolveTitle(title string) (string, bool) {
	var id string
	err := s.db.QueryRow(`
SELECT id FROM nodes WHERE title = ? ORDER BY updated_at DESC LIMIT 1`, title).Scan(&id)
	if err == nil {
		return id, true
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", false
	}

	// Fallback: match the mirror filename (without extension).
	base := strings.TrimSuffix(title, ".md")
	rows, err := s.db.Query(`SELECT id, rel_path FROM nodes`)
	if err != nil {
		return "", false
	}
	defer rows.Close()
	for rows.Next() {
		var id, rel string
		if err := rows.Scan(&id, &rel); err != nil {
			return "", false
		}
		fileBase := strings.TrimSuffix(rel[strings.LastIndex(rel, "/")+1:], ".md")
		if strings.EqualFold(fileBase, base) {
			return id, true
		}
	}
	return "", false
}

// ExtractEdgesFromMarkdown parses the [[wikilinks]] of a node's markdown and
// records a wikilink edge for every target that resolves to an existing node.
func (s *Store) ExtractEdgesFromMarkdown(nodeID, md string) ([]Edge, error) {
	var added []Edge
	for _, wl := range ParseWikilinks(md) {
		target, ok := s.ResolveTitle(wl.Target)
		if !ok || target == nodeID {
			continue
		}
		if err := s.AddEdge(nodeID, target, EdgeWikilink, ""); err != nil {
			return nil, err
		}
		added = append(added, Edge{From: nodeID, To: target, Kind: EdgeWikilink})
	}
	return added, nil
}

// ShortestPath returns the node ids of the shortest path between from and to,
// following edges as undirected. It returns nil when the two nodes are the
// same or disconnected.
func (s *Store) ShortestPath(from, to string) ([]string, error) {
	if from == to {
		return []string{from}, nil
	}
	if !s.nodeExists(from) || !s.nodeExists(to) {
		return nil, fmt.Errorf("store: shortest path: node not found")
	}

	prev := map[string]string{from: ""}
	queue := []string{from}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == to {
			break
		}
		neighbors, err := s.Neighbors(cur)
		if err != nil {
			return nil, err
		}
		for _, e := range neighbors {
			next := e.To
			if e.To == cur {
				next = e.From
			}
			if _, seen := prev[next]; seen {
				continue
			}
			prev[next] = cur
			queue = append(queue, next)
		}
	}

	if _, ok := prev[to]; !ok {
		return nil, nil // disconnected
	}
	var path []string
	for cur := to; cur != ""; cur = prev[cur] {
		path = append([]string{cur}, path...)
	}
	return path, nil
}

func (s *Store) nodeExists(id string) bool {
	var one int
	err := s.db.QueryRow(`SELECT 1 FROM nodes WHERE id = ?`, id).Scan(&one)
	return err == nil
}

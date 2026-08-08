// Package store implements the "second brain" repository: a git-versioned
// markdown mirror of nodes plus a SQLite database holding metadata, edges and
// embeddings. The markdown files are the source of truth for content; SQLite
// is the machine-readable index over it.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Store is a repository backed by a SQLite database and a markdown mirror
// directory.
type Store struct {
	db       *sql.DB
	notesDir string // absolute path to the markdown mirror root
}

// Open opens (creating if necessary) the SQLite database and the notes mirror
// directory, and ensures the schema exists.
func Open(dbPath, notesDir string) (*Store, error) {
	if dbPath == "" || notesDir == "" {
		return nil, errors.New("store: dbPath and notesDir are required")
	}
	if err := os.MkdirAll(notesDir, 0o755); err != nil {
		return nil, fmt.Errorf("store: create notes dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("store: create db dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("store: open db: %w", err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		return nil, fmt.Errorf("store: set WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		return nil, fmt.Errorf("store: set busy_timeout: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		return nil, fmt.Errorf("store: set foreign_keys: %w", err)
	}
	if err := createSchema(db); err != nil {
		db.Close()
		return nil, err
	}

	abs, err := filepath.Abs(notesDir)
	if err != nil {
		return nil, fmt.Errorf("store: resolve notes dir: %w", err)
	}

	return &Store{db: db, notesDir: abs}, nil
}

// Close releases the underlying database handle.
func (s *Store) Close() error {
	return s.db.Close()
}

// NotesDir returns the absolute path of the markdown mirror root.
func (s *Store) NotesDir() string {
	return s.notesDir
}

func createSchema(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS nodes (
    id         TEXT PRIMARY KEY,
    source     TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    rel_path   TEXT NOT NULL,
    tags       TEXT NOT NULL DEFAULT '[]',
    summary    TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL DEFAULT 'draft',
    meta       TEXT NOT NULL DEFAULT '{}',
    embedding  BLOB,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS edges (
    from_id    TEXT NOT NULL,
    to_id      TEXT NOT NULL,
    kind       TEXT NOT NULL,
    reason     TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    PRIMARY KEY (from_id, to_id, kind),
    FOREIGN KEY (from_id) REFERENCES nodes(id) ON DELETE CASCADE,
    FOREIGN KEY (to_id)   REFERENCES nodes(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_edges_from ON edges(from_id);
CREATE INDEX IF NOT EXISTS idx_edges_to   ON edges(to_id);
`
	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("store: create schema: %w", err)
	}
	return nil
}

// mirrorPathFor derives the markdown mirror path (relative to notesDir) from a
// node id of the form "<source>:<path>". A missing file extension gets ".md".
func mirrorPathFor(id string) (string, error) {
	source, rest, ok := strings.Cut(id, ":")
	if !ok || source == "" || rest == "" {
		return "", fmt.Errorf("store: invalid node id %q: expected <source>:<path>", id)
	}
	rel, err := sanitizePath(rest)
	if err != nil {
		return "", fmt.Errorf("store: invalid node id %q: %w", id, err)
	}
	if filepath.Ext(rel) == "" {
		rel += ".md"
	}
	return filepath.Join(slugify(source), rel), nil
}

func slugify(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r - 'A' + 'a')
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

func sanitizePath(p string) (string, error) {
	cleaned := filepath.Clean(p)
	if filepath.IsAbs(cleaned) {
		return "", errors.New("absolute paths are not allowed")
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", errors.New("path escapes the notes directory")
	}
	return cleaned, nil
}

// writeMirror atomically writes content to rel (relative to notesDir).
func (s *Store) writeMirror(rel, content string) error {
	full := filepath.Join(s.notesDir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, full)
}

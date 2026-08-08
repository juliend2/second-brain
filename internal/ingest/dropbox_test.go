package ingest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"desrosiers.org/brain/internal/store"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDropboxSync(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()

	writeFile(t, filepath.Join(dir, "notes", "hello.md"), "# hello")
	writeFile(t, filepath.Join(dir, "notes", "sub", "deep.txt"), "deep")
	writeFile(t, filepath.Join(dir, "notes", "skip.pdf"), "%PDF") // non-text ext
	writeFile(t, filepath.Join(dir, ".hidden"), "x")              // hidden file

	d := NewDropbox(s, dir)
	stats, err := d.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if stats.New != 2 {
		t.Errorf("stats = %s, want 2 new", stats)
	}

	n, err := s.GetNode("dropbox:notes/hello.md")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if n.Title != "hello" || n.Markdown != "# hello" || n.Source != "dropbox" {
		t.Errorf("node = %+v", n)
	}
	if _, err := s.GetNode("dropbox:notes/skip.pdf"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("pdf should not be ingested")
	}
	if _, err := s.GetNode("dropbox:.hidden"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("hidden file should not be ingested")
	}

	// Unchanged re-sync must not churn.
	stats, err = d.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if stats.New != 0 || stats.Updated != 0 {
		t.Errorf("stats = %s, want no churn", stats)
	}

	// Modify a file.
	writeFile(t, filepath.Join(dir, "notes", "hello.md"), "# hello v2")
	stats, err = d.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if stats.Updated != 1 {
		t.Errorf("stats = %s, want 1 updated", stats)
	}
	n, _ = s.GetNode("dropbox:notes/hello.md")
	if n.Markdown != "# hello v2" {
		t.Errorf("markdown = %q", n.Markdown)
	}

	// Remove a file: node must be pruned.
	if err := os.Remove(filepath.Join(dir, "notes", "hello.md")); err != nil {
		t.Fatal(err)
	}
	stats, err = d.Sync(context.Background())
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := s.GetNode("dropbox:notes/hello.md"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("removed file should be pruned")
	}
}

func TestDropboxWatch(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()

	d := NewDropbox(s, dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watchDone := make(chan error, 1)
	go func() { watchDone <- d.Watch(ctx) }()

	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if cond() {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		select {
		case err := <-watchDone:
			t.Fatalf("watch ended early (%s): %v", what, err)
		default:
		}
		t.Fatalf("timeout waiting for %s", what)
	}

	writeFile(t, filepath.Join(dir, "live.md"), "# live")
	waitFor("create", func() bool {
		_, err := s.GetNode("dropbox:live.md")
		return err == nil
	})

	writeFile(t, filepath.Join(dir, "live.md"), "# live v2")
	waitFor("update", func() bool {
		n, err := s.GetNode("dropbox:live.md")
		return err == nil && n.Markdown == "# live v2"
	})

	if err := os.Remove(filepath.Join(dir, "live.md")); err != nil {
		t.Fatal(err)
	}
	waitFor("delete", func() bool {
		_, err := s.GetNode("dropbox:live.md")
		return errors.Is(err, store.ErrNotFound)
	})
}

package ingest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"desrosiers.org/brain/internal/store"
)

// textExts are the file extensions ingested from a synced Dropbox folder.
// Binary parsing (pdf, docx) is deferred.
var textExts = map[string]bool{
	".md": true, ".markdown": true, ".txt": true,
}

// Dropbox ingests files from a directory synced by the official Dropbox
// client. Nodes are identified by "dropbox:<relpath>" and content lives in the
// store's mirror under "dropbox/<relpath>".
type Dropbox struct {
	store *store.Store
	dir   string
	// skip reports whether a file (slash-relative to dir) should be ignored.
	skip func(rel string) bool
}

// NewDropbox returns a Dropbox ingester for a locally synced directory.
func NewDropbox(s *store.Store, dir string) *Dropbox {
	return &Dropbox{
		store: s,
		dir:   dir,
		skip:  defaultSkip,
	}
}

func defaultSkip(rel string) bool {
	if strings.HasPrefix(filepath.Base(rel), ".") {
		return true // hidden files (e.g. .DS_Store)
	}
	return !textExts[strings.ToLower(filepath.Ext(rel))]
}

// fileResult tells callers whether a file was created, updated or skipped.
type fileResult int

const (
	fileSkipped fileResult = iota
	fileNew
	fileUpdated
)

// Sync walks the synced directory once and mirrors every text file into the
// store, pruning nodes whose files have disappeared.
func (d *Dropbox) Sync(ctx context.Context) (Stats, error) {
	var stats Stats
	seen := map[string]bool{}

	err := filepath.WalkDir(d.dir, func(path string, ent fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if ent.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(d.dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		seen[rel] = true

		if d.skip(rel) {
			stats.Skipped++
			return nil
		}
		stats.Pages++
		switch res, err := d.processFile(rel); {
		case err != nil:
			stats.Failed++
			return fmt.Errorf("dropbox: %s: %w", rel, err)
		case res == fileNew:
			stats.New++
		case res == fileUpdated:
			stats.Updated++
		default:
			stats.Skipped++
		}
		return nil
	})
	if err != nil {
		return stats, err
	}

	// Prune nodes whose Dropbox file no longer exists.
	nodes, err := d.store.ListNodes()
	if err != nil {
		return stats, err
	}
	for _, n := range nodes {
		if n.Source != "dropbox" {
			continue
		}
		rel := strings.TrimPrefix(n.RelPath, "dropbox"+string(filepath.Separator))
		if !seen[filepath.ToSlash(rel)] {
			if err := d.deleteNode(n.ID); err != nil {
				return stats, err
			}
		}
	}
	return stats, nil
}

// Watch mirrors the synced directory continuously using fsnotify, until ctx is
// cancelled. It starts with a one-shot Sync to converge to current state (files
// created while the watcher was down are picked up, removed ones pruned), then
// reacts to events. Writes are debounced to coalesce partial writes.
func (d *Dropbox) Watch(ctx context.Context) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("dropbox: new watcher: %w", err)
	}
	defer w.Close()

	// Install the watchers before the initial sync so no change window is
	// missed; anything created mid-sync is seen by the sync itself, anything
	// after is caught by the event loop.
	if err := addWatcherRecursive(w, d.dir); err != nil {
		return fmt.Errorf("dropbox: watch %s: %w", d.dir, err)
	}
	if _, err := d.Sync(ctx); err != nil {
		return fmt.Errorf("dropbox: initial sync: %w", err)
	}

	dirty := map[string]bool{}
	flush := func() {
		for rel := range dirty {
			delete(dirty, rel)
			if err := d.handleRel(rel); err != nil {
				fmt.Printf("dropbox: %s: %v\n", rel, err)
			}
		}
	}

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			rel, err := filepath.Rel(d.dir, ev.Name)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			if ev.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
					addWatcherRecursive(w, ev.Name)
				}
			}
			dirty[rel] = true
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			fmt.Printf("dropbox watcher: %v\n", err)
		case <-ticker.C:
			flush()
		}
	}
}

// processFile mirrors a single file into the store, skipping it when its
// content is unchanged (avoids git churn on re-syncs).
func (d *Dropbox) processFile(rel string) (fileResult, error) {
	id := "dropbox:" + rel
	full := filepath.Join(d.dir, filepath.FromSlash(rel))

	content, err := os.ReadFile(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fileSkipped, d.deleteNode(id)
		}
		return fileSkipped, err
	}

	existing, err := d.store.GetNode(id)
	if err == nil {
		if existing.Markdown == string(content) {
			return fileSkipped, nil
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		return fileSkipped, err
	}

	node := &store.Node{
		ID:       id,
		Source:   "dropbox",
		Title:    titleFromRel(rel),
		Markdown: string(content),
		Meta:     map[string]string{"path": rel},
	}
	if err := d.store.PutNode(node); err != nil {
		return fileSkipped, err
	}
	if existing == nil {
		return fileNew, nil
	}
	return fileUpdated, nil
}

func (d *Dropbox) handleRel(rel string) error {
	full := filepath.Join(d.dir, filepath.FromSlash(rel))
	info, err := os.Stat(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return d.deleteNode("dropbox:" + rel)
		}
		return err
	}
	if info.IsDir() || d.skip(rel) {
		return nil
	}
	_, err = d.processFile(rel)
	return err
}

func (d *Dropbox) deleteNode(id string) error {
	if err := d.store.DeleteNode(id); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	return nil
}

func addWatcherRecursive(w *fsnotify.Watcher, dir string) error {
	return filepath.WalkDir(dir, func(path string, ent fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ent.IsDir() {
			return w.Add(path)
		}
		return nil
	})
}

func titleFromRel(rel string) string {
	base := filepath.Base(rel)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.ReplaceAll(base, "_", " ")
	return strings.ReplaceAll(base, "-", " ")
}

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"desrosiers.org/brain/internal/ingest"
	"desrosiers.org/brain/internal/store"
)

func main() {
	godotenv.Load()

	s, err := store.Open(env("BRAIN_DB", "data/brain.db"), env("BRAIN_NOTES", "data/notes"))
	if err != nil {
		fail(err.Error())
	}
	defer s.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(os.Args) < 2 {
		usage()
	}

	switch os.Args[1] {
	case "notion":
		apiKey := os.Getenv("NOTION_SECRET_KEY")
		root := os.Getenv("NOTION_ROOT_PAGE")
		if apiKey == "" || root == "" {
			fail("NOTION_SECRET_KEY and NOTION_ROOT_PAGE are required")
		}
		n := ingest.NewNotion(s, apiKey, root)
		stats, err := n.Sync(ctx)
		if err != nil {
			fail(fmt.Sprintf("notion: %v", err))
		}
		fmt.Println("notion:", stats)
	case "dropbox-sync":
		dir := env("DROPBOX_DIR", "")
		if dir == "" {
			fail("DROPBOX_DIR is required")
		}
		d := ingest.NewDropbox(s, dir)
		stats, err := d.Sync(ctx)
		if err != nil {
			fail(fmt.Sprintf("dropbox: %v", err))
		}
		fmt.Println("dropbox:", stats)
	case "dropbox-watch":
		dir := env("DROPBOX_DIR", "")
		if dir == "" {
			fail("DROPBOX_DIR is required")
		}
		d := ingest.NewDropbox(s, dir)
		fmt.Printf("watching %s (ctrl-c to stop)\n", dir)
		if err := d.Watch(ctx); err != nil {
			fail(err.Error())
		}
	default:
		usage()
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: ingest <command>

Commands:
  notion          sync the Notion page tree
  dropbox-sync    one-shot mirror of a synced Dropbox folder
  dropbox-watch   continuously mirror a synced Dropbox folder

Environment:
  BRAIN_DB         SQLite path (default data/brain.db)
  BRAIN_NOTES      markdown mirror root (default data/notes)
  NOTION_SECRET_KEY
  NOTION_ROOT_PAGE
  DROPBOX_DIR
`)
	os.Exit(1)
}

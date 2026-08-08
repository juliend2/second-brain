// Package ingest pulls content from the web sources of truth (Notion, Dropbox)
// into the store. The Notion client reuses the strategy of the legacy
// personal-search-engine crawler (depth-first search over child pages, title
// heading prefixed to the markdown), but tracks Notion's last_edited_time in
// node metadata so that unchanged pages are not re-fetched.
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"desrosiers.org/brain/internal/store"
)

const notionVersion = "2026-03-11"

// Meta keys stored on nodes.
const (
	metaURL            = "url"
	metaLastEditedTime = "last_edited_time"
)

// Notion ingests the page tree of a Notion integration.
type Notion struct {
	store    *store.Store
	apiKey   string
	rootPage string
	client   *http.Client
	interval time.Duration // politeness delay between API requests
}

// NewNotion returns a Notion ingester rooted at rootPage.
func NewNotion(s *store.Store, apiKey, rootPage string) *Notion {
	return &Notion{
		store:    s,
		apiKey:   apiKey,
		rootPage: rootPage,
		client:   &http.Client{Timeout: 30 * time.Second},
		interval: 350 * time.Millisecond,
	}
}

// Sync crawls the page tree and upserts every page into the store.
func (n *Notion) Sync(ctx context.Context) (Stats, error) {
	stats := Stats{}
	if n.rootPage == "" {
		return stats, errors.New("notion: root page id is required")
	}
	err := n.walk(ctx, n.rootPage, &stats)
	return stats, err
}

func (n *Notion) walk(ctx context.Context, pageID string, stats *Stats) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := n.process(ctx, pageID, stats); err != nil {
		return err
	}

	children, err := n.getChildPageIDs(ctx, pageID)
	if err != nil {
		return fmt.Errorf("notion: children of %s: %w", pageID, err)
	}
	for _, child := range children {
		if err := n.walk(ctx, child, stats); err != nil {
			return err
		}
	}
	return nil
}

// process upserts a single page, skipping it when last_edited_time is
// unchanged since the previous sync.
func (n *Notion) process(ctx context.Context, pageID string, stats *Stats) error {
	stats.Pages++
	page, err := n.getPage(ctx, pageID)
	if err != nil {
		stats.Failed++
		return err
	}
	if page.Archived {
		return nil
	}

	title := firstTitle(page.Properties.Title.Title)

	// Skip if the page content did not change since last sync.
	existing, err := n.store.GetNode("notion:" + pageID)
	if err == nil {
		if existing.Meta[metaLastEditedTime] == page.LastEditedTime {
			stats.Skipped++
			return nil
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		stats.Failed++
		return err
	}

	md, err := n.getMarkdown(ctx, pageID)
	if err != nil {
		stats.Failed++
		return err
	}
	if title == "" {
		title = pageID
	}

	node := &store.Node{
		ID:       "notion:" + pageID,
		Source:   "notion",
		Title:    title,
		Markdown: "# " + title + "\n\n" + md,
		Meta: map[string]string{
			metaURL:            page.URL,
			metaLastEditedTime: page.LastEditedTime,
		},
	}
	if err := n.store.PutNode(node); err != nil {
		stats.Failed++
		return err
	}

	if existing == nil {
		stats.New++
	} else {
		stats.Updated++
	}
	return nil
}

// notionPage is the subset of the Notion page object we care about.
type notionPage struct {
	ID             string `json:"id"`
	URL            string `json:"url"`
	Archived       bool   `json:"archived"`
	LastEditedTime string `json:"last_edited_time"`
	Properties     struct {
		Title struct {
			Title []struct {
				PlainText string `json:"plain_text"`
			} `json:"title"`
		} `json:"title"`
	} `json:"properties"`
}

func (n *Notion) getPage(ctx context.Context, pageID string) (*notionPage, error) {
	var p notionPage
	if err := n.getJSON(ctx, "https://api.notion.com/v1/pages/"+pageID, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// getChildPageIDs returns the child_page ids directly under a page,
// following pagination.
func (n *Notion) getChildPageIDs(ctx context.Context, parentID string) ([]string, error) {
	var ids []string
	base := "https://api.notion.com/v1/blocks/" + parentID + "/children"
	url := base
	for {
		var resp struct {
			Results []struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"results"`
			HasMore    bool   `json:"has_more"`
			NextCursor string `json:"next_cursor"`
		}
		if err := n.getJSON(ctx, url, &resp); err != nil {
			return nil, err
		}
		for _, r := range resp.Results {
			if r.Type == "child_page" {
				ids = append(ids, r.ID)
			}
		}
		if !resp.HasMore || resp.NextCursor == "" {
			return ids, nil
		}
		url = base + "?start_cursor=" + resp.NextCursor
	}
}

func (n *Notion) getMarkdown(ctx context.Context, pageID string) (string, error) {
	var resp struct {
		Markdown string `json:"markdown"`
	}
	if err := n.getJSON(ctx, "https://api.notion.com/v1/pages/"+pageID+"/markdown", &resp); err != nil {
		return "", err
	}
	return resp.Markdown, nil
}

func (n *Notion) getJSON(ctx context.Context, url string, out any) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(n.interval):
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+n.apiKey)
	req.Header.Set("Notion-Version", notionVersion)
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("notion: %s: %s", resp.Status, truncate(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("notion: decode %s: %w", url, err)
	}
	return nil
}

func firstTitle(items []struct{ PlainText string }) string {
	if len(items) == 0 {
		return ""
	}
	return items[0].PlainText
}

func truncate(b []byte) string {
	const max = 300
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max])
}

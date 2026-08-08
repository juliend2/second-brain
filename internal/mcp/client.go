package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Node mirrors the API's node representation (markdown omitted on list).
type Node struct {
	ID        string            `json:"id"`
	Source    string            `json:"source"`
	Title     string            `json:"title"`
	Markdown  string            `json:"markdown,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
	Summary   string            `json:"summary,omitempty"`
	Status    string            `json:"status"`
	Meta      map[string]string `json:"meta,omitempty"`
	CreatedAt string            `json:"created_at"`
	UpdatedAt string            `json:"updated_at"`
}

// SearchHit is a single FTS5 result.
type SearchHit struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Title   string `json:"title"`
	Summary string `json:"summary,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

// RelatedItem is a nearest neighbor with its cosine score.
type RelatedItem struct {
	ID    string  `json:"id"`
	Title string  `json:"title"`
	Score float64 `json:"score"`
}

// Client talks to the brain HTTP API. The MCP server is a thin wrapper around
// the API, so it carries no knowledge of the store itself.
type Client struct {
	base   string
	client *http.Client
}

// NewClient returns a Client pointing at the brain API base URL.
func NewClient(baseURL string) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse base URL %q: %w", baseURL, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("base URL %q must include scheme and host", baseURL)
	}
	return &Client{base: strings.TrimSuffix(u.String(), "/"), client: &http.Client{}}, nil
}

// Search runs a full-text search and returns the top hits.
func (c *Client) Search(ctx context.Context, q string, limit int) ([]SearchHit, error) {
	if limit <= 0 {
		limit = 10
	}
	qp := url.Values{"q": {q}, "limit": {strconv.Itoa(limit)}}.Encode()
	var out struct {
		Items []SearchHit `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, c.base+"/api/search?"+qp, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// GetNode fetches a single node with its full markdown.
func (c *Client) GetNode(ctx context.Context, id string) (*Node, error) {
	var n Node
	if err := c.do(ctx, http.MethodGet, c.base+"/api/nodes/"+url.PathEscape(id), nil, &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// Related returns the nearest neighbors of a node via embeddings.
func (c *Client) Related(ctx context.Context, id string, k int) ([]RelatedItem, error) {
	if k <= 0 {
		k = 5
	}
	qp := url.Values{"k": {strconv.Itoa(k)}}.Encode()
	var out struct {
		Items []RelatedItem `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, c.base+"/api/graph/related/"+url.PathEscape(id)+"?"+qp, nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// FindPath returns the shortest path between two nodes, if any.
func (c *Client) FindPath(ctx context.Context, from, to string) ([]string, error) {
	var out struct {
		Path []string `json:"path"`
	}
	u := c.base + "/api/graph/path/" + url.PathEscape(from) + "/" + url.PathEscape(to)
	if err := c.do(ctx, http.MethodGet, u, nil, &out); err != nil {
		return nil, err
	}
	return out.Path, nil
}

// CreateNode upserts a node. The id must follow the <source>:<path> convention;
// wikilinks in the markdown become graph edges immediately.
func (c *Client) CreateNode(ctx context.Context, id, title, markdown, source string) (*Node, error) {
	if source == "" {
		source = "local"
	}
	body, err := json.Marshal(map[string]string{
		"source":   source,
		"title":    title,
		"markdown": markdown,
	})
	if err != nil {
		return nil, err
	}
	var n Node
	if err := c.do(ctx, http.MethodPut, c.base+"/api/nodes/"+url.PathEscape(id), bytes.NewReader(body), &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// do performs a request and decodes the JSON response, or the {"error": ...}
// body on non-2xx statuses.
func (c *Client) do(ctx context.Context, method, url string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			return fmt.Errorf("api %s %s: %s", method, url, resp.Status)
		}
		return fmt.Errorf("api %s %s: %s", method, url, e.Error)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

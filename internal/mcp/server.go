// Package mcp exposes the brain repository to LLM clients over the Model
// Context Protocol. It is a thin wrapper around the HTTP API (internal/server):
// each tool maps to one API call, so the MCP server shares a single data path
// with the PWA and never touches the store directly.
package mcp

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// New builds an MCP server exposing the brain's tools. It is transport-agnostic:
// cmd/mcp serves it over stdio; tests drive it over an in-memory transport.
func New(c *Client) *mcp.Server {
	srv := mcp.NewServer(&mcp.Implementation{
		Name:        "brain",
		Title:       "Second brain",
		Description: "Read-only and write access to a personal knowledge repository: full-text search, node retrieval, embedding-based related notes, graph paths, and note creation.",
		Version:     "0.1.0",
	}, &mcp.ServerOptions{
		Instructions: "Node ids follow the <source>:<path> convention (e.g. notion:4f3a…, dropbox:notes/foo.md) and must be passed verbatim.",
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_corpus",
		Description: "Full-text search over all notes. Returns the top hits with a snippet and the node id, title, and summary when present.",
	}, searchHandler(c))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_node",
		Description: "Fetch one note by its exact id (e.g. notion:4f3a…). Returns the full markdown plus metadata.",
	}, getNodeHandler(c))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "related",
		Description: "Find notes similar to a given note using embeddings. Returns nearest neighbors with a similarity score — useful to discover connections the note does not explicitly link.",
	}, relatedHandler(c))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "find_path",
		Description: "Shortest path between two notes in the link graph, if one exists. Empty path means they are not connected.",
	}, pathHandler(c))

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "create_note",
		Description: "Create or overwrite a note. The id must follow the <source>:<path> convention; source defaults to \"mcp\". [[wikilinks]] in the markdown immediately become graph edges.",
	}, createNoteHandler(c))

	return srv
}

type searchInput struct {
	Query string `json:"query" jsonschema:"full-text search query"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum number of hits (default 10)"`
}

type searchOutput struct {
	Items []SearchHit `json:"items" jsonschema:"matching notes, most relevant first"`
}

func searchHandler(c *Client) func(context.Context, *mcp.CallToolRequest, searchInput) (*mcp.CallToolResult, searchOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, searchOutput, error) {
		if strings.TrimSpace(in.Query) == "" {
			return nil, searchOutput{}, errors.New("query is required")
		}
		hits, err := c.Search(ctx, in.Query, in.Limit)
		if err != nil {
			return nil, searchOutput{}, err
		}
		return nil, searchOutput{Items: hits}, nil
	}
}

type nodeInput struct {
	ID string `json:"id" jsonschema:"exact node id, e.g. notion:4f3a or dropbox:notes/foo.md"`
}

type nodeOutput struct {
	ID        string   `json:"id" jsonschema:"node id"`
	Title     string   `json:"title" jsonschema:"note title"`
	Source    string   `json:"source" jsonschema:"source system"`
	Status    string   `json:"status" jsonschema:"draft or enriched"`
	Summary   string   `json:"summary,omitempty" jsonschema:"one-line LLM summary if enriched"`
	Tags      []string `json:"tags,omitempty" jsonschema:"LLM-assigned tags"`
	Markdown  string   `json:"markdown,omitempty" jsonschema:"full note content"`
	CreatedAt string   `json:"created_at,omitempty" jsonschema:"creation timestamp"`
	UpdatedAt string   `json:"updated_at,omitempty" jsonschema:"last update timestamp"`
}

func getNodeHandler(c *Client) func(context.Context, *mcp.CallToolRequest, nodeInput) (*mcp.CallToolResult, nodeOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in nodeInput) (*mcp.CallToolResult, nodeOutput, error) {
		if strings.TrimSpace(in.ID) == "" {
			return nil, nodeOutput{}, errors.New("id is required")
		}
		n, err := c.GetNode(ctx, in.ID)
		if err != nil {
			return nil, nodeOutput{}, err
		}
		return nil, nodeOutput{
			ID: n.ID, Title: n.Title, Source: n.Source, Status: n.Status,
			Summary: n.Summary, Tags: n.Tags, Markdown: n.Markdown,
			CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt,
		}, nil
	}
}

type relatedInput struct {
	ID string `json:"id" jsonschema:"exact node id to find similar notes for"`
	K  int    `json:"k,omitempty" jsonschema:"number of neighbors (default 5)"`
}

type relatedOutput struct {
	Items []RelatedItem `json:"items" jsonschema:"nearest neighbors, highest similarity first"`
}

func relatedHandler(c *Client) func(context.Context, *mcp.CallToolRequest, relatedInput) (*mcp.CallToolResult, relatedOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in relatedInput) (*mcp.CallToolResult, relatedOutput, error) {
		if strings.TrimSpace(in.ID) == "" {
			return nil, relatedOutput{}, errors.New("id is required")
		}
		items, err := c.Related(ctx, in.ID, in.K)
		if err != nil {
			return nil, relatedOutput{}, err
		}
		return nil, relatedOutput{Items: items}, nil
	}
}

type pathInput struct {
	From string `json:"from" jsonschema:"start node id"`
	To   string `json:"to" jsonschema:"end node id"`
}

type pathOutput struct {
	Path []string `json:"path" jsonschema:"node ids from start to end; empty when disconnected"`
}

func pathHandler(c *Client) func(context.Context, *mcp.CallToolRequest, pathInput) (*mcp.CallToolResult, pathOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in pathInput) (*mcp.CallToolResult, pathOutput, error) {
		if strings.TrimSpace(in.From) == "" || strings.TrimSpace(in.To) == "" {
			return nil, pathOutput{}, errors.New("from and to are required")
		}
		path, err := c.FindPath(ctx, in.From, in.To)
		if err != nil {
			return nil, pathOutput{}, err
		}
		return nil, pathOutput{Path: path}, nil
	}
}

type createNoteInput struct {
	ID       string `json:"id" jsonschema:"node id following the <source>:<path> convention, e.g. mcp:ideas/foo"`
	Title    string `json:"title" jsonschema:"note title"`
	Markdown string `json:"markdown" jsonschema:"note content in markdown; [[wikilinks]] become graph edges"`
	Source   string `json:"source,omitempty" jsonschema:"source prefix (default mcp)"`
}

func createNoteHandler(c *Client) func(context.Context, *mcp.CallToolRequest, createNoteInput) (*mcp.CallToolResult, nodeOutput, error) {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in createNoteInput) (*mcp.CallToolResult, nodeOutput, error) {
		if strings.TrimSpace(in.ID) == "" {
			return nil, nodeOutput{}, errors.New("id is required")
		}
		if strings.TrimSpace(in.Markdown) == "" {
			return nil, nodeOutput{}, errors.New("markdown is required")
		}
		source := in.Source
		if source == "" {
			source = "mcp"
		}
		n, err := c.CreateNode(ctx, in.ID, in.Title, in.Markdown, source)
		if err != nil {
			return nil, nodeOutput{}, err
		}
		return nil, nodeOutput{
			ID: n.ID, Title: n.Title, Source: n.Source, Status: n.Status,
			Markdown: n.Markdown, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt,
		}, nil
	}
}

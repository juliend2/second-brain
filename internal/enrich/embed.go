// Package enrich adds a semantic layer over the store: local embeddings via
// Ollama (used for retrieval/related) and a cloud LLM pass that tags, links
// and summarizes each node ("hybrid" as decided: embeddings stay on the
// machine, only the semantic enrichment leaves it).
package enrich

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OllamaEmbedder embeds text into vectors via Ollama's /api/embed endpoint.
type OllamaEmbedder struct {
	baseURL string
	model   string
	client  *http.Client
}

// NewOllamaEmbedder returns an Ollama-backed embedder.
func NewOllamaEmbedder(baseURL, model string) *OllamaEmbedder {
	return &OllamaEmbedder{
		baseURL: baseURL,
		model:   model,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// Embed returns the embedding vector for text.
func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody, err := json.Marshal(map[string]any{
		"model": e.model,
		"input": text,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/embed", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed: %s: %s", resp.Status, truncate(body))
	}

	var out struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("embed: decode: %w", err)
	}
	if len(out.Embeddings) == 0 {
		return nil, fmt.Errorf("embed: no embeddings in response")
	}
	return out.Embeddings[0], nil
}

func truncate(b []byte) string {
	const max = 200
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max])
}

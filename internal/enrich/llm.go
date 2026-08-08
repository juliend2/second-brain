package enrich

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAILLM is a cloud OpenAILLM client used for the semantic enrichment pass. It speaks
// the OpenAI-compatible chat completions API, which most providers expose.
type OpenAILLM struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

// NewOpenAILLM returns a client for an OpenAI-compatible endpoint.
func NewOpenAILLM(apiKey, baseURL, model string) *OpenAILLM {
	return &OpenAILLM{
		apiKey:  apiKey,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		model:   model,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

// CompleteJSON asks the model to produce a JSON object and decodes it into
// out. The content is cleaned of stray code fences before decoding.
func (l *OpenAILLM) CompleteJSON(ctx context.Context, system, user string, out any) error {
	reqBody, err := json.Marshal(map[string]any{
		"model": l.model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature":     0,
		"response_format": map[string]string{"type": "json_object"},
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if l.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+l.apiKey)
	}

	resp, err := l.client.Do(req)
	if err != nil {
		return fmt.Errorf("llm: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("llm: %s: %s", resp.Status, truncate(body))
	}

	var c struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &c); err != nil {
		return fmt.Errorf("llm: decode response: %w", err)
	}
	if len(c.Choices) == 0 {
		return fmt.Errorf("llm: no choices in response")
	}

	content := strings.TrimSpace(c.Choices[0].Message.Content)
	content = stripFences(content)
	if err := json.Unmarshal([]byte(content), out); err != nil {
		return fmt.Errorf("llm: decode JSON content: %w", err)
	}
	return nil
}

// stripFences removes ```json ... ``` wrappers some providers add.
func stripFences(s string) string {
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}

package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Default Ollama endpoint (its OpenAI-compatible API) and model.
const (
	DefaultOllamaURL   = "http://localhost:11434/v1"
	DefaultOllamaModel = "qwen3:latest"
)

// OpenAICompatible is an adapter for any backend speaking the OpenAI
// /chat/completions streaming API — Ollama, OpenAI, OpenRouter, …
type OpenAICompatible struct {
	name    string
	baseURL string
	apiKey  string
	model   string
	client  *http.Client
}

// NewOpenAICompatible builds an adapter. apiKey may be empty (Ollama ignores it).
func NewOpenAICompatible(name, baseURL, apiKey, model string) *OpenAICompatible {
	return &OpenAICompatible{
		name:    name,
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		model:   model,
		// No client-level timeout: streaming runs are open-ended; cancellation
		// is driven by the caller's context instead.
		client: &http.Client{},
	}
}

// NewOllama builds an adapter for a local Ollama server. An empty model falls
// back to DefaultOllamaModel.
func NewOllama(model string) *OpenAICompatible {
	if model == "" {
		model = DefaultOllamaModel
	}
	return NewOpenAICompatible("ollama", DefaultOllamaURL, "", model)
}

// Name implements Provider.
func (p *OpenAICompatible) Name() string { return p.name }

// Model returns the provider's default model.
func (p *OpenAICompatible) Model() string { return p.model }

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

// streamResponse is one SSE `data:` payload from /chat/completions.
type streamResponse struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			Reasoning string `json:"reasoning"` // thinking models (e.g. qwen3).
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Chat implements Provider.
func (p *OpenAICompatible) Chat(ctx context.Context, msgs []Message, opts Options) (<-chan Chunk, error) {
	model := opts.Model
	if model == "" {
		model = p.model
	}
	body, err := json.Marshal(chatRequest{
		Model:       model,
		Messages:    msgs,
		Stream:      true,
		Temperature: opts.Temperature,
		MaxTokens:   opts.MaxTokens,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: request failed: %w", p.name, err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("%s: unexpected status %s: %s",
			p.name, resp.Status, strings.TrimSpace(string(snippet)))
	}

	out := make(chan Chunk)
	go p.stream(ctx, resp.Body, out)
	return out, nil
}

// stream parses the SSE body and forwards chunks until [DONE], EOF, an error,
// or context cancellation. It owns closing body and out.
func (p *OpenAICompatible) stream(ctx context.Context, body io.ReadCloser, out chan<- Chunk) {
	defer body.Close()
	defer close(out)

	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024) // tolerate long SSE lines.

	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue // skip blank lines and any comments.
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		switch data {
		case "":
			continue
		case "[DONE]":
			return
		}

		var sr streamResponse
		if err := json.Unmarshal([]byte(data), &sr); err != nil {
			p.send(ctx, out, Chunk{Err: fmt.Errorf("%s: bad stream chunk: %w", p.name, err)})
			return
		}
		if sr.Error != nil {
			p.send(ctx, out, Chunk{Err: fmt.Errorf("%s: %s", p.name, sr.Error.Message)})
			return
		}
		if len(sr.Choices) == 0 {
			continue
		}
		d := sr.Choices[0].Delta
		if d.Content == "" && d.Reasoning == "" {
			continue
		}
		if !p.send(ctx, out, Chunk{Content: d.Content, Reasoning: d.Reasoning}) {
			return // context cancelled.
		}
	}
	if err := sc.Err(); err != nil {
		p.send(ctx, out, Chunk{Err: fmt.Errorf("%s: stream read: %w", p.name, err)})
	}
}

// send delivers c unless the context is cancelled first; returns false if it was.
func (p *OpenAICompatible) send(ctx context.Context, out chan<- Chunk, c Chunk) bool {
	select {
	case out <- c:
		return true
	case <-ctx.Done():
		return false
	}
}

// Package llm defines Talunor's LLM provider abstraction and its adapters.
//
// The interface is deliberately tiny: a provider streams a chat completion as a
// channel of Chunks. Most providers Talunor targets (Ollama, OpenAI, OpenRouter)
// speak the OpenAI-compatible API, so a single adapter (OpenAICompatible) serves
// all three; Anthropic will get its own adapter later.
package llm

import (
	"context"
	"strings"
)

// Message roles.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Message is a single chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Options tunes a single chat call. Zero values mean "use the provider default".
type Options struct {
	Model       string  // overrides the provider's default model when set.
	Temperature float64 // 0 → provider default.
	MaxTokens   int     // 0 → provider default.
}

// Chunk is one streamed piece of a completion. Thinking models (e.g. qwen3 via
// Ollama) emit their chain-of-thought in Reasoning and the final answer in
// Content; either may be empty on a given chunk. A non-nil Err is terminal: it
// is the last Chunk on the channel.
type Chunk struct {
	Content   string
	Reasoning string
	Err       error
}

// Provider is an LLM backend that streams chat completions.
type Provider interface {
	// Name identifies the provider (e.g. "ollama") for logs and errors.
	Name() string
	// Chat starts a streaming completion. Setup failures (bad request,
	// connection refused, non-200) are returned as the error; failures mid-
	// stream arrive as a Chunk with Err set. The channel is closed when the
	// completion ends or the context is cancelled.
	Chat(ctx context.Context, msgs []Message, opts Options) (<-chan Chunk, error)
}

// Collect drains a Chat stream into the full answer text (Content only),
// returning the first error encountered. Convenience for non-streaming callers
// and tests.
func Collect(ctx context.Context, p Provider, msgs []Message, opts Options) (string, error) {
	ch, err := p.Chat(ctx, msgs, opts)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			return sb.String(), chunk.Err
		}
		sb.WriteString(chunk.Content)
	}
	return sb.String(), nil
}

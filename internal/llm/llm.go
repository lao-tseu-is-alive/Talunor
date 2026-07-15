// Package llm defines Talunor's LLM provider abstraction and its adapters.
//
// The interface is deliberately tiny: a provider streams a chat completion as a
// channel of Chunks. Most providers Talunor targets (Ollama, OpenAI, OpenRouter)
// speak the OpenAI-compatible API, so a single adapter (OpenAICompatible) serves
// all three; Anthropic will get its own adapter later.
package llm

import (
	"context"
	"encoding/json"
	"strings"
)

// Message roles.
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool" // a tool's result, observed by the model.
)

// Message is a single chat message. For tool use: an assistant message may carry
// ToolCalls (the model asking to run tools), and a RoleTool message carries a
// tool's result via ToolCallID + Content.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolSpec is a tool offered to the model: a name, a description, and the JSON
// Schema of its arguments. The adapter wraps it in the OpenAI "function" shape.
type ToolSpec struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ToolCall is the model's request to invoke one tool, with JSON-encoded
// arguments. It marshals to the OpenAI wire shape ({id,type,function:{…}}) so it
// can be echoed back verbatim in the follow-up assistant message.
type ToolCall struct {
	ID   string
	Name string
	Args string // raw JSON arguments.
}

// MarshalJSON renders a ToolCall in OpenAI's nested function-call format.
func (tc ToolCall) MarshalJSON() ([]byte, error) {
	type fn struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	return json.Marshal(struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function fn     `json:"function"`
	}{ID: tc.ID, Type: "function", Function: fn{Name: tc.Name, Arguments: tc.Args}})
}

// Options tunes a single chat call. Zero values mean "use the provider default".
type Options struct {
	Model       string     // overrides the provider's default model when set.
	Temperature float64    // 0 → provider default.
	MaxTokens   int        // 0 → provider default.
	Tools       []ToolSpec // tools offered to the model (empty → none).
}

// Chunk is one streamed piece of a completion. Thinking models (e.g. qwen3 via
// Ollama) emit their chain-of-thought in Reasoning and the final answer in
// Content; either may be empty on a given chunk. When the model finishes by
// requesting tools, the terminal chunk carries the assembled ToolCalls. A
// non-nil Err is terminal: it is the last Chunk on the channel.
type Chunk struct {
	Content   string
	Reasoning string
	ToolCalls []ToolCall
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

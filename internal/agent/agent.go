// Package agent implements Talunor's cognitive loop. A single Turn ties the
// three substrates together:
//
//	Perceive : take the user's input.
//	Recall   : fetch relevant long-term memories (KNN, thresholded) and the
//	           recent short-term turns.
//	Reason   : build a prompt (system + memories + recent turns + input) and
//	           stream a completion from the LLM provider.
//	Store    : remember the user turn and the assistant turn (short-term ring
//	           + long-term store) so the next turn can recall them.
//
// This is the first layer that remembers across turns.
package agent

import (
	"context"
	"strings"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
)

// Config tunes an Agent.
type Config struct {
	// SystemPrompt frames every conversation.
	SystemPrompt string
	// RecallK is the maximum number of long-term memories to retrieve per turn.
	RecallK int
	// RecallMaxDistance drops memories whose cosine distance exceeds it, so only
	// relevant ones are injected (0 keeps all k).
	RecallMaxDistance float64
	// ShortTermCap is the number of recent turns kept verbatim as immediate
	// context.
	ShortTermCap int
	// Options is passed through to the provider on every call.
	Options llm.Options
}

// DefaultConfig returns sensible defaults for a conversational agent.
func DefaultConfig() Config {
	return Config{
		SystemPrompt: "You are Talunor, a helpful assistant with long-term memory. " +
			"When the provided memories are relevant, use them to answer; " +
			"otherwise ignore them and answer normally. Do not mention the memory system unless asked.",
		RecallK:           5,
		RecallMaxDistance: 0.75,
		ShortTermCap:      6, // ~3 exchanges.
	}
}

// Agent owns the memory substrates and the LLM provider and runs the loop.
type Agent struct {
	store    *memory.Store
	short    *memory.ShortTerm
	provider llm.Provider
	cfg      Config
}

// New builds an Agent. Zero-valued config fields fall back to DefaultConfig.
func New(store *memory.Store, provider llm.Provider, cfg Config) *Agent {
	def := DefaultConfig()
	if cfg.SystemPrompt == "" {
		cfg.SystemPrompt = def.SystemPrompt
	}
	if cfg.RecallK <= 0 {
		cfg.RecallK = def.RecallK
	}
	if cfg.ShortTermCap <= 0 {
		cfg.ShortTermCap = def.ShortTermCap
	}
	return &Agent{
		store:    store,
		short:    memory.NewShortTerm(cfg.ShortTermCap),
		provider: provider,
		cfg:      cfg,
	}
}

// Turn runs one cognitive turn for input and returns a stream of the assistant's
// reply. The user turn is recorded immediately; the assistant turn is recorded
// once the stream completes successfully (a failed or cancelled stream is not
// stored). Callers must drain the returned channel.
func (a *Agent) Turn(ctx context.Context, input string) (<-chan llm.Chunk, error) {
	// Recall against the input *before* storing it, so the current message is
	// not retrieved as its own top match.
	hits, err := a.store.Recall(ctx, input, a.cfg.RecallK, a.cfg.RecallMaxDistance)
	if err != nil {
		return nil, err
	}

	// Reason: build the prompt from prior context, then start streaming.
	msgs := a.buildMessages(hits, input)

	// Store the user turn now (it happened regardless of how the reply goes).
	a.short.Add(llm.RoleUser, input)
	if _, err := a.store.Remember(ctx, memory.KindTurn, llm.RoleUser, input); err != nil {
		return nil, err
	}

	stream, err := a.provider.Chat(ctx, msgs, a.cfg.Options)
	if err != nil {
		return nil, err
	}

	// Tee the stream to the caller while accumulating the answer; store it on
	// clean completion.
	out := make(chan llm.Chunk)
	go a.learnWhileStreaming(ctx, stream, out)
	return out, nil
}

// learnWhileStreaming forwards chunks to out, accumulates the answer content,
// and on successful completion records the assistant turn.
func (a *Agent) learnWhileStreaming(ctx context.Context, in <-chan llm.Chunk, out chan<- llm.Chunk) {
	defer close(out)
	var answer strings.Builder
	for c := range in {
		if c.Err != nil {
			// Forward the error; do not store a failed/partial answer.
			select {
			case out <- c:
			case <-ctx.Done():
			}
			return
		}
		answer.WriteString(c.Content)
		select {
		case out <- c:
		case <-ctx.Done():
			return
		}
	}
	// Learn: the stream finished cleanly.
	if text := answer.String(); text != "" {
		a.short.Add(llm.RoleAssistant, text)
		// Best-effort: a storage failure must not corrupt the finished reply the
		// caller already received.
		_, _ = a.store.Remember(ctx, memory.KindTurn, llm.RoleAssistant, text)
	}
}

// buildMessages assembles the prompt: system prompt, an optional block of
// recalled memories, the recent short-term turns, then the new user input.
func (a *Agent) buildMessages(hits []memory.Hit, input string) []llm.Message {
	msgs := []llm.Message{{Role: llm.RoleSystem, Content: a.cfg.SystemPrompt}}

	if len(hits) > 0 {
		var b strings.Builder
		b.WriteString("Relevant memories retrieved for this message:\n")
		for _, h := range hits {
			b.WriteString("- ")
			b.WriteString(h.Content)
			b.WriteByte('\n')
		}
		msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: b.String()})
	}

	for _, t := range a.short.Recent() {
		msgs = append(msgs, llm.Message{Role: t.Role, Content: t.Content})
	}

	msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: input})
	return msgs
}

// ShortTermLen reports how many turns are currently in immediate context.
func (a *Agent) ShortTermLen() int { return a.short.Len() }

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
	"fmt"
	"strconv"
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
		RecallK:           8,
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

// MemoryCount reports how many long-term memories are stored.
func (a *Agent) MemoryCount(ctx context.Context) (int, error) { return a.store.Count(ctx) }

// HelpText lists the slash commands understood by both the TUI and the REPL.
const HelpText = `Commands:
  /help        show this help
  /mem         memory stats (count + database file)
  /list [n]    list the most recent n memories (default 10)
  /forget <id> delete the memory with that #id (as shown by /list)
  /clear       clear the on-screen transcript (TUI only; does not erase memory)
  /exit, /quit quit
Keys (TUI): enter = send · ctrl+c / esc = quit · ↑/↓ or PgUp/PgDn = scroll
(Mouse selection works: click-drag to select and copy text.)`

// Help returns the command help text.
func (a *Agent) Help() string { return HelpText }

// MemoryStats returns a one-line summary of stored memory and where it lives.
func (a *Agent) MemoryStats(ctx context.Context) (string, error) {
	n, err := a.store.Count(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d memories stored in %s", n, a.store.Path()), nil
}

// ListMemories returns a formatted listing of the most recent n memories.
func (a *Agent) ListMemories(ctx context.Context, n int) (string, error) {
	mems, err := a.store.List(ctx, n)
	if err != nil {
		return "", err
	}
	return FormatMemories(mems), nil
}

// MemoryID parses the id argument of a slash command whose fields have been
// split on whitespace (e.g. "/forget 7" → 7). It reports ok=false when the id
// is missing or not a valid integer, so callers can show usage help.
func MemoryID(fields []string) (id int64, ok bool) {
	if len(fields) < 2 {
		return 0, false
	}
	id, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// ForgetMemory deletes the long-term memory with the given id (the #id shown by
// ListMemories) and returns a one-line, display-ready result. Forgetting a
// long-term memory does not alter the current session's short-term context.
func (a *Agent) ForgetMemory(ctx context.Context, id int64) (string, error) {
	ok, err := a.store.Forget(ctx, id)
	if err != nil {
		return "", err
	}
	if !ok {
		return fmt.Sprintf("no memory #%d to forget", id), nil
	}
	return fmt.Sprintf("forgot memory #%d", id), nil
}

// FormatMemories renders memories (newest first) as a compact, readable list.
func FormatMemories(mems []memory.Memory) string {
	if len(mems) == 0 {
		return "(no memories yet)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Most recent %d memories (newest first):\n", len(mems))
	for _, m := range mems {
		label := m.Role
		if label == "" {
			label = string(m.Kind)
		}
		fmt.Fprintf(&b, "  #%d [%s] %s  %s\n",
			m.ID, label, m.CreatedAt.Format("2006-01-02 15:04"), oneLine(m.Content, 70))
	}
	return b.String()
}

// oneLine collapses whitespace and truncates s to at most max runes.
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

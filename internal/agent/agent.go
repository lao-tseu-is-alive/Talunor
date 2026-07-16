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
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
	"github.com/lao-tseu-is-alive/Talunor/internal/tools"
)

// Config tunes an Agent.
type Config struct {
	// SystemPrompt frames every conversation.
	SystemPrompt string
	// RecallK is the maximum number of long-term memories to retrieve per turn.
	RecallK int
	// RecallMaxDistance drops memories whose cosine distance exceeds it, so only
	// relevant ones are injected. 0 is a meaningful value — it keeps all k
	// matches (no thresholding) — so, unlike the other numeric fields, New does
	// *not* substitute DefaultConfig's value for a zero here. Set it explicitly
	// (DefaultConfig uses 0.75) to enable thresholding.
	RecallMaxDistance float64
	// ShortTermCap is the number of recent turns kept verbatim as immediate
	// context.
	ShortTermCap int
	// Options is passed through to the provider on every call.
	Options llm.Options

	// Extractor is the reflection step: after each turn it distils durable facts
	// from the user's message into semantic memory (memory.KindFact). If nil, New
	// installs a default LLM-based extractor over the agent's own provider; inject
	// DisableReflection() to turn reflection off.
	Extractor FactExtractor
	// DedupMaxDistance suppresses storing a freshly-extracted fact when an
	// existing fact lies within this cosine distance, so restating something does
	// not pile up near-duplicate facts. Small = "only skip near-identical facts".
	DedupMaxDistance float64

	// Tools, when set, are offered to the model each turn; the agent runs an
	// act→observe loop, executing any tool calls and feeding results back until
	// the model answers. Nil = a plain conversational turn (no tools).
	Tools *tools.Registry
	// MaxToolIters caps the act/observe rounds per turn, so a confused model can't
	// loop forever. Defaults to 6.
	MaxToolIters int

	// Debug, when non-nil, receives a structured trace of the loop's otherwise
	// invisible decisions: which memories were recalled (id + distance), which
	// tools ran, and what reflection stored or skipped. It is a teaching/debug
	// aid, off by default; cmd/talunor wires it from TALUNOR_DEBUG. The trace may
	// include snippets of recalled memory content, so it is opt-in and local.
	Debug *slog.Logger
}

// DefaultConfig returns sensible defaults for a conversational agent.
func DefaultConfig() Config {
	return Config{
		SystemPrompt: "You are Talunor, a helpful assistant with long-term memory. " +
			"When the provided memories are relevant, use them to answer; " +
			"otherwise ignore them and answer normally. Do not mention the memory system unless asked.",
		RecallK:           8,
		RecallMaxDistance: 0.75,
		ShortTermCap:      6,    // ~3 exchanges.
		DedupMaxDistance:  0.20, // near-identical facts only.
		MaxToolIters:      6,
	}
}

// Agent owns the memory substrates and the LLM provider and runs the loop.
type Agent struct {
	store     *memory.Store
	short     *memory.ShortTerm
	provider  llm.Provider
	extractor FactExtractor
	tools     *tools.Registry
	cfg       Config
}

// New builds an Agent. Zero-valued config fields fall back to DefaultConfig,
// with one deliberate exception: RecallMaxDistance is left as-is because 0 is a
// meaningful value there (keep all k matches — see its field doc). Callers that
// want thresholding must set it (DefaultConfig uses 0.75); cmd/talunor does.
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
	if cfg.DedupMaxDistance <= 0 {
		cfg.DedupMaxDistance = def.DedupMaxDistance
	}
	if cfg.MaxToolIters <= 0 {
		cfg.MaxToolIters = def.MaxToolIters
	}
	// Default reflection: the agent uses its own LLM provider to write its
	// semantic memory. Callers disable it with DisableReflection().
	if cfg.Extractor == nil {
		cfg.Extractor = newLLMExtractor(provider, cfg.Options)
	}
	return &Agent{
		store:     store,
		short:     memory.NewShortTerm(cfg.ShortTermCap),
		provider:  provider,
		extractor: cfg.Extractor,
		tools:     cfg.Tools,
		cfg:       cfg,
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
	a.traceRecall(input, hits)

	// Reason: build the prompt from prior context.
	msgs := a.buildMessages(hits, input)

	// Store the user turn now (it happened regardless of how the reply goes).
	a.short.Add(llm.RoleUser, input)
	if _, err := a.store.Remember(ctx, memory.KindTurn, llm.RoleUser, input); err != nil {
		return nil, err
	}

	// Run the act→observe loop in the background, streaming to the caller.
	out := make(chan llm.Chunk)
	go a.runLoop(ctx, msgs, input, out)
	return out, nil
}

// runLoop is the cognitive loop's reasoning+acting core. It calls the model with
// the offered tools; while the model asks for tools it executes them, feeds the
// observations back, and calls again (up to MaxToolIters); once the model answers
// without a tool call, that answer is the final reply. Answer content streams to
// the caller live; tool activity is surfaced as dimmed notes. On clean
// completion the final answer is stored and reflection runs — all before the
// channel closes, so observing the stream end means learning is done.
func (a *Agent) runLoop(ctx context.Context, msgs []llm.Message, input string, out chan<- llm.Chunk) {
	defer close(out)

	opts := a.cfg.Options
	if a.tools != nil {
		opts.Tools = a.toolSpecs()
	}

	var answer string
	answered := false
	for iter := 0; iter <= a.cfg.MaxToolIters; iter++ {
		stream, err := a.provider.Chat(ctx, msgs, opts)
		if err != nil {
			a.send(ctx, out, llm.Chunk{Err: err})
			return
		}

		var content strings.Builder
		var calls []llm.ToolCall
		for c := range stream {
			if c.Err != nil {
				a.send(ctx, out, c) // forward the error; store nothing.
				return
			}
			if len(c.ToolCalls) > 0 {
				calls = c.ToolCalls // terminal tool-call chunk; not user-facing.
				continue
			}
			content.WriteString(c.Content)
			if !a.send(ctx, out, c) {
				return // context cancelled.
			}
		}

		if len(calls) == 0 {
			answer = content.String() // the model answered; we're done.
			answered = true
			break
		}

		// Budget exhausted: the model still wants tools but we won't call it
		// again, so running these tools would waste work whose observations are
		// never seen. Stop and report below instead of ending the turn silently.
		if iter == a.cfg.MaxToolIters {
			break
		}

		// Act: echo the assistant's tool-call message, run each tool, and append
		// its observation for the next round.
		msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, ToolCalls: calls})
		for _, tc := range calls {
			if !a.send(ctx, out, llm.Chunk{Reasoning: fmt.Sprintf("🔧 %s(%s)\n", tc.Name, oneLine(tc.Args, 80))}) {
				return
			}
			a.trace("tool.call", "iter", iter, "name", tc.Name, "args", oneLine(tc.Args, 80))
			obs, done := a.runTool(ctx, out, tc)
			if done {
				return // context cancelled mid-tool.
			}
			a.trace("tool.result", "name", tc.Name, "result", oneLine(obs, 120))
			if !a.send(ctx, out, llm.Chunk{Reasoning: fmt.Sprintf("   ↳ %s\n", oneLine(obs, 120))}) {
				return
			}
			msgs = append(msgs, llm.Message{Role: llm.RoleTool, ToolCallID: tc.ID, Content: obs})
		}
	}

	// If the model never produced a final answer (it kept asking for tools until
	// the cap), don't end the turn silently: surface a clear error so the user
	// and the transcript both know the turn did not converge. Nothing is stored
	// as an assistant turn, and reflection is skipped (the turn failed).
	if !answered {
		a.trace("tool.loop.exhausted", "maxIters", a.cfg.MaxToolIters)
		a.send(ctx, out, llm.Chunk{Err: fmt.Errorf(
			"the model kept requesting tools without answering after %d tool rounds; giving up on this turn",
			a.cfg.MaxToolIters)})
		return
	}

	// Learn: record the assistant turn and reflect on the user's message.
	if answer != "" {
		a.short.Add(llm.RoleAssistant, answer)
		_, _ = a.store.Remember(ctx, memory.KindTurn, llm.RoleAssistant, answer)
	}
	a.reflect(ctx, input)
}

// runTool runs one tool call, first asking the human for approval if the tool
// requires it (a denied or cancelled request becomes an observation the model
// can react to). It returns the observation and done=true if the context was
// cancelled while waiting for approval (the caller should stop).
func (a *Agent) runTool(ctx context.Context, out chan<- llm.Chunk, tc llm.ToolCall) (obs string, done bool) {
	if a.needsApproval(tc.Name, tc.Args) {
		req := llm.NewApprovalRequest(tc.Name, tc.Args)
		if !a.send(ctx, out, llm.Chunk{Approval: req}) {
			return "", true
		}
		if !req.Decision(ctx) {
			if ctx.Err() != nil {
				return "", true
			}
			return "error: the user denied permission to run this tool", false
		}
	}
	return a.tools.Execute(ctx, tc.Name, json.RawMessage(tc.Args)), false
}

// needsApproval reports whether calling the named tool with these arguments
// requires human approval. A tool implementing tools.ApprovableFor decides per
// call from its arguments (e.g. web_fetch waving through an allowlisted host);
// otherwise the coarse tools.Approvable applies; tools implementing neither run
// freely.
func (a *Agent) needsApproval(name, args string) bool {
	t, ok := a.tools.Get(name)
	if !ok {
		return false
	}
	if af, ok := t.(tools.ApprovableFor); ok {
		return af.RequiresApprovalForArgs(json.RawMessage(args))
	}
	ap, ok := t.(tools.Approvable)
	return ok && ap.RequiresApproval()
}

// send delivers c unless the context is cancelled first; returns false if it was.
func (a *Agent) send(ctx context.Context, out chan<- llm.Chunk, c llm.Chunk) bool {
	select {
	case out <- c:
		return true
	case <-ctx.Done():
		return false
	}
}

// toolSpecs converts the registry's definitions into the provider's tool specs.
func (a *Agent) toolSpecs() []llm.ToolSpec {
	defs := a.tools.Defs()
	specs := make([]llm.ToolSpec, len(defs))
	for i, d := range defs {
		specs[i] = llm.ToolSpec{Name: d.Name, Description: d.Description, Parameters: d.Parameters}
	}
	return specs
}

// reflect is the agent's learning step: it asks the extractor for durable facts
// in the user's message and stores each new one as semantic memory
// (memory.KindFact). It is best-effort — an extraction or storage failure must
// never disturb the reply the caller already received — and it deduplicates
// against existing facts so restating something does not accumulate copies.
func (a *Agent) reflect(ctx context.Context, input string) {
	if a.extractor == nil {
		return
	}
	facts, err := a.extractor.Extract(ctx, input)
	if err != nil {
		// Reflection is best-effort (see runLoop), but a debug trace explains a
		// later "why didn't it remember that?" without changing behaviour.
		a.trace("reflect.error", "err", err)
		return
	}
	stored, skipped := 0, 0
	for _, f := range facts {
		if a.factKnown(ctx, f) {
			skipped++
			continue
		}
		if _, err := a.store.Remember(ctx, memory.KindFact, "", f); err == nil {
			stored++
		}
	}
	a.trace("reflect", "extracted", len(facts), "stored", stored, "skipped", skipped)
}

// trace emits a structured debug event when Config.Debug is set; it is a no-op
// otherwise, so instrumentation call sites stay unconditional and cheap.
func (a *Agent) trace(msg string, args ...any) {
	if a.cfg.Debug != nil {
		a.cfg.Debug.Debug(msg, args...)
	}
}

// traceRecall logs the recall decision — how many memories matched and, per hit,
// its id, cosine distance, and kind (plus a short content snippet to make the
// trace readable). Nothing is logged when debug is off.
func (a *Agent) traceRecall(input string, hits []memory.Hit) {
	if a.cfg.Debug == nil {
		return
	}
	a.trace("recall",
		"query", oneLine(input, 60),
		"k", a.cfg.RecallK,
		"maxDistance", a.cfg.RecallMaxDistance,
		"hits", len(hits))
	for _, h := range hits {
		a.trace("recall.hit",
			"id", h.ID,
			"distance", h.Distance,
			"kind", string(h.Kind),
			"snippet", oneLine(h.Content, 60))
	}
}

// factKnown reports whether a fact semantically equivalent to the given one is
// already stored, so reflect can skip near-duplicates. Only existing KindFact
// rows count: a raw conversation turn that happens to sit nearby must not block
// the *first* distillation of that turn into a fact.
func (a *Agent) factKnown(ctx context.Context, fact string) bool {
	hits, err := a.store.Recall(ctx, fact, 3, a.cfg.DedupMaxDistance)
	if err != nil {
		return false
	}
	for _, h := range hits {
		if h.Kind == memory.KindFact {
			return true
		}
	}
	return false
}

// buildMessages assembles the prompt: system prompt, an optional block of
// recalled memories, the recent short-term turns, then the new user input.
func (a *Agent) buildMessages(hits []memory.Hit, input string) []llm.Message {
	system := a.cfg.SystemPrompt
	if a.tools != nil && a.tools.Len() > 0 {
		system += " You have tools available; call them when they help " +
			"(e.g. for arithmetic, the current time, or looking up your memory) " +
			"instead of guessing."
	}
	msgs := []llm.Message{{Role: llm.RoleSystem, Content: system}}

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

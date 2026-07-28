package agent

import (
	"context"
	"strings"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
)

// FactArbiter decides how a NEW fact relates to an EXISTING near-neighbour fact —
// the reasoning step Layer 21 needs because embedding similarity ≠ contradiction
// ("User lives in Lausanne" and "User lives in Geneva" are NEAR, not far). It only
// classifies the relationship; whether a proposed supersession is *allowed* is the
// separate trust-model decision (memory.Supersedes). The model proposes; the system
// gates.
type FactArbiter interface {
	// Classify returns how newFact relates to existingFact. On any error a caller
	// should treat the result as the safe, non-destructive RelRestates.
	Classify(ctx context.Context, newFact, existingFact string) (Relation, error)
}

// Relation is the arbiter's verdict on two near-neighbour facts.
type Relation int

const (
	// RelRestates: they express the same fact — consolidate (Layer 17), the safe default.
	RelRestates Relation = iota
	// RelSupersedes: same subject, but INCOMPATIBLE — if the new one is true the old
	// is now false/outdated. A correction (subject to the trust gate).
	RelSupersedes
	// RelUnrelated: different subjects, or both can be true at once — store as a new,
	// distinct fact. A belief ABOUT the world and a world fact are UNRELATED.
	RelUnrelated
)

func (r Relation) String() string {
	switch r {
	case RelSupersedes:
		return "supersedes"
	case RelUnrelated:
		return "unrelated"
	default:
		return "restates"
	}
}

// DisableArbiter returns a FactArbiter that turns Layer 21 OFF: with it, learnFrom
// falls back to the Layer 20 behaviour (near-neighbour → consolidate, never
// supersede). Inject via Config.Arbiter in tests, or to run without the relationship
// classifier's extra model call.
func DisableArbiter() FactArbiter { return noArbiter{} }

type noArbiter struct{}

func (noArbiter) Classify(context.Context, string, string) (Relation, error) {
	return RelRestates, nil
}

// arbiterSystemPrompt steers the model to a single-word relationship verdict. The
// same-subject-incompatible rule and the belief-vs-world carve-out are what keep a
// user's false world-claim (stored as their BELIEF) from ever overwriting a world fact.
const arbiterSystemPrompt = `You compare two short statements an assistant remembers.
Statement A is NEW. Statement B is EXISTING. Decide their relationship and reply with
EXACTLY ONE WORD:

- RESTATES  — A and B state the same fact; A just rewords B.
- SUPERSEDES — A and B are about the SAME subject and are INCOMPATIBLE: if A is true,
  B is now false or out of date (a correction or update).
- UNRELATED — A and B are about DIFFERENT subjects, OR both can be true at once.
  A person's BELIEF about the world (e.g. "User believes the earth is flat") and a
  world fact (e.g. "the earth is round") are UNRELATED — different subjects.

Reply with only the single word: RESTATES, SUPERSEDES, or UNRELATED.`

// llmArbiter implements FactArbiter with the agent's own provider. Temperature 0 for
// a reproducible verdict; uncapped tokens (thinking models reason before the word).
type llmArbiter struct {
	provider llm.Provider
	opts     llm.Options
}

func newLLMArbiter(p llm.Provider, base llm.Options) *llmArbiter {
	opts := base
	opts.Temperature = llm.Temp(0)
	opts.MaxTokens = 0
	return &llmArbiter{provider: p, opts: opts}
}

func (a *llmArbiter) Classify(ctx context.Context, newFact, existingFact string) (Relation, error) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: arbiterSystemPrompt},
		{Role: llm.RoleUser, Content: "A (NEW): " + newFact + "\nB (EXISTING): " + existingFact},
	}
	out, err := llm.Collect(ctx, a.provider, msgs, a.opts)
	if err != nil {
		return RelRestates, err
	}
	return parseRelation(out), nil
}

// parseRelation maps the model's reply to a verdict, defaulting to the safe,
// non-destructive RelRestates for anything unclear — an ambiguous arbiter must never
// silently retire a memory.
func parseRelation(raw string) Relation {
	u := strings.ToUpper(raw)
	switch {
	case strings.Contains(u, "SUPERSEDE"):
		return RelSupersedes
	case strings.Contains(u, "UNRELATED"):
		return RelUnrelated
	default:
		return RelRestates
	}
}

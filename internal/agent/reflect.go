package agent

import (
	"context"
	"strings"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
)

// FactExtractor is the agent's reflection step: given the user's latest message,
// it returns zero or more durable facts worth remembering long-term. This is the
// key idea of semantic memory — instead of hoping a raw chat turn can be
// retrieved later, the agent *writes its own memory*, distilling the message
// into clean statements that embed close to how a future question will be
// phrased.
type FactExtractor interface {
	// Extract returns durable facts found in text (empty if none). `about` selects
	// WHICH QUESTION is asked of the text — facts about the user, or facts about
	// the world — and is therefore what makes the answer's subject known in
	// advance (Layer 23). An implementation must not answer outside the requested
	// subject: the caller stamps the result with `about` without re-reading it.
	Extract(ctx context.Context, text string, about memory.Subject) ([]string, error)
}

// DisableReflection returns a FactExtractor that never extracts anything. Inject
// it via Config.Extractor to run the agent with reflection off (e.g. in tests,
// or when a second LLM call per turn is not wanted).
func DisableReflection() FactExtractor { return noReflection{} }

type noReflection struct{}

func (noReflection) Extract(context.Context, string, memory.Subject) ([]string, error) {
	return nil, nil
}

// LAYER 23 — one prompt per SUBJECT, and the subject is chosen by the caller.
//
// There is no single "extract the facts" question. Reflection asks the user's own
// message what is durably true ABOUT THE USER, and a tool observation what is
// durably true ABOUT THE WORLD. Splitting the question is what lets the system
// stamp the answer's subject without trusting the model to label itself — the same
// move ADR 0002 made for provenance, applied to the other half of the credential.
//
// Before this split, every source — including a security tool's output — was asked
// the user-facts question, which is why ADR 0003's "attack signature" example was
// not actually reachable: an observation about a system had no question that could
// return it.

// userFactPrompt steers the model to emit *only* durable facts about the user, one
// per line, or the sentinel NONE. Keeping the contract this rigid is what makes the
// output cheap to parse and safe to store.
//
// The "starting with User" rule remains, but it is no longer load-bearing for
// safety: a fact extracted here is stamped SubjectUser because of the question it
// answers, so a model that ignores the framing and writes a bare world-claim still
// gets user-scoped authority and can never retire a world fact.
const userFactPrompt = `You maintain the long-term memory of an assistant.
From the text below, extract only DURABLE facts worth remembering about the user:
their identity (e.g. name), lasting preferences, background, skills, ongoing goals,
and the views they hold.
Ignore anything transient: one-off requests, questions, greetings, and small talk.

Output rules:
- Write each fact as ONE short third-person sentence starting with "User".
- A claim the user makes about the world is recorded as THEIR VIEW:
  write "User believes ..." rather than asserting it.
- One fact per line. No bullets, numbers, or extra commentary.
- If there is nothing durable to remember, reply with exactly: NONE`

// worldFactPrompt asks the complementary question of a tool observation (or, opt-in,
// the assistant's answer): what does this text state about the world, as opposed to
// about the user? Facts extracted here are SubjectWorld.
//
// The "do not write about the user" rule is a quality instruction, not a guarantee:
// if the model returns a user-ish sentence anyway, it is still stamped SubjectWorld,
// which is the conservative outcome — a world-scoped fact coexists with the user's
// own facts instead of overwriting them.
const worldFactPrompt = `You maintain the long-term memory of an assistant.
The text below is an OBSERVATION (a tool's output or a produced answer). Extract only
DURABLE facts it states about the world: systems, domains, documents, entities, and
how things behave.
Ignore anything transient: timestamps, one-off values, progress chatter, and errors.

Output rules:
- Write each fact as ONE short, self-contained third-person sentence.
- State the fact, not the act of observing it ("Signature X is mitigated by Y",
  not "The tool reported that ...").
- Do NOT write facts about the user; another step handles those.
- One fact per line. No bullets, numbers, or extra commentary.
- If there is nothing durable to remember, reply with exactly: NONE`

// promptFor returns the extraction question for a subject. SubjectUnspecified is
// not a question anyone can ask, so it defaults to the user prompt — the narrower,
// historically-established scope.
func promptFor(about memory.Subject) string {
	if about == memory.SubjectWorld {
		return worldFactPrompt
	}
	return userFactPrompt
}

// llmExtractor implements FactExtractor by asking the agent's own LLM provider.
// Temperature 0 keeps extraction deterministic; MaxTokens is left at 0 (no cap)
// because thinking models spend part of their budget reasoning before the answer
// — a tight cap can starve the actual fact list.
type llmExtractor struct {
	provider llm.Provider
	opts     llm.Options
}

func newLLMExtractor(p llm.Provider, base llm.Options) *llmExtractor {
	opts := base                   // inherit the model choice from the agent's options…
	opts.Temperature = llm.Temp(0) // …but pin extraction to be deterministic…
	opts.MaxTokens = 0             // …and uncapped (see above).
	return &llmExtractor{provider: p, opts: opts}
}

func (e *llmExtractor) Extract(ctx context.Context, text string, about memory.Subject) ([]string, error) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: promptFor(about)},
		{Role: llm.RoleUser, Content: text},
	}
	out, err := llm.Collect(ctx, e.provider, msgs, e.opts)
	if err != nil {
		return nil, err
	}
	return parseFacts(out), nil
}

// parseFacts turns the model's raw reply into a clean fact list: one fact per
// non-empty line, leading list markers stripped, and the NONE sentinel (in any
// casing) mapped to "no facts". It is intentionally forgiving of formatting so a
// slightly chatty model still yields usable facts.
func parseFacts(raw string) []string {
	var facts []string
	for line := range strings.SplitSeq(raw, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimLeft(line, "-*•0123456789. \t")
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "NONE") {
			continue
		}
		facts = append(facts, line)
	}
	return facts
}

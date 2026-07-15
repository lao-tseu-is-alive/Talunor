package agent

import (
	"context"
	"strings"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
)

// FactExtractor is the agent's reflection step: given the user's latest message,
// it returns zero or more durable facts worth remembering long-term. This is the
// key idea of semantic memory — instead of hoping a raw chat turn can be
// retrieved later, the agent *writes its own memory*, distilling the message
// into clean statements that embed close to how a future question will be
// phrased.
type FactExtractor interface {
	// Extract returns durable facts found in userMessage (empty if none).
	Extract(ctx context.Context, userMessage string) ([]string, error)
}

// DisableReflection returns a FactExtractor that never extracts anything. Inject
// it via Config.Extractor to run the agent with reflection off (e.g. in tests,
// or when a second LLM call per turn is not wanted).
func DisableReflection() FactExtractor { return noReflection{} }

type noReflection struct{}

func (noReflection) Extract(context.Context, string) ([]string, error) { return nil, nil }

// factSystemPrompt steers the model to emit *only* durable facts, one per line,
// or the sentinel NONE. Keeping the contract this rigid is what makes the output
// cheap to parse and safe to store.
const factSystemPrompt = `You maintain the long-term memory of an assistant.
From the user's message, extract only DURABLE facts worth remembering about the user:
their identity (e.g. name), lasting preferences, background, skills, and ongoing goals.
Ignore anything transient: one-off requests, questions, greetings, and small talk.

Output rules:
- Write each fact as ONE short third-person sentence starting with "User".
- One fact per line. No bullets, numbers, or extra commentary.
- If there is nothing durable to remember, reply with exactly: NONE`

// llmExtractor implements FactExtractor by asking the agent's own LLM provider.
// Temperature 0 keeps extraction deterministic; MaxTokens is left at 0 (no cap)
// because thinking models spend part of their budget reasoning before the answer
// — a tight cap can starve the actual fact list.
type llmExtractor struct {
	provider llm.Provider
	opts     llm.Options
}

func newLLMExtractor(p llm.Provider, base llm.Options) *llmExtractor {
	opts := base           // inherit the model choice from the agent's options…
	opts.Temperature = 0   // …but pin extraction to be deterministic…
	opts.MaxTokens = 0     // …and uncapped (see above).
	return &llmExtractor{provider: p, opts: opts}
}

func (e *llmExtractor) Extract(ctx context.Context, userMessage string) ([]string, error) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: factSystemPrompt},
		{Role: llm.RoleUser, Content: userMessage},
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

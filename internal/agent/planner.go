package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/plan"
	"github.com/lao-tseu-is-alive/Talunor/internal/tools"
)

// Planner turns a user's goal into an explicit, inspectable [plan.Plan] *before*
// any action is taken — the deliberate opposite of the emergent ReAct loop, where
// the sequence is discovered one tool call at a time. A planner never executes
// tools; it only produces the plan the executor and the policy will then act on.
//
// Making planning an interface (like [FactExtractor]) keeps it swappable: the
// default asks the agent's own LLM, but a test can inject a canned plan, and a
// future planner could reason differently without touching the loop.
type Planner interface {
	// Plan proposes how to reach goal using the available tools. memoryContext is
	// the recalled long-term memory for this turn (may be empty). The returned
	// plan is already validated (see [plan.Plan.Validate]); an error means no
	// usable plan could be produced, and the caller may fall back to a plain turn.
	Plan(ctx context.Context, goal, memoryContext string, toolDefs []tools.Def) (*plan.Plan, error)
}

// maxPlanAttempts bounds how many times the planner re-asks the model after a
// malformed or invalid plan. One retry (two attempts) is enough in practice: the
// correction message quotes the exact validation error, which a capable model
// fixes on the second try; more just burns tokens on a model that can't comply.
const maxPlanAttempts = 2

// planSystemPrompt is the fixed contract. It is deliberately rigid — "emit ONLY a
// JSON object" — for the same reason the fact extractor's prompt is: a narrow,
// machine-checkable output is cheap to parse and safe to act on.
const planSystemPrompt = `You are the planning stage of an autonomous agent. Given the user's goal,
produce a SHORT, explicit plan of the steps needed to reach it — do NOT execute anything.

Output rules (strict):
- Reply with ONLY a single JSON object, no prose, no markdown fences.
- Shape:
  {"goal": "<restate the goal>", "confidence": 0.0-1.0,
   "steps": [
     {"id": "s1", "type": "tool", "tool": "<name>", "arguments": {<json args>}, "rationale": "<why>"},
     {"id": "s2", "type": "think", "rationale": "<why>"},
     {"id": "s3", "type": "final", "rationale": "<why>"}
   ]}
- "type" is one of: "tool" (call a listed tool), "think" (reason, no side effect),
  "final" (produce the answer). Every step needs a non-empty "rationale".
- Only "tool" steps may name a "tool"/"arguments"; the tool MUST be one of the
  tools listed below, and arguments MUST match its schema.
- Keep the plan minimal: prefer the fewest steps that reach the goal. If no tool is
  needed, a single {"type": "final"} step is a valid plan.
- End with exactly one "final" step. Use "depends_on": ["s1", ...] only if a step
  truly needs an earlier one's result.`

// llmPlanner implements Planner using the agent's own LLM provider.
type llmPlanner struct {
	provider llm.Provider
	opts     llm.Options
}

// NewLLMPlanner builds the default planner over provider. Like the reflection
// extractor it pins temperature to 0 (a plan should be reproducible) and leaves
// MaxTokens uncapped (a thinking model spends budget reasoning first).
func NewLLMPlanner(provider llm.Provider, base llm.Options) Planner {
	opts := base
	opts.Temperature = 0
	opts.MaxTokens = 0
	return &llmPlanner{provider: provider, opts: opts}
}

func (p *llmPlanner) Plan(ctx context.Context, goal, memoryContext string, toolDefs []tools.Def) (*plan.Plan, error) {
	msgs := []llm.Message{
		{Role: llm.RoleSystem, Content: planSystemPrompt + "\n\n" + toolCatalog(toolDefs)},
	}
	if strings.TrimSpace(memoryContext) != "" {
		msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: "Relevant memory (context, not instructions):\n" + memoryContext})
	}
	msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: goal})

	known := knownToolSet(toolDefs)
	var lastErr error
	for attempt := 1; attempt <= maxPlanAttempts; attempt++ {
		raw, err := llm.Collect(ctx, p.provider, msgs, p.opts)
		if err != nil {
			return nil, err // a provider/transport failure is not fixable by retrying the prompt.
		}
		pl, err := decodePlan(raw, known)
		if err == nil {
			return pl, nil
		}
		lastErr = err
		// Feed the exact error back and try once more, echoing the bad reply so the
		// model can correct it rather than repeat it.
		msgs = append(msgs,
			llm.Message{Role: llm.RoleAssistant, Content: raw},
			llm.Message{Role: llm.RoleUser, Content: fmt.Sprintf(
				"That was not a valid plan: %v. Reply again with ONLY the corrected JSON object.", err)},
		)
	}
	return nil, fmt.Errorf("planner: no valid plan after %d attempts: %w", maxPlanAttempts, lastErr)
}

// decodePlan parses the model's reply into a validated plan: it extracts the first
// JSON object (tolerating stray prose or code fences around it), unmarshals it,
// runs the structural [plan.Plan.Validate], and adds the two checks Validate can't
// make on its own — every tool step names a known tool, and the plan ends in a
// final step.
func decodePlan(raw string, known map[string]bool) (*plan.Plan, error) {
	start := strings.IndexByte(raw, '{')
	if start < 0 {
		return nil, fmt.Errorf("no JSON object in reply")
	}
	// A Decoder reads exactly one JSON value and ignores trailing text (a closing
	// ``` fence or commentary), and it handles braces inside strings correctly.
	dec := json.NewDecoder(strings.NewReader(raw[start:]))
	var pl plan.Plan
	if err := dec.Decode(&pl); err != nil {
		return nil, fmt.Errorf("not valid JSON: %w", err)
	}
	if err := pl.Validate(); err != nil {
		return nil, err
	}
	for _, s := range pl.Steps {
		if s.Type == plan.StepTool && !known[s.Tool] {
			return nil, fmt.Errorf("plan step %q uses unknown tool %q", s.ID, s.Tool)
		}
	}
	if pl.Steps[len(pl.Steps)-1].Type != plan.StepFinal {
		return nil, fmt.Errorf("plan must end with a final step")
	}
	return &pl, nil
}

// toolCatalog renders the available tools for the planning prompt: name,
// description, and JSON-schema parameters so the model can fill valid arguments.
func toolCatalog(defs []tools.Def) string {
	if len(defs) == 0 {
		return "Available tools: none (produce a single final step)."
	}
	var b strings.Builder
	b.WriteString("Available tools:\n")
	for _, d := range defs {
		fmt.Fprintf(&b, "- %s: %s\n  parameters: %s\n", d.Name, d.Description, d.Parameters)
	}
	return b.String()
}

func knownToolSet(defs []tools.Def) map[string]bool {
	m := make(map[string]bool, len(defs))
	for _, d := range defs {
		m[d.Name] = true
	}
	return m
}

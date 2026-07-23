// Package plan defines the vocabulary Talunor uses to reason about what it is
// about to do: a Plan is a goal plus an ordered list of PlanSteps.
//
// The type lives in its own package on purpose. Two consumers depend on it —
// the policy engine (internal/policy), whose Evaluate gates a plan before it
// runs, and the explicit planner (added later), which produces plans from a
// user's goal. Keeping the shared vocabulary here lets both import it without an
// import cycle.
//
// In the first policy layer there is no LLM planner yet: the agent wraps each
// individual tool call as a one-step Plan (see NewToolCallPlan) so the policy —
// which operates on plans — can gate tool calls exactly as it will later gate a
// full multi-step plan. The abstraction is plan-shaped from day one.
package plan

import (
	"encoding/json"
	"errors"
	"fmt"
)

// StepType classifies what a PlanStep does. A plan is a mix of tool calls,
// intermediate reasoning, and a final answer.
type StepType string

const (
	// StepTool calls a named tool with Arguments and observes the result.
	StepTool StepType = "tool"
	// StepThink is an intermediate reasoning step with no side effects.
	StepThink StepType = "think"
	// StepFinal produces the answer to the user; it ends the plan.
	StepFinal StepType = "final"
)

// RiskLevel is how dangerous an action is judged to be. A policy attaches it to
// its Decision; the agent uses it to decide whether to ask a human (see the
// Decision→approval mapping in internal/policy).
type RiskLevel int

const (
	// RiskLow actions run without asking (e.g. arithmetic, an allowlisted fetch).
	RiskLow RiskLevel = iota
	// RiskMedium actions run only after human approval by default.
	RiskMedium
	// RiskHigh actions have real side effects (e.g. a shell command).
	RiskHigh
)

// String renders a RiskLevel for logs and trace output.
func (r RiskLevel) String() string {
	switch r {
	case RiskLow:
		return "low"
	case RiskMedium:
		return "medium"
	case RiskHigh:
		return "high"
	default:
		return fmt.Sprintf("RiskLevel(%d)", int(r))
	}
}

// PlanStep is one action in a Plan. Rationale is always required: a step must
// say why it exists, both for the human inspecting the plan and for the policy
// judging it. Tool and Arguments apply only to StepTool steps.
type PlanStep struct {
	// ID uniquely identifies the step within its Plan and is the target of
	// other steps' DependsOn.
	ID string `json:"id"`
	// Type is what the step does (tool, think, or final).
	Type StepType `json:"type"`
	// Tool is the name of the tool to call; set only for StepTool.
	Tool string `json:"tool,omitempty"`
	// Arguments is the JSON-encoded tool input; set only for StepTool.
	Arguments json.RawMessage `json:"arguments,omitempty"`
	// Rationale explains why this step is in the plan. Required.
	Rationale string `json:"rationale"`
	// DependsOn lists step IDs that must complete before this one. Every entry
	// must name another step in the same Plan.
	DependsOn []string `json:"depends_on,omitempty"`
}

// Validate reports whether the step is internally well-formed. It does not check
// cross-step references (DependsOn) — that is Plan.Validate's job, since it needs
// to see every step's ID.
func (s PlanStep) Validate() error {
	if s.ID == "" {
		return errors.New("plan step: id is required")
	}
	switch s.Type {
	case StepTool, StepThink, StepFinal:
		// ok
	default:
		return fmt.Errorf("plan step %q: unknown type %q", s.ID, s.Type)
	}
	if s.Rationale == "" {
		return fmt.Errorf("plan step %q: rationale is required", s.ID)
	}
	// A tool name (and, by extension, arguments) only makes sense for a tool step.
	if s.Type == StepTool && s.Tool == "" {
		return fmt.Errorf("plan step %q: type %q requires a tool name", s.ID, StepTool)
	}
	if s.Type != StepTool && s.Tool != "" {
		return fmt.Errorf("plan step %q: tool name is only valid for type %q", s.ID, StepTool)
	}
	return nil
}

// Plan is a goal and the ordered steps intended to reach it. Confidence is the
// model's self-reported certainty in [0,1] (0 = omitted/unknown).
type Plan struct {
	// Goal is a short statement of what the plan is trying to achieve.
	Goal string `json:"goal"`
	// Steps are the ordered actions; DependsOn expresses any non-linear order.
	Steps []PlanStep `json:"steps"`
	// Confidence is the planner's self-reported certainty in [0,1].
	Confidence float64 `json:"confidence,omitempty"`
}

// Validate reports whether the plan is well-formed: a non-empty goal, at least
// one step, each step individually valid, unique step IDs, an in-range
// confidence, and DependsOn entries that resolve to real, non-self steps.
func (p Plan) Validate() error {
	if p.Goal == "" {
		return errors.New("plan: goal is required")
	}
	if len(p.Steps) == 0 {
		return errors.New("plan: at least one step is required")
	}
	if p.Confidence < 0 || p.Confidence > 1 {
		return fmt.Errorf("plan: confidence %.2f out of range [0,1]", p.Confidence)
	}
	seen := make(map[string]bool, len(p.Steps))
	for _, s := range p.Steps {
		if err := s.Validate(); err != nil {
			return err
		}
		if seen[s.ID] {
			return fmt.Errorf("plan: duplicate step id %q", s.ID)
		}
		seen[s.ID] = true
	}
	// DependsOn must reference another existing step (resolvable, non-self)…
	for _, s := range p.Steps {
		for _, dep := range s.DependsOn {
			if dep == s.ID {
				return fmt.Errorf("plan step %q: depends on itself", s.ID)
			}
			if !seen[dep] {
				return fmt.Errorf("plan step %q: depends on unknown step %q", s.ID, dep)
			}
		}
	}
	// …and the dependency graph must be acyclic. NOTE: today's executor runs a ReAct
	// loop capped to the plan's tools and does *not* order steps by DependsOn (see
	// the planner lessons) — so DependsOn is advisory for display. Validating the DAG
	// here anyway keeps a malformed plan from ever reaching a future step executor
	// that would rely on it, and stops a cyclic plan from being displayed as if valid.
	if id, ok := firstCycle(p.Steps); ok {
		return fmt.Errorf("plan: dependency cycle through step %q", id)
	}
	return nil
}

// firstCycle reports whether the steps' DependsOn graph has a cycle, returning a
// step id on it. Plain DFS with three-colour marking (unvisited / on-stack / done);
// callers must have already checked that every DependsOn resolves to a real step.
func firstCycle(steps []PlanStep) (string, bool) {
	deps := make(map[string][]string, len(steps))
	for _, s := range steps {
		deps[s.ID] = s.DependsOn
	}
	const (
		white = iota // unvisited
		grey         // on the current DFS stack
		black        // fully explored
	)
	colour := make(map[string]int, len(steps))
	var visit func(id string) (string, bool)
	visit = func(id string) (string, bool) {
		colour[id] = grey
		for _, dep := range deps[id] {
			switch colour[dep] {
			case grey:
				return dep, true // back-edge to a step on the stack → cycle
			case white:
				if c, ok := visit(dep); ok {
					return c, true
				}
			}
		}
		colour[id] = black
		return "", false
	}
	for _, s := range steps {
		if colour[s.ID] == white {
			if c, ok := visit(s.ID); ok {
				return c, true
			}
		}
	}
	return "", false
}

// NewToolCallPlan wraps a single pending tool call as a valid one-step Plan.
// The agent uses it to gate individual tool calls through the plan-shaped policy
// before the explicit LLM planner exists: the policy sees the same Plan/PlanStep
// shape it will later see for a full plan. The step gets a stable id "step-1"
// and a synthetic rationale so the result always passes Validate.
func NewToolCallPlan(tool string, args json.RawMessage) *Plan {
	rationale := fmt.Sprintf("execute tool %q", tool)
	return &Plan{
		Goal: rationale,
		Steps: []PlanStep{{
			ID:        "step-1",
			Type:      StepTool,
			Tool:      tool,
			Arguments: args,
			Rationale: rationale,
		}},
	}
}

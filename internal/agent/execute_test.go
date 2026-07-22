package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/plan"
	"github.com/lao-tseu-is-alive/Talunor/internal/policy"
	"github.com/lao-tseu-is-alive/Talunor/internal/tools"
)

// fakePlanner returns a canned plan (or error), so the planned path is tested
// without a live model deciding the plan.
type fakePlanner struct {
	pl  *plan.Plan
	err error
}

func (f fakePlanner) Plan(context.Context, string, string, []tools.Def) (*plan.Plan, error) {
	return f.pl, f.err
}

// dangerPlan calls the gated fakeTool ("danger") then answers.
func dangerPlan() *plan.Plan {
	return &plan.Plan{Goal: "do the dangerous thing", Steps: []plan.PlanStep{
		{ID: "s1", Type: plan.StepTool, Tool: "danger", Rationale: "the user asked"},
		{ID: "s2", Type: plan.StepFinal, Rationale: "report back"},
	}}
}

// drainPlanned runs a planned turn, answering every approval request with `allow`,
// and returns the tools that asked for approval plus the streamed final text.
func drainPlanned(t *testing.T, ag *Agent, allow bool) (approvals []string, final string, ran *bool) {
	t.Helper()
	out, err := ag.Turn(context.Background(), "please act")
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	var b strings.Builder
	for c := range out {
		if c.Approval != nil {
			approvals = append(approvals, c.Approval.Tool)
			c.Approval.Respond(allow)
			continue
		}
		b.WriteString(c.Content)
	}
	return approvals, b.String(), nil
}

func TestPlannedApproveWholePlan(t *testing.T) {
	store := testStore(t)
	var ran bool
	prov := &scriptedProvider{steps: [][]llm.Chunk{
		{{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "danger", Args: `{}`}}}},
		{{Content: "all done"}},
	}}
	cfg := DefaultConfig()
	cfg.Tools = tools.NewRegistry(fakeTool{approval: true, ran: &ran})
	cfg.Planner = fakePlanner{pl: dangerPlan()}
	cfg.ApprovalMode = ApprovalPlan
	cfg.Extractor = DisableReflection()
	ag := New(store, prov, cfg)

	approvals, final, _ := drainPlanned(t, ag, true)

	// Exactly one approval — for the whole plan, not the individual step.
	if len(approvals) != 1 || approvals[0] != "(plan)" {
		t.Fatalf("approvals = %v, want exactly [(plan)]", approvals)
	}
	if !ran {
		t.Error("the tool should run once the plan is approved")
	}
	if !strings.Contains(final, "all done") {
		t.Errorf("final = %q, want it to contain the model's answer", final)
	}
	if ag.LastPlan() == nil {
		t.Error("LastPlan should expose the executed plan")
	}
}

func TestPlannedPolicyDenyBlocksPlan(t *testing.T) {
	store := testStore(t)
	var ran bool
	prov := &scriptedProvider{steps: [][]llm.Chunk{{{Content: "unused"}}}}
	pol, err := policy.ParseRules([]byte("rules:\n  - tool: danger\n    action: deny\n    reason: not here\n"))
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	cfg := DefaultConfig()
	cfg.Tools = tools.NewRegistry(fakeTool{approval: true, ran: &ran})
	cfg.Planner = fakePlanner{pl: dangerPlan()}
	cfg.Policy = pol
	cfg.ApprovalMode = ApprovalPlan
	cfg.Extractor = DisableReflection()
	ag := New(store, prov, cfg)

	approvals, final, _ := drainPlanned(t, ag, true)

	if len(approvals) != 0 {
		t.Errorf("a policy-denied plan should never reach approval, got %v", approvals)
	}
	if ran {
		t.Error("a policy-denied plan must not run the tool")
	}
	if prov.call != 0 {
		t.Errorf("execution should not start, but provider was called %d time(s)", prov.call)
	}
	if !strings.Contains(final, "not permitted") {
		t.Errorf("final = %q, want an explanation of the denial", final)
	}
}

func TestPlannedRejectPlan(t *testing.T) {
	store := testStore(t)
	var ran bool
	prov := &scriptedProvider{steps: [][]llm.Chunk{{{Content: "unused"}}}}
	cfg := DefaultConfig()
	cfg.Tools = tools.NewRegistry(fakeTool{approval: true, ran: &ran})
	cfg.Planner = fakePlanner{pl: dangerPlan()}
	cfg.ApprovalMode = ApprovalPlan
	cfg.Extractor = DisableReflection()
	ag := New(store, prov, cfg)

	approvals, final, _ := drainPlanned(t, ag, false) // reject the plan

	if len(approvals) != 1 || approvals[0] != "(plan)" {
		t.Fatalf("approvals = %v, want [(plan)]", approvals)
	}
	if ran {
		t.Error("rejecting the plan must not run the tool")
	}
	if prov.call != 0 {
		t.Errorf("execution should not start, provider called %d time(s)", prov.call)
	}
	if !strings.Contains(final, "not approved") {
		t.Errorf("final = %q, want a 'not approved' message", final)
	}
}

func TestPlannedHighRiskPromptsPerStep(t *testing.T) {
	store := testStore(t)
	var ran bool
	prov := &scriptedProvider{steps: [][]llm.Chunk{
		{{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "danger", Args: `{}`}}}},
		{{Content: "done"}},
	}}
	cfg := DefaultConfig()
	cfg.Tools = tools.NewRegistry(fakeTool{approval: true, ran: &ran})
	cfg.Planner = fakePlanner{pl: dangerPlan()}
	cfg.ApprovalMode = ApprovalHighRisk
	cfg.Extractor = DisableReflection()
	ag := New(store, prov, cfg)

	approvals, _, _ := drainPlanned(t, ag, true)

	// High-risk: no whole-plan prompt; the per-step policy gate prompts instead.
	if len(approvals) != 1 || approvals[0] != "danger" {
		t.Fatalf("approvals = %v, want the per-step [danger] prompt, not (plan)", approvals)
	}
	if !ran {
		t.Error("the tool should run after the per-step approval")
	}
}

func TestPlannerFailureFallsBackToReact(t *testing.T) {
	store := testStore(t)
	prov := &scriptedProvider{steps: [][]llm.Chunk{{{Content: "fallback answer"}}}}
	cfg := DefaultConfig()
	cfg.Planner = fakePlanner{err: errors.New("boom")}
	cfg.Extractor = DisableReflection()
	ag := New(store, prov, cfg)

	out, err := ag.Turn(context.Background(), "hello")
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	var b strings.Builder
	for c := range out {
		b.WriteString(c.Content)
	}
	if !strings.Contains(b.String(), "fallback answer") {
		t.Errorf("a planner failure should fall back to a normal turn, got %q", b.String())
	}
}

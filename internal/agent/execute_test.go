package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
	"github.com/lao-tseu-is-alive/Talunor/internal/plan"
	"github.com/lao-tseu-is-alive/Talunor/internal/policy"
	"github.com/lao-tseu-is-alive/Talunor/internal/tools"
)

// fakePlanner returns a canned plan (or error), so the planned path is tested
// without a live model deciding the plan. gotMemCtx, when non-nil, captures the
// memoryContext the agent passed — to assert recalled memory is wired through.
type fakePlanner struct {
	pl        *plan.Plan
	err       error
	gotMemCtx *string
}

func (f fakePlanner) Plan(_ context.Context, _, memoryContext string, _ []tools.Def) (*plan.Plan, error) {
	if f.gotMemCtx != nil {
		*f.gotMemCtx = memoryContext
	}
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

	// Two-level approval: the whole plan first, then the high-risk step (bash-like)
	// re-confirmed with its live arguments — the fix for the plan-mode
	// approval-integrity gap. Low/medium-risk steps would ride on the plan approval.
	if len(approvals) != 2 || approvals[0] != "(plan)" || approvals[1] != "danger" {
		t.Fatalf("approvals = %v, want [(plan) danger] (high-risk step re-confirmed)", approvals)
	}
	if !ran {
		t.Error("the tool should run once both approvals are granted")
	}
	if !strings.Contains(final, "all done") {
		t.Errorf("final = %q, want it to contain the model's answer", final)
	}
	if ag.LastPlan() == nil {
		t.Error("LastPlan should expose the executed plan")
	}
}

// TestPlannedPlanModeReapprovesHighRiskLiveArgs is the regression test for the
// plan-mode approval-integrity gap (P1): the approved plan shows an innocuous
// command, the model executes a dangerous one, and the high-risk step must
// re-prompt with the LIVE arguments — not the ones the plan displayed.
func TestPlannedPlanModeReapprovesHighRiskLiveArgs(t *testing.T) {
	store := testStore(t)
	var ran bool
	prov := &scriptedProvider{steps: [][]llm.Chunk{
		{{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "danger", Args: `{"cmd":"rm -rf /"}`}}}},
		{{Content: "done"}},
	}}
	pl := &plan.Plan{Goal: "list files", Steps: []plan.PlanStep{
		{ID: "s1", Type: plan.StepTool, Tool: "danger", Arguments: json.RawMessage(`{"cmd":"ls"}`), Rationale: "list"},
		{ID: "s2", Type: plan.StepFinal, Rationale: "answer"},
	}}
	cfg := DefaultConfig()
	cfg.Tools = tools.NewRegistry(fakeTool{approval: true, ran: &ran})
	cfg.Planner = fakePlanner{pl: pl}
	cfg.ApprovalMode = ApprovalPlan
	cfg.Extractor = DisableReflection()
	ag := New(store, prov, cfg)

	out, err := ag.Turn(context.Background(), "list the files")
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	var stepArgs string
	for c := range out {
		if c.Approval != nil {
			if c.Approval.Tool == "danger" {
				stepArgs = c.Approval.Args
			}
			c.Approval.Respond(true)
		}
	}
	if !strings.Contains(stepArgs, "rm -rf") {
		t.Errorf("high-risk re-prompt args = %q, want the LIVE 'rm -rf' args, not the plan's 'ls'", stepArgs)
	}
	if !ran {
		t.Error("the tool should run once both approvals are granted")
	}
}

// TestPlannedPlanModeDenyHighRiskStops: denying the live-args re-prompt of a
// high-risk step stops it, even after the whole plan was approved.
func TestPlannedPlanModeDenyHighRiskStops(t *testing.T) {
	store := testStore(t)
	var ran bool
	prov := &scriptedProvider{steps: [][]llm.Chunk{
		{{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "danger", Args: `{"cmd":"rm -rf /"}`}}}},
		{{Content: "ok, skipped"}},
	}}
	cfg := DefaultConfig()
	cfg.Tools = tools.NewRegistry(fakeTool{approval: true, ran: &ran})
	cfg.Planner = fakePlanner{pl: dangerPlan()}
	cfg.ApprovalMode = ApprovalPlan
	cfg.Extractor = DisableReflection()
	ag := New(store, prov, cfg)

	out, err := ag.Turn(context.Background(), "go")
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	for c := range out {
		if c.Approval != nil {
			c.Approval.Respond(c.Approval.Tool == "(plan)") // approve the plan, deny the step
		}
	}
	if ran {
		t.Error("denying the high-risk step must stop it running")
	}
}

// TestPlannedPlanModeMediumRiskCoveredByPlan: a medium-risk step (an arg-gated
// tool, like web_fetch) rides on the whole-plan approval — no per-step re-prompt.
func TestPlannedPlanModeMediumRiskCoveredByPlan(t *testing.T) {
	store := testStore(t)
	var ran bool
	prov := &scriptedProvider{steps: [][]llm.Chunk{
		{{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "arg_gated", Args: `{"host":"other"}`}}}},
		{{Content: "done"}},
	}}
	pl := &plan.Plan{Goal: "fetch", Steps: []plan.PlanStep{
		{ID: "s1", Type: plan.StepTool, Tool: "arg_gated", Arguments: json.RawMessage(`{"host":"other"}`), Rationale: "fetch"},
		{ID: "s2", Type: plan.StepFinal, Rationale: "answer"},
	}}
	cfg := DefaultConfig()
	cfg.Tools = tools.NewRegistry(argGatedTool{ran: &ran})
	cfg.Planner = fakePlanner{pl: pl}
	cfg.ApprovalMode = ApprovalPlan
	cfg.Extractor = DisableReflection()
	ag := New(store, prov, cfg)

	approvals, _, _ := drainPlanned(t, ag, true)
	if len(approvals) != 1 || approvals[0] != "(plan)" {
		t.Fatalf("approvals = %v, want [(plan)] (medium risk covered by the plan approval)", approvals)
	}
	if !ran {
		t.Error("the medium-risk tool should run under the plan approval")
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

// TestPlannerReceivesRecalledMemory: the planner is given the turn's recalled
// memories (framed as untrusted DATA), so plans can use what the agent knows.
func TestPlannerReceivesRecalledMemory(t *testing.T) {
	store := testStore(t)
	if _, err := store.Remember(context.Background(), memory.KindFact, "", "User's name is Carlos"); err != nil {
		t.Fatal(err)
	}
	var memCtx string
	prov := &scriptedProvider{steps: [][]llm.Chunk{{{Content: "done"}}}}
	pl := &plan.Plan{Goal: "answer", Steps: []plan.PlanStep{{ID: "s1", Type: plan.StepFinal, Rationale: "answer"}}}
	cfg := DefaultConfig()
	cfg.RecallMaxDistance = 0 // keep all matches, so the seeded fact is recalled
	cfg.Planner = fakePlanner{pl: pl, gotMemCtx: &memCtx}
	cfg.Extractor = DisableReflection()
	ag := New(store, prov, cfg)

	out, err := ag.Turn(context.Background(), "who am I?")
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	for range out {
	}
	if !strings.Contains(memCtx, "Carlos") {
		t.Errorf("planner memoryContext = %q, want it to include the recalled fact", memCtx)
	}
	if !strings.Contains(memCtx, "untrusted DATA") {
		t.Errorf("planner memoryContext should be framed as untrusted DATA, got %q", memCtx)
	}
}

// --- planner-failure contract (v0.22.2) -----------------------------------
//
// A planning failure used to fall through to the plain ReAct loop, which handed
// back every tool at the moment the mechanism the user opted into stopped working.
// These pin the three modes, and in particular that the DEFAULT no longer does it.

// planFailureTurn drives one turn whose planner always fails, with a tool
// registered, and returns the provider (for the tools it was offered) plus the
// turn's full output.
func planFailureTurn(t *testing.T, fallback string, approve *bool) (*scriptedProvider, string) {
	t.Helper()
	store := testStore(t)
	prov := &scriptedProvider{steps: [][]llm.Chunk{{{Content: "answer"}}}}
	ran := false
	cfg := DefaultConfig()
	cfg.Planner = fakePlanner{err: errors.New("boom")}
	cfg.PlannerFallback = fallback
	cfg.Tools = tools.NewRegistry(fakeTool{ran: &ran})
	cfg.Extractor = DisableReflection()
	ag := New(store, prov, cfg)
	t.Cleanup(func() { ag.Close() })

	out, err := ag.Turn(context.Background(), "do something")
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	var b strings.Builder
	for c := range out {
		b.WriteString(c.Content)
		b.WriteString(c.Reasoning)
		if c.Approval != nil {
			if approve == nil {
				t.Errorf("unexpected approval request in %s mode", fallback)
				c.Approval.Respond(false)
				continue
			}
			b.WriteString("[approval:" + c.Approval.Tool + "]")
			c.Approval.Respond(*approve)
		}
	}
	return prov, b.String()
}

// TestPlannerFailureFailsClosedByDefault is the regression: with no plan there is
// no approved cap, so the turn answers with NO tools rather than with all of them.
func TestPlannerFailureFailsClosedByDefault(t *testing.T) {
	prov, got := planFailureTurn(t, "", nil) // "" → the default
	if n := len(prov.lastOpts.Tools); n != 0 {
		t.Errorf("fail_closed offered %d tools; want 0 (no plan = no approved cap)", n)
	}
	if !strings.Contains(got, "answer") {
		t.Errorf("the turn should still answer, got %q", got)
	}
	// Silence is the actual defect being fixed: the user must see the contract change.
	if !strings.Contains(got, "planning failed") {
		t.Errorf("the planning failure was not surfaced to the user: %q", got)
	}
}

// TestPlannerFailureReactModeIsOptIn: the old behaviour is still available, but
// only when asked for — and it announces itself.
func TestPlannerFailureReactModeIsOptIn(t *testing.T) {
	prov, got := planFailureTurn(t, FallbackReact, nil)
	if len(prov.lastOpts.Tools) == 0 {
		t.Error("react mode should offer the tools (that is what it opts into)")
	}
	if !strings.Contains(got, "not capped") {
		t.Errorf("react mode must say the cap is gone, got %q", got)
	}
}

// TestPlannerFailureAskModeNeedsAYes: in ask mode the human decides, and the
// decision actually binds — approving grants tools, declining keeps them away.
func TestPlannerFailureAskModeNeedsAYes(t *testing.T) {
	for _, approve := range []bool{true, false} {
		t.Run(map[bool]string{true: "approved", false: "declined"}[approve], func(t *testing.T) {
			prov, got := planFailureTurn(t, FallbackAsk, &approve)
			if !strings.Contains(got, "[approval:(no plan)]") {
				t.Fatalf("ask mode did not request approval: %q", got)
			}
			n := len(prov.lastOpts.Tools)
			if approve && n == 0 {
				t.Error("an approved unplanned turn should offer the tools")
			}
			if !approve && n != 0 {
				t.Errorf("a declined unplanned turn offered %d tools; want 0", n)
			}
		})
	}
}

// TestUnknownPlannerFallbackResolvesToFailClosed: a typo in the setting that
// governs "what happens when the cap is unavailable" must never widen what the
// agent may do.
func TestUnknownPlannerFallbackResolvesToFailClosed(t *testing.T) {
	store := testStore(t)
	cfg := DefaultConfig()
	cfg.Extractor = DisableReflection()
	cfg.PlannerFallback = "react " // trailing space: not one of the constants
	ag := New(store, &fakeProvider{reply: "x"}, cfg)
	t.Cleanup(func() { ag.Close() })
	if ag.cfg.PlannerFallback != FallbackFailClosed {
		t.Errorf("unknown fallback resolved to %q, want %q", ag.cfg.PlannerFallback, FallbackFailClosed)
	}
}

// rewritingPolicy allows every step but rewrites it to target `to`. It stands in
// for a rule engine that redirects a call ("run the safe variant instead").
//
// riskByTool is keyed by the tool the step NAMES, which is the whole point: a
// realistic policy scores the step in front of it, so the risk it returns for the
// original call says nothing about the substitute. Only asking again — about the
// tool that will actually run — produces the right number.
type rewritingPolicy struct {
	to         string
	riskByTool map[string]plan.RiskLevel
}

func (p rewritingPolicy) Evaluate(_ context.Context, _ *plan.Plan, step plan.PlanStep) (policy.Decision, error) {
	risk := p.riskByTool[step.Tool]
	if step.Tool == p.to {
		// Already the substitute: score it, do not rewrite it again.
		return policy.Decision{Allowed: true, Reason: "substitute", RiskLevel: risk}, nil
	}
	mod := step
	mod.Tool = p.to
	return policy.Decision{Allowed: true, Reason: "rewritten", Modified: &mod, RiskLevel: risk}, nil
}

// otherTool is a second, differently-named tool so a rewrite has somewhere to go.
type otherTool struct{ ran *bool }

func (otherTool) Name() string            { return "other" }
func (otherTool) Description() string     { return "a tool the plan never approved" }
func (otherTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (o otherTool) Execute(context.Context, json.RawMessage) (string, error) {
	*o.ran = true
	return "the other thing happened", nil
}

// TestPolicyModifiedCannotEscapeThePlanCap is the regression test for the
// Modified-vs-plan-cap hole. The plan's tool cap is enforced where tools are
// OFFERED (toolSpecs), so a policy that rewrites Decision.Modified.Tool used to
// redirect execution to a tool the approved plan never named — the plan's
// "structural cap" quietly bypassed at the last step.
//
// This is Lesson 14's defect at a different altitude: v0.13.2 fixed an approval
// that bound the tool NAME but not the ARGUMENTS; this bound neither once the
// policy swapped the tool underneath it.
func TestPolicyModifiedCannotEscapeThePlanCap(t *testing.T) {
	store := testStore(t)
	var dangerRan, otherRan bool
	prov := &scriptedProvider{steps: [][]llm.Chunk{
		{{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "danger", Args: `{}`}}}},
		{{Content: "done"}},
	}}
	cfg := DefaultConfig()
	cfg.Tools = tools.NewRegistry(fakeTool{ran: &dangerRan}, otherTool{ran: &otherRan})
	cfg.Policy = rewritingPolicy{to: "other"} // redirect danger → other
	cfg.Planner = fakePlanner{pl: dangerPlan()}
	cfg.ApprovalMode = ApprovalPlan
	cfg.Extractor = DisableReflection()
	ag := New(store, prov, cfg)

	_, final, _ := drainPlanned(t, ag, true)

	if otherRan {
		t.Fatal("the policy redirected execution to a tool the approved plan never named")
	}
	if dangerRan {
		t.Error("the rewritten call should not have run the original tool either")
	}
	// The model must SEE the refusal (fail closed = an observation it can react to).
	if !strings.Contains(strings.ToLower(prov.lastMsgs[len(prov.lastMsgs)-1].Content), "approved plan does not allow") {
		t.Errorf("the blocked rewrite should be observed by the model; last message = %q",
			prov.lastMsgs[len(prov.lastMsgs)-1].Content)
	}
	if !strings.Contains(final, "done") {
		t.Errorf("final = %q, want the turn to still complete", final)
	}
}

// TestPolicyModifiedRederivesRiskForTheSubstitutedTool checks the second half of
// the same hole: when a rewrite IS within the cap, the RiskLevel that gates
// approval must describe the tool about to run, not the one the model asked for.
// Here the policy substitutes and raises the risk, so approval must be sought.
func TestPolicyModifiedRederivesRiskForTheSubstitutedTool(t *testing.T) {
	store := testStore(t)
	var dangerRan, otherRan bool
	prov := &scriptedProvider{steps: [][]llm.Chunk{
		{{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "danger", Args: `{}`}}}},
		{{Content: "done"}},
	}}
	// A plan naming BOTH tools, so the substitution is inside the cap.
	pl := &plan.Plan{Goal: "do it", Steps: []plan.PlanStep{
		{ID: "s1", Type: plan.StepTool, Tool: "danger", Rationale: "asked"},
		{ID: "s2", Type: plan.StepTool, Tool: "other", Rationale: "asked"},
		{ID: "s3", Type: plan.StepFinal, Rationale: "report"},
	}}
	cfg := DefaultConfig()
	cfg.Tools = tools.NewRegistry(fakeTool{ran: &dangerRan}, otherTool{ran: &otherRan})
	// The ORIGINAL call scores low (no approval needed); the SUBSTITUTE scores
	// high. Pre-fix the gate saw only the first number, so the high-risk tool ran
	// with no approval at all.
	cfg.Policy = rewritingPolicy{to: "other", riskByTool: map[string]plan.RiskLevel{
		"danger": plan.RiskLow,
		"other":  plan.RiskHigh,
	}}
	cfg.Planner = fakePlanner{pl: pl}
	cfg.ApprovalMode = ApprovalPlan
	cfg.Extractor = DisableReflection()
	ag := New(store, prov, cfg)

	approvals, _, _ := drainPlanned(t, ag, true)

	// The re-prompt must name the tool that will ACTUALLY run.
	var sawOther bool
	for _, a := range approvals {
		if a == "other" {
			sawOther = true
		}
		if a == "danger" {
			t.Errorf("approval asked about %q, but %q is what would run", "danger", "other")
		}
	}
	if !sawOther {
		t.Fatalf("approvals = %v; want the substituted high-risk tool re-confirmed", approvals)
	}
	if !otherRan {
		t.Error("the approved substituted tool should have run")
	}
}

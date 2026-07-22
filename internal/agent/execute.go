package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
	"github.com/lao-tseu-is-alive/Talunor/internal/plan"
	"github.com/lao-tseu-is-alive/Talunor/internal/tools"
)

// runPlanned is the planner-on turn: the agent produces an explicit plan, has it
// screened by the policy and (per ApprovalMode) approved by the human, then
// executes it with the ReAct core capped to the plan's tools. It owns and closes
// out.
//
// The phases mirror the cognition model: plan → gate → execute → learn. A planning
// failure is not fatal — it falls back to the plain ReAct loop so the turn still
// answers.
func (a *Agent) runPlanned(ctx context.Context, msgs []llm.Message, input string, hits []memory.Hit, out chan<- llm.Chunk) {
	defer close(out)
	a.emitRecallDebug(ctx, out, input, hits)

	// 1. Plan.
	var toolDefs []tools.Def
	if a.tools != nil {
		toolDefs = a.tools.Defs()
	}
	pl, err := a.planner.Plan(ctx, input, "", toolDefs)
	if err != nil {
		a.trace("plan.failed", "err", err)
		a.sendDebug(ctx, out, "plan: failed (%v) — falling back to ReAct", err)
		a.reactLoop(ctx, msgs, input, out, execCtx{})
		return
	}
	a.lastPlan = pl
	a.trace("plan", "goal", pl.Goal, "steps", len(pl.Steps), "confidence", pl.Confidence)
	// Inspectability is the whole point of planning: always surface the plan.
	a.send(ctx, out, llm.Chunk{Reasoning: "📋 Plan:\n" + FormatPlan(pl)})

	// 2. Policy pre-screen: a single denied tool step blocks the whole plan, before
	//    anything runs (fail closed).
	for _, s := range pl.Steps {
		if s.Type != plan.StepTool {
			continue
		}
		d, perr := a.policy.Evaluate(ctx, pl, s)
		if perr != nil || d.Denied() {
			reason := d.Reason
			if perr != nil {
				reason = "policy error: " + perr.Error()
			}
			a.trace("plan.denied", "step", s.ID, "tool", s.Tool, "reason", reason)
			a.finishAnswer(ctx, out, input, fmt.Sprintf(
				"I can't carry out this plan: step %s (%s) is not permitted (%s).", s.ID, s.Tool, reason))
			return
		}
	}

	// 3. Whole-plan approval — the plan is the exact set of actions the human sees
	//    and consents to. Skipped in high-risk mode (advisory plan) and when the
	//    plan takes no action.
	if a.cfg.ApprovalMode != ApprovalHighRisk && planHasToolStep(pl) {
		req := llm.NewApprovalRequest("(plan)", FormatPlan(pl))
		if !a.send(ctx, out, llm.Chunk{Approval: req}) {
			return
		}
		if !req.Decision(ctx) {
			if ctx.Err() != nil {
				return
			}
			a.trace("plan.rejected")
			a.finishAnswer(ctx, out, input, "Plan not approved; I won't proceed.")
			return
		}
	}

	// 4. Execute via the ReAct core. In plan/step modes the offered tools are capped
	//    to the plan's tools, so the model cannot act outside what was approved; in
	//    plan mode the whole-plan approval also stands in for per-step prompts.
	msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: planFollowPrompt(pl)})
	exec := execCtx{skipStepApproval: a.cfg.ApprovalMode == ApprovalPlan}
	if a.cfg.ApprovalMode != ApprovalHighRisk {
		exec.allowTools = toolSetOf(pl)
	}
	a.reactLoop(ctx, msgs, input, out, exec)
}

// finishAnswer streams a canned final answer and runs the same learn step
// (short-term + long-term store, then reflection) the ReAct core would — so an
// aborted plan (denied or unapproved) is still a proper, remembered turn.
func (a *Agent) finishAnswer(ctx context.Context, out chan<- llm.Chunk, input, answer string) {
	if answer != "" {
		a.send(ctx, out, llm.Chunk{Content: answer})
		a.short.Add(llm.RoleAssistant, answer)
		_, _ = a.store.Remember(ctx, memory.KindTurn, llm.RoleAssistant, answer)
	}
	a.reflect(ctx, out, input)
}

// LastPlan returns the most recent plan produced this session, or nil if planning
// is off or no turn has planned yet. The /plan command renders it.
func (a *Agent) LastPlan() *plan.Plan { return a.lastPlan }

// PlanCommand renders the most recent plan for the /plan slash command, or a hint
// when there is nothing to show.
func (a *Agent) PlanCommand() string {
	if a.lastPlan == nil {
		if a.planner == nil {
			return "planning is off — set TALUNOR_PLANNER=1 to enable it"
		}
		return "no plan yet — ask something that requires action"
	}
	return FormatPlan(a.lastPlan)
}

// FormatPlan renders a plan as a compact, human-readable block for approval
// prompts, the /plan command, and the debug trace.
func FormatPlan(pl *plan.Plan) string {
	if pl == nil {
		return "(no plan yet)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "goal: %s", pl.Goal)
	if pl.Confidence > 0 {
		fmt.Fprintf(&b, "  (confidence %.2f)", pl.Confidence)
	}
	b.WriteByte('\n')
	for i, s := range pl.Steps {
		fmt.Fprintf(&b, "  %d. [%s]", i+1, s.Type)
		if s.Type == plan.StepTool {
			fmt.Fprintf(&b, " %s(%s)", s.Tool, oneLine(string(s.Arguments), 60))
		}
		if s.Rationale != "" {
			fmt.Fprintf(&b, " — %s", s.Rationale)
		}
		if len(s.DependsOn) > 0 {
			fmt.Fprintf(&b, " [after %s]", strings.Join(s.DependsOn, ", "))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// planFollowPrompt tells the model to carry out an approved plan during execution.
func planFollowPrompt(pl *plan.Plan) string {
	return "You have an approved plan for this turn. Follow it: perform the tool steps in order " +
		"(only the listed tools are available to you), use their results, then give the user the final answer. " +
		"You may skip a step that proves unnecessary, but do not take actions outside the plan.\n\n" + FormatPlan(pl)
}

// toolSetOf is the set of tool names a plan calls — the execution cap. It is
// non-nil even when empty (a plan with no tool steps offers no tools).
func toolSetOf(pl *plan.Plan) map[string]bool {
	set := make(map[string]bool)
	for _, s := range pl.Steps {
		if s.Type == plan.StepTool && s.Tool != "" {
			set[s.Tool] = true
		}
	}
	return set
}

// planHasToolStep reports whether the plan calls any tool (a pure think/final plan
// has no side effects, so it needs no whole-plan approval).
func planHasToolStep(pl *plan.Plan) bool {
	for _, s := range pl.Steps {
		if s.Type == plan.StepTool {
			return true
		}
	}
	return false
}

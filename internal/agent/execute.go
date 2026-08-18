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
// failure is not fatal, but it is not silent either: see planFallback, which decides
// what a turn may do when no approved plan exists to bound it.
func (a *Agent) runPlanned(ctx context.Context, msgs []llm.Message, input string, userTurnID int64, hits []memory.Hit, out chan<- llm.Chunk) {
	defer close(out)
	a.emitRecallDebug(ctx, out, input, hits)

	// 1. Plan.
	var toolDefs []tools.Def
	if a.tools != nil {
		toolDefs = a.tools.Defs()
	}
	// Give the planner the same recalled memories the executor gets, framed as
	// untrusted DATA — so plans can use what the agent knows (the user's name,
	// preferences) instead of re-discovering it.
	pl, err := a.planner.Plan(ctx, input, fencedMemories(hits), toolDefs)
	if err != nil {
		a.planFallback(ctx, msgs, input, userTurnID, out, err)
		return
	}
	a.lastPlan.Store(pl)
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
			a.finishAnswer(ctx, out, input, userTurnID, fmt.Sprintf(
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
			a.finishAnswer(ctx, out, input, userTurnID, "Plan not approved; I won't proceed.")
			return
		}
	}

	// 4. Execute via the ReAct core. In plan/step modes the offered tools are capped
	//    to the plan's tools, so the model cannot act outside what was approved. How
	//    much the whole-plan approval covers depends on the mode — see below. The cap
	//    is by tool *name*, so a high-risk step must still re-confirm its live args.
	msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: planFollowPrompt(pl)})
	exec := execCtx{}
	switch a.cfg.ApprovalMode {
	case ApprovalPlan:
		// The plan approval covers low/medium-risk steps; high-risk steps (e.g. the
		// shell) still re-prompt with the arguments the executor actually chose,
		// which may differ from those the approved plan displayed.
		exec.allowTools = toolSetOf(pl)
		exec.reapproveAtOrAbove = plan.RiskHigh
	case ApprovalStep:
		// Belt and braces: approve the plan AND re-confirm every risky step live.
		exec.allowTools = toolSetOf(pl)
		exec.reapproveAtOrAbove = plan.RiskLow
	case ApprovalHighRisk:
		// The plan is advisory: no whole-plan prompt, and the per-call policy gate
		// prompts as it would without a planner.
		exec.reapproveAtOrAbove = plan.RiskLow
	}
	a.reactLoop(ctx, msgs, input, userTurnID, out, exec)
}

// planFallback runs the turn when planning failed, under Config.PlannerFallback.
// It owns the rest of the turn (the caller returns straight after) but does NOT
// close out — runPlanned's defer does.
//
// Why this is not just `reactLoop(…, execCtx{})`: the plan is what caps which tools
// the executor may offer (execCtx.allowTools). Falling through to the plain loop
// therefore hands back every tool at the exact moment the mechanism the user opted
// into stopped working — a silent, upward change of the turn's execution contract.
// The policy and per-call approval still apply, so it was never an unrestricted
// bypass; but "still gated" is not "still capped", and only the human can trade one
// for the other. Hence: fail closed by default, and never change the contract
// without either an explicit config or an explicit yes.
func (a *Agent) planFallback(ctx context.Context, msgs []llm.Message, input string, userTurnID int64, out chan<- llm.Chunk, planErr error) {
	mode := a.cfg.PlannerFallback
	a.trace("plan.failed", "err", planErr, "fallback", mode)

	if mode == FallbackAsk {
		req := llm.NewApprovalRequest("(no plan)", fmt.Sprintf(
			"Planning failed: %v\n\nProceed WITHOUT a plan? The tools would no longer be "+
				"capped to an approved set (each call is still policy-gated).", planErr))
		if !a.send(ctx, out, llm.Chunk{Approval: req}) {
			return
		}
		if req.Decision(ctx) {
			mode = FallbackReact
		} else {
			if ctx.Err() != nil {
				return
			}
			mode = FallbackFailClosed
		}
		a.trace("plan.fallback.decided", "mode", mode)
	}

	if mode == FallbackReact {
		// Explicitly chosen (config or a live yes): the pre-v0.22.2 behaviour, but
		// announced rather than assumed.
		a.send(ctx, out, llm.Chunk{Reasoning: fmt.Sprintf(
			"⚠ planning failed (%v) — continuing without a plan; tools are not capped this turn.\n", planErr)})
		a.reactLoop(ctx, msgs, input, userTurnID, out, execCtx{})
		return
	}

	// FallbackFailClosed: answer, but do not act. A non-nil EMPTY allowTools set is
	// what expresses that — toolSpecs offers nothing, so the model cannot request a
	// call it would then be gated on. The turn is still a real turn: it streams,
	// stores and reflects like any other.
	a.send(ctx, out, llm.Chunk{Reasoning: fmt.Sprintf(
		"⚠ planning failed (%v) — answering without tools (planner fallback: fail_closed).\n", planErr)})
	msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: noPlanPrompt})
	a.reactLoop(ctx, msgs, input, userTurnID, out, execCtx{allowTools: map[string]bool{}})
}

// noPlanPrompt tells the model it has no tools this turn, so it explains what it
// cannot do instead of inventing a result it could not obtain — the failure mode a
// tool-less turn actually has.
const noPlanPrompt = "No executable plan could be produced for this turn, so you have NO tools " +
	"available. Answer from what you already know. If the request genuinely requires an action or " +
	"a lookup you cannot perform, say so plainly and suggest what the user could ask next — do not " +
	"pretend to have performed it."

// finishAnswer streams a canned final answer and runs the same learn step
// (short-term + long-term store, then reflection) the ReAct core would — so an
// aborted plan (denied or unapproved) is still a proper, remembered turn. No tools
// ran, so there are no observations to reflect on.
func (a *Agent) finishAnswer(ctx context.Context, out chan<- llm.Chunk, input string, userTurnID int64, answer string) {
	var assistantTurnID int64
	if answer != "" {
		a.send(ctx, out, llm.Chunk{Content: answer})
		a.short.Add(llm.RoleAssistant, answer)
		if m, err := a.store.Remember(ctx, memory.KindTurn, llm.RoleAssistant, answer); err != nil {
			a.trace("store.assistant.error", "err", err)
			a.sendDebug(ctx, out, "store: assistant turn not persisted: %v", err)
		} else {
			assistantTurnID = m.ID
		}
	}
	// Learn off the critical path (Layer 18) — see reactLoop.
	a.enqueueReflect(reflectJob{
		userInput:       input,
		userTurnID:      userTurnID,
		assistantAnswer: answer,
		assistantTurnID: assistantTurnID,
	})
}

// LastPlan returns the most recent plan produced this session, or nil if planning
// is off or no turn has planned yet. The /plan command renders it.
func (a *Agent) LastPlan() *plan.Plan { return a.lastPlan.Load() }

// PlanCommand renders the most recent plan for the /plan slash command, or a hint
// when there is nothing to show.
func (a *Agent) PlanCommand() string {
	pl := a.lastPlan.Load()
	if pl == nil {
		if a.planner == nil {
			return "planning is off — set TALUNOR_PLANNER=1 to enable it"
		}
		return "no plan yet — ask something that requires action"
	}
	return FormatPlan(pl)
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

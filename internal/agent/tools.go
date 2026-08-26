// This file is the seam between the loop and the action layer: which tools a turn
// may offer (toolSpecs, the plan's structural cap) and what happens when the model
// asks for one (runTool, which is where the policy decides). Split out of agent.go
// in v0.22.5 — same package, same code, no behaviour change.

package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/plan"
)

// runTool runs one tool call after consulting the policy. It wraps the call as a
// one-step plan and asks a.policy whether it may run: a policy error or a denial
// fails closed (the model observes the refusal and can react); a step needing
// approval pauses for a human y/n (deny/cancel also become observations). A
// policy may rewrite the step (Decision.Modified) before it runs. A whole-plan
// approval can cover lower-risk steps (exec.reapproveAtOrAbove), but a step at or
// above that risk still re-prompts with its *live* arguments; a policy denial
// always holds. It returns the observation and done=true if the context was
// cancelled while waiting (the caller should stop).
func (a *Agent) runTool(ctx context.Context, out chan<- llm.Chunk, tc llm.ToolCall, exec execCtx) (obs string, done bool) {
	p := plan.NewToolCallPlan(tc.Name, json.RawMessage(tc.Args))
	step := p.Steps[0]

	d, err := a.policy.Evaluate(ctx, p, step)
	if err != nil {
		// A policy that cannot decide does not get to run the tool.
		a.trace("policy.error", "name", tc.Name, "err", err)
		return fmt.Sprintf("error: policy evaluation failed, tool not run: %v", err), false
	}
	if d.Denied() {
		a.trace("policy.deny", "name", tc.Name, "reason", d.Reason)
		return fmt.Sprintf("error: policy denied this tool call (%s)", d.Reason), false
	}

	// A policy may rewrite the step before it runs (e.g. force a safer argument
	// set). The default policies leave Modified nil.
	name, args := tc.Name, step.Arguments
	if d.Modified != nil {
		if d.Modified.Tool != "" {
			name = d.Modified.Tool
		}
		args = d.Modified.Arguments
		a.trace("policy.modify", "name", name, "reason", d.Reason)
	}

	// A rewrite that SUBSTITUTES A DIFFERENT TOOL re-opens both questions the code
	// above just answered — is this tool inside the plan's cap, and what is it worth
	// on the risk scale? Neither answer travels with Decision.Modified: the cap is
	// enforced at offer time (toolSpecs) and the RiskLevel describes the tool the
	// model asked for, not the one now about to run. So both are asked again.
	//
	// This is Lesson 14's defect at a different altitude. v0.13.2 fixed an approval
	// that bound the tool NAME but not the ARGUMENTS; this bound neither once the
	// policy swapped the tool underneath it.
	if name != tc.Name {
		if exec.allowTools != nil && !exec.allowTools[name] {
			a.trace("policy.modify.blocked", "from", tc.Name, "to", name, "reason", "outside the approved plan")
			return fmt.Sprintf("error: the policy rewrote this call to %q, which the approved plan does not allow", name), false
		}
		sub := plan.NewToolCallPlan(name, args)
		d2, err := a.policy.Evaluate(ctx, sub, sub.Steps[0])
		if err != nil {
			a.trace("policy.error", "name", name, "err", err)
			return fmt.Sprintf("error: policy evaluation failed, tool not run: %v", err), false
		}
		if d2.Denied() {
			a.trace("policy.deny", "name", name, "reason", d2.Reason)
			return fmt.Sprintf("error: policy denied this tool call (%s)", d2.Reason), false
		}
		// One rewrite only. A policy that keeps redirecting does not get to run
		// anything — there would be no stable effect left to approve.
		if d2.Modified != nil && d2.Modified.Tool != "" && d2.Modified.Tool != name {
			a.trace("policy.modify.blocked", "from", name, "to", d2.Modified.Tool, "reason", "second rewrite")
			return "error: the policy rewrote this tool call twice, tool not run", false
		}
		if d2.Modified != nil {
			args = d2.Modified.Arguments
		}
		d = d2 // risk and approval now describe the tool that will ACTUALLY run.
	}

	if d.NeedsApproval() && d.RiskLevel >= exec.reapproveAtOrAbove {
		req := llm.NewApprovalRequest(name, string(args))
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
	return a.tools.Execute(ctx, name, args), false
}

// toolSpecs converts the registry's definitions into the provider's tool specs.
// toolSpecs renders the registry's tools as LLM tool specs. When allow is non-nil
// only tools whose name is in it are offered — the planner's structural cap: the
// model cannot call a tool the approved plan didn't name because it never sees it.
func (a *Agent) toolSpecs(allow map[string]bool) []llm.ToolSpec {
	specs := make([]llm.ToolSpec, 0, a.tools.Len())
	for _, d := range a.tools.Defs() {
		if allow != nil && !allow[d.Name] {
			continue
		}
		specs = append(specs, llm.ToolSpec{Name: d.Name, Description: d.Description, Parameters: d.Parameters})
	}
	return specs
}

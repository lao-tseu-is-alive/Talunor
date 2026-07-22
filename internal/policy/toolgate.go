package policy

import (
	"context"

	"github.com/lao-tseu-is-alive/Talunor/internal/plan"
	"github.com/lao-tseu-is-alive/Talunor/internal/tools"
)

// ToolLookup resolves a tool by name. It matches tools.Registry.Get, so a
// registry satisfies it directly (r.Get), but a func keeps ToolGatePolicy
// testable with a fake.
type ToolLookup func(name string) (tools.Tool, bool)

// ToolGatePolicy is the default policy. It derives its verdict from each tool's
// own approval interfaces, so it reproduces exactly the behaviour Talunor had
// before the policy layer existed:
//
//   - a tool implementing tools.ApprovableFor decides per-call from its
//     arguments (web_fetch waves through an allowlisted host) → medium risk
//     when it wants approval, low when it does not;
//   - otherwise a tool implementing tools.Approvable is an all-or-nothing gate
//     (bash always prompts) → high risk;
//   - a tool implementing neither, or an unknown tool, runs freely → low risk.
//
// It never denies: it only distinguishes "run automatically" from "ask a human".
// Denials come from a richer policy such as RuleEnginePolicy.
type ToolGatePolicy struct {
	lookup ToolLookup
}

// NewToolGate builds a ToolGatePolicy backed by lookup (typically a
// *tools.Registry's Get method).
func NewToolGate(lookup ToolLookup) *ToolGatePolicy {
	return &ToolGatePolicy{lookup: lookup}
}

// Evaluate consults the step's tool for its approval requirement. Non-tool steps
// (think/final) carry no side effect and are always allowed at low risk.
func (p *ToolGatePolicy) Evaluate(_ context.Context, _ *plan.Plan, step plan.PlanStep) (Decision, error) {
	if step.Type != plan.StepTool {
		return Decision{Allowed: true, RiskLevel: plan.RiskLow, Reason: "non-tool step"}, nil
	}
	t, ok := p.lookup(step.Tool)
	if !ok {
		// Unknown tool: match the old needsApproval, which did not gate it.
		// The registry surfaces the "unknown tool" error at execution time.
		return Decision{Allowed: true, RiskLevel: plan.RiskLow, Reason: "unknown tool"}, nil
	}
	// Fine-grained per-argument gate (e.g. web_fetch's allowlist) takes
	// precedence over the coarse one, mirroring agent.needsApproval.
	if af, ok := t.(tools.ApprovableFor); ok {
		if af.RequiresApprovalForArgs(step.Arguments) {
			return Decision{Allowed: true, RiskLevel: plan.RiskMedium, Reason: "tool requires approval for these arguments"}, nil
		}
		return Decision{Allowed: true, RiskLevel: plan.RiskLow, Reason: "tool waved through by its own rule"}, nil
	}
	if ap, ok := t.(tools.Approvable); ok && ap.RequiresApproval() {
		return Decision{Allowed: true, RiskLevel: plan.RiskHigh, Reason: "tool always requires approval"}, nil
	}
	return Decision{Allowed: true, RiskLevel: plan.RiskLow, Reason: "tool needs no approval"}, nil
}

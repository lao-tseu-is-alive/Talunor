// Package policy decides whether Talunor may take an action before it runs.
//
// It generalises the earlier per-tool approval interfaces (tools.Approvable /
// tools.ApprovableFor) into a single question the agent asks about a planned
// step: given the plan and the step, is it allowed, and how risky is it? A
// Policy answers with a Decision; the agent maps that Decision onto its existing
// behaviour — deny (fail closed), ask a human, or run automatically.
//
// Three implementations ship:
//
//   - AllowAllPolicy: permits everything at low risk. For tests and a
//     deliberately permissive mode.
//   - ToolGatePolicy: the default. It consults each tool's own Approvable /
//     ApprovableFor interfaces, exactly reproducing pre-policy behaviour
//     (bash always prompts; web_fetch prompts unless the host is allowlisted).
//   - RuleEnginePolicy: data-driven. It reads a YAML rule file (TALUNOR_POLICY)
//     so an operator can allow/prompt/deny per tool without recompiling.
//
// The Decision→behaviour mapping lives here (Denied / NeedsApproval), so every
// caller interprets a Decision the same way.
package policy

import (
	"context"

	"github.com/lao-tseu-is-alive/Talunor/internal/plan"
)

// Decision is a Policy's verdict on a single planned step.
type Decision struct {
	// Allowed reports whether the step may run at all. False means deny: the
	// agent turns it into an observation and never executes the step.
	Allowed bool
	// Reason is a short human-readable justification, surfaced in traces and
	// (for denials) fed back to the model.
	Reason string
	// Modified, when non-nil, replaces the step before it runs — letting a
	// policy rewrite arguments (e.g. force a dry-run flag). The default
	// policies leave it nil; it is here so richer policies can use it.
	Modified *plan.PlanStep
	// RiskLevel is how dangerous the action is judged to be. At or above
	// ApprovalThreshold an allowed step still requires human approval.
	RiskLevel plan.RiskLevel
}

// ApprovalThreshold is the risk level at or above which an *allowed* step still
// needs explicit human approval. Below it, the step runs automatically. Denied
// steps never run regardless of risk.
const ApprovalThreshold = plan.RiskMedium

// Denied reports whether the step must not run.
func (d Decision) Denied() bool { return !d.Allowed }

// NeedsApproval reports whether an allowed step must be confirmed by a human
// before running. This is the single place the risk→approval rule is applied.
func (d Decision) NeedsApproval() bool {
	return d.Allowed && d.RiskLevel >= ApprovalThreshold
}

// Policy decides whether a planned step may run. Evaluate is given the whole
// plan (for context — later policies may reason across steps) and the specific
// step under consideration. An error is a policy *failure* (e.g. a malformed
// rule file at evaluation time), distinct from a deny Decision, and the agent
// fails closed on it.
type Policy interface {
	Evaluate(ctx context.Context, p *plan.Plan, step plan.PlanStep) (Decision, error)
}

// AllowAllPolicy permits every step at low risk. Useful in tests and as an
// explicitly permissive mode. It never denies and never asks for approval.
type AllowAllPolicy struct{}

// Evaluate always allows the step at RiskLow.
func (AllowAllPolicy) Evaluate(context.Context, *plan.Plan, plan.PlanStep) (Decision, error) {
	return Decision{Allowed: true, RiskLevel: plan.RiskLow, Reason: "allow-all policy"}, nil
}

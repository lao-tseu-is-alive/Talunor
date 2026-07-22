package policy

import (
	"context"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/lao-tseu-is-alive/Talunor/internal/plan"
)

// Action is what a rule decides for a matching tool call.
type Action string

const (
	// ActionAllow runs the step automatically (low risk).
	ActionAllow Action = "allow"
	// ActionPrompt runs the step only after human approval (medium risk).
	ActionPrompt Action = "prompt"
	// ActionDeny refuses the step; the model observes the refusal (high risk).
	ActionDeny Action = "deny"
)

// Rule matches a tool by name and says what to do. Tool "*" (or empty) matches
// any tool; it is how the `default` rule and catch-all rules are written.
type Rule struct {
	Tool   string `yaml:"tool"`
	Action Action `yaml:"action"`
	// Reason overrides the generic justification in the resulting Decision.
	Reason string `yaml:"reason,omitempty"`
}

func (r Rule) matches(tool string) bool {
	return r.Tool == tool || r.Tool == "*" || r.Tool == ""
}

// decision maps the rule's action onto a Decision. The risk level is implied by
// the action (allow→low, prompt→medium, deny→high), which is what the agent's
// ApprovalThreshold then acts on.
func (r Rule) decision() (Decision, error) {
	switch r.Action {
	case ActionAllow:
		return Decision{Allowed: true, RiskLevel: plan.RiskLow, Reason: r.reasonOr("allowed by policy rule")}, nil
	case ActionPrompt:
		return Decision{Allowed: true, RiskLevel: plan.RiskMedium, Reason: r.reasonOr("policy rule requires approval")}, nil
	case ActionDeny:
		return Decision{Allowed: false, RiskLevel: plan.RiskHigh, Reason: r.reasonOr("denied by policy rule")}, nil
	default:
		return Decision{}, fmt.Errorf("policy: invalid action %q for tool %q", r.Action, r.Tool)
	}
}

func (r Rule) reasonOr(fallback string) string {
	if r.Reason != "" {
		return r.Reason
	}
	return fallback
}

// ruleFile is the on-disk YAML shape.
type ruleFile struct {
	Default Rule   `yaml:"default"`
	Rules   []Rule `yaml:"rules"`
}

// RuleEnginePolicy decides from a data-driven rule set. The first rule whose
// tool matches wins; if none match, the default rule applies. Matching is by
// tool name only — argument-level conditions (e.g. a per-host allowlist) are a
// deliberate future extension; today, delegate that to ToolGatePolicy.
type RuleEnginePolicy struct {
	def   Rule
	rules []Rule
}

// NewRuleEngine builds a policy from an explicit default and rule list,
// validating every action up front so Evaluate cannot fail on a bad action. An
// empty default action falls back to ActionPrompt (fail safe: ask a human).
func NewRuleEngine(def Rule, rules []Rule) (*RuleEnginePolicy, error) {
	if def.Action == "" {
		def.Action = ActionPrompt
	}
	if _, err := def.decision(); err != nil {
		return nil, fmt.Errorf("default rule: %w", err)
	}
	for i, r := range rules {
		if r.Action == "" {
			return nil, fmt.Errorf("rule %d (tool %q): action is required", i, r.Tool)
		}
		if _, err := r.decision(); err != nil {
			return nil, fmt.Errorf("rule %d: %w", i, err)
		}
	}
	return &RuleEnginePolicy{def: def, rules: rules}, nil
}

// ParseRules builds a RuleEnginePolicy from YAML bytes.
func ParseRules(data []byte) (*RuleEnginePolicy, error) {
	var f ruleFile
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("policy: parse rules: %w", err)
	}
	return NewRuleEngine(f.Default, f.Rules)
}

// LoadRules reads and parses a YAML rule file.
func LoadRules(path string) (*RuleEnginePolicy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("policy: read rules: %w", err)
	}
	return ParseRules(data)
}

// Evaluate applies the first matching rule (or the default). Non-tool steps
// carry no side effect and are always allowed at low risk.
func (p *RuleEnginePolicy) Evaluate(_ context.Context, _ *plan.Plan, step plan.PlanStep) (Decision, error) {
	if step.Type != plan.StepTool {
		return Decision{Allowed: true, RiskLevel: plan.RiskLow, Reason: "non-tool step"}, nil
	}
	for _, r := range p.rules {
		if r.matches(step.Tool) {
			return r.decision()
		}
	}
	return p.def.decision()
}

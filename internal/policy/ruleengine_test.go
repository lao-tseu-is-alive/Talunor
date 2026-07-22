package policy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/plan"
)

const sampleRules = `
# first match wins; unmatched tools fall through to default
default:
  action: prompt
rules:
  - tool: calculator
    action: allow
  - tool: bash
    action: prompt
    reason: shell has side effects
  - tool: rm_rf
    action: deny
    reason: never
`

func TestRuleEngineEvaluate(t *testing.T) {
	pol, err := ParseRules([]byte(sampleRules))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	tests := []struct {
		tool         string
		wantAllowed  bool
		wantApproval bool
		wantReason   string
	}{
		{"calculator", true, false, "allowed by policy rule"},
		{"bash", true, true, "shell has side effects"},
		{"rm_rf", false, false, "never"},
		{"unlisted", true, true, "policy rule requires approval"}, // default: prompt
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			d, err := pol.Evaluate(context.Background(), nil, toolStep(tt.tool))
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if d.Allowed != tt.wantAllowed || d.NeedsApproval() != tt.wantApproval {
				t.Errorf("tool %q → %+v, want allowed=%v approval=%v", tt.tool, d, tt.wantAllowed, tt.wantApproval)
			}
			if d.Reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", d.Reason, tt.wantReason)
			}
		})
	}
}

func TestRuleEngineNonToolStep(t *testing.T) {
	pol, err := ParseRules([]byte(sampleRules))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	step := plan.PlanStep{ID: "s1", Type: plan.StepThink, Rationale: "reason"}
	d, err := pol.Evaluate(context.Background(), nil, step)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if !d.Allowed || d.NeedsApproval() {
		t.Errorf("non-tool step should auto-allow, got %+v", d)
	}
}

func TestRuleEngineDefaultsToPrompt(t *testing.T) {
	// A rule file with no default should fall back to prompt (fail safe).
	pol, err := ParseRules([]byte("rules:\n  - tool: calculator\n    action: allow\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d, _ := pol.Evaluate(context.Background(), nil, toolStep("anything"))
	if !d.NeedsApproval() {
		t.Errorf("missing default should prompt, got %+v", d)
	}
}

func TestRuleEngineWildcard(t *testing.T) {
	pol, err := ParseRules([]byte("rules:\n  - tool: '*'\n    action: deny\n    reason: locked down\n"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d, _ := pol.Evaluate(context.Background(), nil, toolStep("whatever"))
	if !d.Denied() {
		t.Errorf("wildcard deny should deny everything, got %+v", d)
	}
}

func TestRuleEngineInvalidAction(t *testing.T) {
	_, err := ParseRules([]byte("rules:\n  - tool: bash\n    action: maybe\n"))
	if err == nil || !strings.Contains(err.Error(), "invalid action") {
		t.Fatalf("want invalid action error, got %v", err)
	}
}

func TestRuleEngineMissingAction(t *testing.T) {
	_, err := ParseRules([]byte("rules:\n  - tool: bash\n"))
	if err == nil || !strings.Contains(err.Error(), "action is required") {
		t.Fatalf("want missing action error, got %v", err)
	}
}

func TestLoadRulesFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	if err := os.WriteFile(path, []byte(sampleRules), 0o600); err != nil {
		t.Fatal(err)
	}
	pol, err := LoadRules(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	d, _ := pol.Evaluate(context.Background(), nil, toolStep("rm_rf"))
	if !d.Denied() {
		t.Errorf("rm_rf should be denied, got %+v", d)
	}

	if _, err := LoadRules(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("expected error loading missing file")
	}
}

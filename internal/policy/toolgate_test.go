package policy

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/plan"
	"github.com/lao-tseu-is-alive/Talunor/internal/tools"
)

// --- fake tools implementing the various approval interfaces ---

type plainTool struct{ name string }

func (t plainTool) Name() string                                             { return t.name }
func (t plainTool) Description() string                                      { return "plain" }
func (t plainTool) Schema() json.RawMessage                                  { return json.RawMessage(`{}`) }
func (t plainTool) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }

// coarseTool always requires approval (like bash).
type coarseTool struct{ plainTool }

func (coarseTool) RequiresApproval() bool { return true }

// fineTool decides per-call (like web_fetch's allowlist).
type fineTool struct {
	plainTool
	wants bool
}

func (t fineTool) RequiresApprovalForArgs(json.RawMessage) bool { return t.wants }

func lookupFrom(ts ...tools.Tool) ToolLookup {
	reg := tools.NewRegistry(ts...)
	return reg.Get
}

func TestToolGatePolicy(t *testing.T) {
	lookup := lookupFrom(
		plainTool{name: "calculator"},
		coarseTool{plainTool{name: "bash"}},
		fineTool{plainTool{name: "web_fetch_prompt"}, true},
		fineTool{plainTool{name: "web_fetch_allow"}, false},
	)
	pol := NewToolGate(lookup)

	tests := []struct {
		name         string
		step         plan.PlanStep
		wantAllowed  bool
		wantApproval bool
		wantRisk     plan.RiskLevel
	}{
		{"plain tool auto", toolStep("calculator"), true, false, plan.RiskLow},
		{"coarse tool high", toolStep("bash"), true, true, plan.RiskHigh},
		{"fine tool wants approval", toolStep("web_fetch_prompt"), true, true, plan.RiskMedium},
		{"fine tool waved through", toolStep("web_fetch_allow"), true, false, plan.RiskLow},
		{"unknown tool auto", toolStep("ghost"), true, false, plan.RiskLow},
		{"non-tool step", plan.PlanStep{ID: "s1", Type: plan.StepFinal, Rationale: "answer"}, true, false, plan.RiskLow},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := pol.Evaluate(context.Background(), nil, tt.step)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.Allowed != tt.wantAllowed {
				t.Errorf("Allowed = %v, want %v", d.Allowed, tt.wantAllowed)
			}
			if d.NeedsApproval() != tt.wantApproval {
				t.Errorf("NeedsApproval = %v, want %v (%+v)", d.NeedsApproval(), tt.wantApproval, d)
			}
			if d.RiskLevel != tt.wantRisk {
				t.Errorf("RiskLevel = %v, want %v", d.RiskLevel, tt.wantRisk)
			}
		})
	}
}

func toolStep(tool string) plan.PlanStep {
	return plan.PlanStep{ID: "s1", Type: plan.StepTool, Tool: tool, Rationale: "x"}
}

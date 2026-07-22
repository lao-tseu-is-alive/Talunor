package policy

import (
	"context"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/plan"
)

func TestDecisionMapping(t *testing.T) {
	tests := []struct {
		name         string
		d            Decision
		wantDenied   bool
		wantApproval bool
	}{
		{"denied", Decision{Allowed: false, RiskLevel: plan.RiskHigh}, true, false},
		{"auto low", Decision{Allowed: true, RiskLevel: plan.RiskLow}, false, false},
		{"gate medium", Decision{Allowed: true, RiskLevel: plan.RiskMedium}, false, true},
		{"gate high", Decision{Allowed: true, RiskLevel: plan.RiskHigh}, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.Denied(); got != tt.wantDenied {
				t.Errorf("Denied() = %v, want %v", got, tt.wantDenied)
			}
			if got := tt.d.NeedsApproval(); got != tt.wantApproval {
				t.Errorf("NeedsApproval() = %v, want %v", got, tt.wantApproval)
			}
		})
	}
}

func TestAllowAllPolicy(t *testing.T) {
	step := plan.PlanStep{ID: "s1", Type: plan.StepTool, Tool: "bash", Rationale: "x"}
	d, err := AllowAllPolicy{}.Evaluate(context.Background(), nil, step)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !d.Allowed || d.NeedsApproval() {
		t.Errorf("allow-all should permit without approval, got %+v", d)
	}
}

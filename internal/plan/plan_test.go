package plan

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRiskLevelString(t *testing.T) {
	cases := map[RiskLevel]string{
		RiskLow:       "low",
		RiskMedium:    "medium",
		RiskHigh:      "high",
		RiskLevel(99): "RiskLevel(99)",
	}
	for r, want := range cases {
		if got := r.String(); got != want {
			t.Errorf("RiskLevel(%d).String() = %q, want %q", int(r), got, want)
		}
	}
}

func TestPlanStepValidate(t *testing.T) {
	tests := []struct {
		name    string
		step    PlanStep
		wantErr string // substring; "" means valid
	}{
		{
			name: "valid tool step",
			step: PlanStep{ID: "s1", Type: StepTool, Tool: "calculator", Rationale: "compute"},
		},
		{
			name: "valid think step",
			step: PlanStep{ID: "s1", Type: StepThink, Rationale: "reason about it"},
		},
		{
			name: "valid final step",
			step: PlanStep{ID: "s1", Type: StepFinal, Rationale: "answer"},
		},
		{
			name:    "missing id",
			step:    PlanStep{Type: StepThink, Rationale: "x"},
			wantErr: "id is required",
		},
		{
			name:    "unknown type",
			step:    PlanStep{ID: "s1", Type: "wander", Rationale: "x"},
			wantErr: "unknown type",
		},
		{
			name:    "missing rationale",
			step:    PlanStep{ID: "s1", Type: StepThink},
			wantErr: "rationale is required",
		},
		{
			name:    "tool step without tool",
			step:    PlanStep{ID: "s1", Type: StepTool, Rationale: "x"},
			wantErr: "requires a tool name",
		},
		{
			name:    "non-tool step with tool",
			step:    PlanStep{ID: "s1", Type: StepThink, Tool: "calculator", Rationale: "x"},
			wantErr: "only valid for type",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.step.Validate()
			assertErr(t, err, tt.wantErr)
		})
	}
}

func TestPlanValidate(t *testing.T) {
	tests := []struct {
		name    string
		plan    Plan
		wantErr string
	}{
		{
			name: "valid single step",
			plan: Plan{Goal: "g", Steps: []PlanStep{{ID: "s1", Type: StepFinal, Rationale: "answer"}}},
		},
		{
			name: "valid with resolvable dependency",
			plan: Plan{Goal: "g", Confidence: 0.8, Steps: []PlanStep{
				{ID: "s1", Type: StepTool, Tool: "clock", Rationale: "time"},
				{ID: "s2", Type: StepFinal, Rationale: "answer", DependsOn: []string{"s1"}},
			}},
		},
		{
			name:    "missing goal",
			plan:    Plan{Steps: []PlanStep{{ID: "s1", Type: StepFinal, Rationale: "a"}}},
			wantErr: "goal is required",
		},
		{
			name:    "no steps",
			plan:    Plan{Goal: "g"},
			wantErr: "at least one step",
		},
		{
			name:    "confidence out of range",
			plan:    Plan{Goal: "g", Confidence: 1.5, Steps: []PlanStep{{ID: "s1", Type: StepFinal, Rationale: "a"}}},
			wantErr: "out of range",
		},
		{
			name: "duplicate ids",
			plan: Plan{Goal: "g", Steps: []PlanStep{
				{ID: "s1", Type: StepThink, Rationale: "a"},
				{ID: "s1", Type: StepFinal, Rationale: "b"},
			}},
			wantErr: "duplicate step id",
		},
		{
			name:    "invalid step propagates",
			plan:    Plan{Goal: "g", Steps: []PlanStep{{ID: "s1", Type: StepTool, Rationale: "a"}}},
			wantErr: "requires a tool name",
		},
		{
			name: "depends on itself",
			plan: Plan{Goal: "g", Steps: []PlanStep{
				{ID: "s1", Type: StepFinal, Rationale: "a", DependsOn: []string{"s1"}},
			}},
			wantErr: "depends on itself",
		},
		{
			name: "depends on unknown step",
			plan: Plan{Goal: "g", Steps: []PlanStep{
				{ID: "s1", Type: StepFinal, Rationale: "a", DependsOn: []string{"ghost"}},
			}},
			wantErr: "unknown step",
		},
		{
			name: "dependency cycle",
			plan: Plan{Goal: "g", Steps: []PlanStep{
				{ID: "s1", Type: StepThink, Rationale: "a", DependsOn: []string{"s2"}},
				{ID: "s2", Type: StepFinal, Rationale: "b", DependsOn: []string{"s1"}},
			}},
			wantErr: "dependency cycle",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.plan.Validate()
			assertErr(t, err, tt.wantErr)
		})
	}
}

func TestNewToolCallPlan(t *testing.T) {
	args := json.RawMessage(`{"expression":"2+2"}`)
	p := NewToolCallPlan("calculator", args)
	if err := p.Validate(); err != nil {
		t.Fatalf("wrapped plan should be valid: %v", err)
	}
	if len(p.Steps) != 1 {
		t.Fatalf("want 1 step, got %d", len(p.Steps))
	}
	s := p.Steps[0]
	if s.Type != StepTool || s.Tool != "calculator" {
		t.Errorf("step = {%q, %q}, want {tool, calculator}", s.Type, s.Tool)
	}
	if string(s.Arguments) != string(args) {
		t.Errorf("arguments = %s, want %s", s.Arguments, args)
	}
	if s.Rationale == "" {
		t.Error("rationale must be non-empty so the plan validates")
	}
}

// assertErr checks err against a wanted substring ("" means no error expected).
func assertErr(t *testing.T, err error, want string) {
	t.Helper()
	switch {
	case want == "" && err != nil:
		t.Fatalf("unexpected error: %v", err)
	case want != "" && err == nil:
		t.Fatalf("expected error containing %q, got nil", want)
	case want != "" && !strings.Contains(err.Error(), want):
		t.Fatalf("error %q does not contain %q", err.Error(), want)
	}
}

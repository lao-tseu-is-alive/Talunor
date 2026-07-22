package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/plan"
	"github.com/lao-tseu-is-alive/Talunor/internal/tools"
)

func planToolDefs() []tools.Def {
	return tools.NewRegistry(tools.Calculator{}, tools.Clock{}).Defs()
}

const validPlanJSON = `{"goal":"add two numbers","confidence":0.9,"steps":[` +
	`{"id":"s1","type":"tool","tool":"calculator","arguments":{"expression":"2+2"},"rationale":"compute the sum"},` +
	`{"id":"s2","type":"final","rationale":"report the result"}]}`

func TestLLMPlannerHappyPath(t *testing.T) {
	prov := &scriptedProvider{steps: [][]llm.Chunk{{{Content: validPlanJSON}}}}
	p := NewLLMPlanner(prov, llm.Options{})
	pl, err := p.Plan(context.Background(), "what is 2+2?", "", planToolDefs())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(pl.Steps) != 2 || pl.Steps[0].Tool != "calculator" {
		t.Fatalf("unexpected plan: %+v", pl)
	}
	if pl.Steps[len(pl.Steps)-1].Type != plan.StepFinal {
		t.Errorf("plan should end with a final step")
	}
}

func TestLLMPlannerRetriesThenSucceeds(t *testing.T) {
	prov := &scriptedProvider{steps: [][]llm.Chunk{
		{{Content: "sorry, here is my reasoning but no json"}}, // attempt 1: unparseable
		{{Content: validPlanJSON}},                             // attempt 2: valid
	}}
	p := NewLLMPlanner(prov, llm.Options{})
	pl, err := p.Plan(context.Background(), "what is 2+2?", "", planToolDefs())
	if err != nil {
		t.Fatalf("plan should succeed on retry: %v", err)
	}
	if pl.Goal == "" {
		t.Error("expected a goal")
	}
	if prov.call != 2 {
		t.Errorf("expected 2 provider calls (retry), got %d", prov.call)
	}
}

func TestLLMPlannerFailsAfterMaxAttempts(t *testing.T) {
	prov := &scriptedProvider{steps: [][]llm.Chunk{
		{{Content: "nope"}},
		{{Content: "still nope"}},
	}}
	p := NewLLMPlanner(prov, llm.Options{})
	if _, err := p.Plan(context.Background(), "go", "", planToolDefs()); err == nil {
		t.Fatal("expected failure after max attempts")
	}
}

func TestDecodePlan(t *testing.T) {
	known := knownToolSet(planToolDefs())
	tests := []struct {
		name    string
		raw     string
		wantErr string // "" = ok
	}{
		{"clean json", validPlanJSON, ""},
		{
			name: "wrapped in prose and fences",
			raw:  "Sure! Here is the plan:\n```json\n" + validPlanJSON + "\n```\nHope that helps.",
		},
		{"no json", "there is no plan here", "no JSON object"},
		{"malformed json", `{"goal":"x","steps":[`, "not valid JSON"},
		{
			name:    "unknown tool",
			raw:     `{"goal":"x","steps":[{"id":"s1","type":"tool","tool":"nuke","rationale":"why"},{"id":"s2","type":"final","rationale":"end"}]}`,
			wantErr: "unknown tool",
		},
		{
			name:    "missing final step",
			raw:     `{"goal":"x","steps":[{"id":"s1","type":"think","rationale":"hmm"}]}`,
			wantErr: "must end with a final step",
		},
		{
			name:    "invalid per plan.Validate (missing rationale)",
			raw:     `{"goal":"x","steps":[{"id":"s1","type":"final"}]}`,
			wantErr: "rationale is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pl, err := decodePlan(tt.raw, known)
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr == "" && pl == nil:
				t.Fatal("expected a plan")
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

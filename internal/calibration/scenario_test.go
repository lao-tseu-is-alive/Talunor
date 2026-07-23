package calibration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validSuiteYAML = `
suite: core
scenarios:
  - id: arithmetic-chain
    category: arithmetic
    risk: low
    runs: 3
    turns:
      - user: "What is 137 * 4? Answer with only the number."
        expect: { equals: "548" }
      - user: "Divide that by 2."
        expect: { number: { equals: 274 } }
  - id: json-format
    category: format
    turns:
      - user: "Reply with a JSON object {\"ok\": true} and nothing else."
        expect: { json_valid: true }
`

func TestParseValidSuite(t *testing.T) {
	s, err := Parse([]byte(validSuiteYAML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Name != "core" || len(s.Scenarios) != 2 {
		t.Fatalf("unexpected suite: %+v", s)
	}
	sc := s.Scenarios[0]
	if sc.ID != "arithmetic-chain" || sc.Runs != 3 || len(sc.Turns) != 2 {
		t.Errorf("scenario 0 mismatch: %+v", sc)
	}
	if sc.Turns[0].Expect.Equals == nil || *sc.Turns[0].Expect.Equals != "548" {
		t.Errorf("turn 0 assert not parsed: %+v", sc.Turns[0].Expect)
	}
}

func TestSuiteValidate(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string // "" = valid
	}{
		{"valid", validSuiteYAML, ""},
		{
			name:    "no scenarios",
			yaml:    "suite: empty\nscenarios: []\n",
			wantErr: "no scenarios",
		},
		{
			name:    "missing id",
			yaml:    "scenarios:\n  - category: x\n    turns:\n      - user: hi\n        expect: { equals: hi }\n",
			wantErr: "id is required",
		},
		{
			name: "duplicate id",
			yaml: "scenarios:\n" +
				"  - id: dup\n    turns:\n      - user: a\n        expect: { equals: a }\n" +
				"  - id: dup\n    turns:\n      - user: b\n        expect: { equals: b }\n",
			wantErr: "duplicate scenario id",
		},
		{
			name:    "no turns",
			yaml:    "scenarios:\n  - id: x\n    turns: []\n",
			wantErr: "at least one turn",
		},
		{
			name:    "empty user",
			yaml:    "scenarios:\n  - id: x\n    turns:\n      - user: \"\"\n        expect: { equals: a }\n",
			wantErr: "user message is required",
		},
		{
			name:    "empty assert",
			yaml:    "scenarios:\n  - id: x\n    turns:\n      - user: hi\n        expect: {}\n",
			wantErr: "checks nothing",
		},
		{
			name:    "bad risk",
			yaml:    "scenarios:\n  - id: x\n    risk: critical\n    turns:\n      - user: hi\n        expect: { equals: a }\n",
			wantErr: "unknown risk",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestTooManyTurns(t *testing.T) {
	var b strings.Builder
	b.WriteString("scenarios:\n  - id: long\n    turns:\n")
	for i := 0; i < maxTurns+1; i++ {
		b.WriteString("      - user: hi\n        expect: { equals: hi }\n")
	}
	if _, err := Parse([]byte(b.String())); err == nil || !strings.Contains(err.Error(), "too many turns") {
		t.Fatalf("want too-many-turns error, got %v", err)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "suite.yaml")
	if err := os.WriteFile(path, []byte(validSuiteYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(s.Scenarios) != 2 {
		t.Errorf("want 2 scenarios, got %d", len(s.Scenarios))
	}
	if _, err := Load(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Error("expected error loading missing file")
	}
}

func TestLoadReader(t *testing.T) {
	s, err := LoadReader(strings.NewReader(validSuiteYAML))
	if err != nil {
		t.Fatalf("load reader: %v", err)
	}
	if len(s.Scenarios) != 2 {
		t.Errorf("want 2 scenarios, got %d", len(s.Scenarios))
	}
}

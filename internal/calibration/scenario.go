// Package calibration measures how reliably an LLM provider follows a fixed set
// of scenarios whose correct answers are known in advance — a deterministic
// "canary" for a model's factual accuracy, format compliance, and consistency, so
// that silent quality drift (a provider update, a cheaper "flash" variant, a bad
// day) is caught before users feel it.
//
// Two invariants define the package:
//
//   - Every verifier is DETERMINISTIC (see Assert). A calibration harness that
//     judged answers with another LLM would inherit the unreliability it exists to
//     measure. Ground truth is machine-checkable or it is not in a scenario.
//   - The loader is SOURCE-AGNOSTIC: Parse takes YAML bytes, wherever they come
//     from — a plaintext seed suite, a private file, or a decrypted blob. So an
//     encrypted private suite (to resist test-set memorisation) drops in later
//     without touching the core.
//
// The scenarios carry no session memory: the runner builds each conversation from
// the scenario's turns alone, so the model under test is always evaluated
// clean-room. The same suite therefore runs identically against any llm.Provider —
// local or hosted — which is the point: one deterministic yardstick, many models.
package calibration

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// Risk labels for a scenario. Informational in this layer (they let a later layer
// have a policy consult a model's calibration on, say, high-risk categories).
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// maxTurns bounds a scenario at 1..5 interactions — long enough to surface
// in-context degradation, short enough to keep ground truth authorable.
const maxTurns = 5

// Suite is a named set of scenarios loaded from one YAML document.
type Suite struct {
	Name      string     `yaml:"suite"`
	Scenarios []Scenario `yaml:"scenarios"`
}

// Scenario is a fixed, replayable conversation with a per-turn ground-truth
// assertion. It holds no session memory: the runner constructs the conversation
// from Turns (and the optional System prompt) alone.
type Scenario struct {
	ID          string  `yaml:"id"`
	Category    string  `yaml:"category,omitempty"`    // for per-category aggregation
	Risk        string  `yaml:"risk,omitempty"`        // low|medium|high (informational)
	Runs        int     `yaml:"runs,omitempty"`        // repeats to measure consistency; 0 → runner default
	Temperature float64 `yaml:"temperature,omitempty"` // optional per-scenario override
	System      string  `yaml:"system,omitempty"`      // optional clean-room system prompt
	Turns       []Turn  `yaml:"turns"`
}

// Turn is one user message and the deterministic assertion its assistant reply
// must satisfy.
type Turn struct {
	User   string `yaml:"user"`
	Expect Assert `yaml:"expect"`
}

// Parse loads and validates a Suite from YAML bytes. It is the source-agnostic
// core: the bytes may be plaintext or already-decrypted — Parse does not care
// where they came from.
func Parse(data []byte) (*Suite, error) {
	var s Suite
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("calibration: parse suite: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// Load reads and parses a suite file.
func Load(path string) (*Suite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("calibration: read suite %q: %w", path, err)
	}
	return Parse(data)
}

// LoadReader reads and parses a suite from r.
func LoadReader(r io.Reader) (*Suite, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("calibration: read suite: %w", err)
	}
	return Parse(data)
}

// Validate reports whether the suite is well-formed: at least one scenario, unique
// non-empty ids, and every scenario valid.
func (s *Suite) Validate() error {
	if len(s.Scenarios) == 0 {
		return fmt.Errorf("calibration: suite has no scenarios")
	}
	seen := make(map[string]bool, len(s.Scenarios))
	for i := range s.Scenarios {
		sc := &s.Scenarios[i]
		if sc.ID == "" {
			return fmt.Errorf("calibration: scenario %d: id is required", i)
		}
		if seen[sc.ID] {
			return fmt.Errorf("calibration: duplicate scenario id %q", sc.ID)
		}
		seen[sc.ID] = true
		if err := sc.validate(); err != nil {
			return fmt.Errorf("calibration: scenario %q: %w", sc.ID, err)
		}
	}
	return nil
}

func (sc *Scenario) validate() error {
	switch sc.Risk {
	case "", RiskLow, RiskMedium, RiskHigh:
	default:
		return fmt.Errorf("unknown risk %q (want low|medium|high)", sc.Risk)
	}
	if sc.Runs < 0 {
		return fmt.Errorf("runs must be >= 0")
	}
	if sc.Temperature < 0 {
		return fmt.Errorf("temperature must be >= 0")
	}
	if len(sc.Turns) == 0 {
		return fmt.Errorf("at least one turn is required")
	}
	if len(sc.Turns) > maxTurns {
		return fmt.Errorf("too many turns (%d > %d)", len(sc.Turns), maxTurns)
	}
	for i := range sc.Turns {
		t := &sc.Turns[i]
		if t.User == "" {
			return fmt.Errorf("turn %d: user message is required", i+1)
		}
		if err := t.Expect.validate(); err != nil {
			return fmt.Errorf("turn %d: %w", i+1, err)
		}
	}
	return nil
}

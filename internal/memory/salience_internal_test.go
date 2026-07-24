package memory

import (
	"math"
	"testing"
	"time"
)

// These are pure-function tests: no database, no extensions, so they run in CI
// even without `make deps`.

func TestEffectiveSalienceDecay(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	half := 24 * time.Hour

	cases := []struct {
		name     string
		salience float64
		ref      time.Time
		want     float64
	}{
		{"no age keeps full salience", 2.0, now, 2.0},
		{"one half-life halves it", 2.0, now.Add(-half), 1.0},
		{"two half-lives quarter it", 2.0, now.Add(-2 * half), 0.5},
		{"future ref (clock skew) keeps full", 2.0, now.Add(half), 2.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := effectiveSalience(c.salience, c.ref, now, half)
			if math.Abs(got-c.want) > 1e-9 {
				t.Errorf("effectiveSalience = %v, want %v", got, c.want)
			}
		})
	}

	// A non-positive half-life or a zero ref means "no decay".
	if got := effectiveSalience(1.5, now.Add(-1000*time.Hour), now, 0); got != 1.5 {
		t.Errorf("zero half-life should not decay: got %v", got)
	}
	if got := effectiveSalience(1.5, time.Time{}, now, half); got != 1.5 {
		t.Errorf("zero ref should not decay: got %v", got)
	}
}

func TestEvidenceCredibility(t *testing.T) {
	// Independent evidence (user, tool) fully counts toward confidence; the model
	// echoing its own inference counts for nothing (the echo-chamber guard); an
	// unclassified source is discounted.
	cases := map[Provenance]float64{
		ProvenanceUserStated:    1.0,
		ProvenanceToolObserved:  1.0,
		ProvenanceModelInferred: 0.0,
		ProvenanceUnspecified:   0.5,
	}
	for p, want := range cases {
		if got := EvidenceCredibility(p); got != want {
			t.Errorf("EvidenceCredibility(%s) = %v, want %v", p, got, want)
		}
	}
}

func TestReinforcedConfidence(t *testing.T) {
	// Zero (or negative) gain leaves confidence untouched.
	if got := reinforcedConfidence(0.9, 0); got != 0.9 {
		t.Errorf("gain 0 changed confidence: %v", got)
	}

	// Repetition raises confidence monotonically, but with diminishing returns and
	// never past the ceiling.
	c0 := 0.5
	c1 := reinforcedConfidence(c0, 0.34)
	c2 := reinforcedConfidence(c1, 0.34)
	if !(c1 > c0 && c2 > c1) {
		t.Errorf("confidence not monotonically increasing: %v -> %v -> %v", c0, c1, c2)
	}
	if (c1 - c0) <= (c2 - c1) {
		t.Errorf("expected diminishing returns: first step %v, second step %v", c1-c0, c2-c1)
	}
	if c2 > confidenceCeiling {
		t.Errorf("confidence %v exceeded ceiling %v", c2, confidenceCeiling)
	}

	// An already-certain fact does not creep past the ceiling.
	if got := reinforcedConfidence(confidenceCeiling, 0.9); got != confidenceCeiling {
		t.Errorf("at-ceiling confidence moved: %v", got)
	}
}

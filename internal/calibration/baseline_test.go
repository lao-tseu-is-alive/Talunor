package calibration

import (
	"context"
	"path/filepath"
	"testing"
)

// report builds a small report by running a suite against fixed replies.
func reportFrom(t *testing.T, replies []string, scenarios []Scenario) *Report {
	t.Helper()
	return Run(context.Background(), newFake(replies...), &Suite{Name: "s", Scenarios: scenarios}, Options{})
}

func TestBaselineRoundTrip(t *testing.T) {
	rep := reportFrom(t, []string{"2"}, []Scenario{
		oneTurn("a1", "arithmetic", "1+1?", Assert{Equals: sp("2")}),
	})
	base := rep.AsBaseline()

	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := base.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	loaded, err := LoadBaseline(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Overall != base.Overall || loaded.Scenarios["a1"] != 1.0 {
		t.Errorf("round-trip mismatch: %+v", loaded)
	}
}

func TestDriftDetectsRegression(t *testing.T) {
	scenarios := []Scenario{
		oneTurn("a1", "arithmetic", "1+1?", Assert{Equals: sp("2")}),
		oneTurn("a2", "arithmetic", "2+2?", Assert{Equals: sp("4")}),
	}
	// Baseline: both pass (overall 1.0).
	base := reportFrom(t, []string{"2", "4"}, scenarios).AsBaseline()
	// Later: a2 now wrong (overall 0.5, a2 regresses).
	later := reportFrom(t, []string{"2", "5"}, scenarios)

	d := later.Diff(base, 0.05)
	if !d.Regressed() {
		t.Fatal("expected a regression")
	}
	if d.OverallDelta >= 0 {
		t.Errorf("overall delta should be negative, got %v", d.OverallDelta)
	}
	var sawScenario bool
	for _, r := range d.Regressions {
		if r.Scope == "scenario:a2" {
			sawScenario = true
		}
	}
	if !sawScenario {
		t.Errorf("expected scenario:a2 regression, got %+v", d.Regressions)
	}
}

func TestDriftNoRegressionWhenStable(t *testing.T) {
	scenarios := []Scenario{oneTurn("a1", "arithmetic", "1+1?", Assert{Equals: sp("2")})}
	base := reportFrom(t, []string{"2"}, scenarios).AsBaseline()
	same := reportFrom(t, []string{"2"}, scenarios)
	if d := same.Diff(base, 0.05); d.Regressed() {
		t.Errorf("stable run should not regress, got %+v", d.Regressions)
	}
}

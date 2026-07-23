package calibration

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// Baseline is a pinned snapshot of a past calibration run, used to detect *drift*:
// the whole point of calibration is less the absolute score than the change from a
// trusted reference (a provider update or a cheaper variant that silently degrades
// should show up as a drop). It is a truthfulness canary, à la Lesson 11.
type Baseline struct {
	Suite      string             `json:"suite"`
	Provider   string             `json:"provider"`
	Model      string             `json:"model,omitempty"`
	When       time.Time          `json:"when"`
	Overall    float64            `json:"overall"`
	Categories map[string]float64 `json:"categories"`
	Scenarios  map[string]float64 `json:"scenarios"`
}

// AsBaseline extracts a comparable baseline from a report.
func (r *Report) AsBaseline() *Baseline {
	b := &Baseline{
		Suite: r.Suite, Provider: r.Provider, Model: r.Model, When: r.When, Overall: r.Overall,
		Categories: make(map[string]float64, len(r.Categories)),
		Scenarios:  make(map[string]float64, len(r.Scenarios)),
	}
	for _, c := range r.Categories {
		b.Categories[c.Category] = c.PassRate
	}
	for _, s := range r.Scenarios {
		b.Scenarios[s.ID] = s.PassRate
	}
	return b
}

// Save writes the baseline as indented JSON.
func (b *Baseline) Save(path string) error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// LoadBaseline reads a baseline JSON file.
func LoadBaseline(path string) (*Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("calibration: read baseline %q: %w", path, err)
	}
	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("calibration: parse baseline: %w", err)
	}
	return &b, nil
}

// Regression is a single scope (overall / a category / a scenario) whose pass-rate
// dropped from the baseline by more than the threshold.
type Regression struct {
	Scope    string  `json:"scope"`
	Baseline float64 `json:"baseline"`
	Current  float64 `json:"current"`
	Delta    float64 `json:"delta"` // current - baseline (negative = worse)
}

// Drift compares a report against a baseline.
type Drift struct {
	OverallDelta float64      `json:"overall_delta"`
	Regressions  []Regression `json:"regressions"` // scopes that dropped by more than threshold
}

// Regressed reports whether any scope dropped past the threshold.
func (d Drift) Regressed() bool { return len(d.Regressions) > 0 }

// Diff compares the report against base: any scope whose pass-rate dropped by more
// than threshold (e.g. 0.05 = 5 points) is a regression. A scope present in the
// baseline but missing from the report is ignored (suite changed); new scopes are
// not regressions. Results are sorted for stable output.
func (r *Report) Diff(base *Baseline, threshold float64) Drift {
	d := Drift{OverallDelta: r.Overall - base.Overall}
	if r.Overall < base.Overall-threshold {
		d.Regressions = append(d.Regressions, Regression{
			Scope: "overall", Baseline: base.Overall, Current: r.Overall, Delta: r.Overall - base.Overall,
		})
	}
	for _, c := range r.Categories {
		if bp, ok := base.Categories[c.Category]; ok && c.PassRate < bp-threshold {
			d.Regressions = append(d.Regressions, Regression{
				Scope: "category:" + c.Category, Baseline: bp, Current: c.PassRate, Delta: c.PassRate - bp,
			})
		}
	}
	for _, s := range r.Scenarios {
		if bp, ok := base.Scenarios[s.ID]; ok && s.PassRate < bp-threshold {
			d.Regressions = append(d.Regressions, Regression{
				Scope: "scenario:" + s.ID, Baseline: bp, Current: s.PassRate, Delta: s.PassRate - bp,
			})
		}
	}
	sort.Slice(d.Regressions, func(i, j int) bool { return d.Regressions[i].Scope < d.Regressions[j].Scope })
	return d
}

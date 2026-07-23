package calibration

import (
	"fmt"
	"strings"
	"time"
)

// TurnResult aggregates one turn of a scenario across its runs — so you can see
// *which* turn of a multi-turn scenario degrades.
type TurnResult struct {
	Index        int     `json:"index"`         // 1-based turn number
	PassRate     float64 `json:"pass_rate"`     // fraction of runs this turn passed
	SampleReason string  `json:"sample_reason"` // a failure reason (from a failing run), for debugging
}

// ScenarioResult aggregates one scenario across its runs.
type ScenarioResult struct {
	ID       string       `json:"id"`
	Category string       `json:"category"`
	Risk     string       `json:"risk,omitempty"`
	Runs     int          `json:"runs"`
	PassRate float64      `json:"pass_rate"` // fraction of runs where EVERY turn passed
	Turns    []TurnResult `json:"turns"`
	Latency  Stats        `json:"latency_ms"` // over every Chat call in this scenario
	Errors   int          `json:"errors"`     // transport/Chat errors encountered
}

// CategoryResult aggregates the scenarios sharing a category.
type CategoryResult struct {
	Category  string  `json:"category"`
	PassRate  float64 `json:"pass_rate"` // mean of the category's scenario pass-rates
	Scenarios int     `json:"scenarios"`
}

// Report is a full calibration run's outcome — the machine-readable and
// human-readable summary produced by Run.
type Report struct {
	Suite      string           `json:"suite"`
	Provider   string           `json:"provider"`
	Model      string           `json:"model,omitempty"`
	When       time.Time        `json:"when"`
	Overall    float64          `json:"overall"` // mean of scenario pass-rates
	Latency    Stats            `json:"latency_ms"`
	Categories []CategoryResult `json:"categories"`
	Scenarios  []ScenarioResult `json:"scenarios"`
}

// String renders a compact human-readable summary.
func (r *Report) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "calibration: suite %q · provider %s", r.Suite, r.Provider)
	if r.Model != "" {
		fmt.Fprintf(&b, " (%s)", r.Model)
	}
	fmt.Fprintf(&b, " · %s\n", r.When.Format(time.RFC3339))
	fmt.Fprintf(&b, "  overall pass-rate: %s · latency %.0fms ±%.0f (n=%d)\n",
		pct(r.Overall), r.Latency.Mean, r.Latency.Stddev, r.Latency.N)

	if len(r.Categories) > 0 {
		b.WriteString("  by category:\n")
		for _, c := range r.Categories {
			fmt.Fprintf(&b, "    %-14s %4s  (%d)\n", c.Category, pct(c.PassRate), c.Scenarios)
		}
	}
	b.WriteString("  scenarios:\n")
	for _, s := range r.Scenarios {
		fmt.Fprintf(&b, "    [%4s] %s (%s", pct(s.PassRate), s.ID, s.Category)
		if s.Risk != "" {
			fmt.Fprintf(&b, ", %s", s.Risk)
		}
		b.WriteString(")")
		if s.Errors > 0 {
			fmt.Fprintf(&b, "  ⚠ %d error(s)", s.Errors)
		}
		// Surface the weakest turn's reason when the scenario isn't fully passing.
		if s.PassRate < 1 {
			for _, t := range s.Turns {
				if t.PassRate < 1 && t.SampleReason != "" {
					fmt.Fprintf(&b, "  turn%d %s (%s)", t.Index, pct(t.PassRate), t.SampleReason)
					break
				}
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// pct formats a 0..1 rate as a percentage.
func pct(f float64) string { return fmt.Sprintf("%.0f%%", f*100) }

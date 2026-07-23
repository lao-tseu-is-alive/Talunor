package calibration

import (
	"context"
	"sort"
	"time"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
)

// Options tune a calibration run.
type Options struct {
	// DefaultRuns is how many times a scenario is replayed when it does not set
	// its own Runs. More runs sharpen the pass-rate (and expose flakiness). 0 → 1.
	DefaultRuns int
	// Temperature is the sampling temperature for scenarios that do not override
	// it. 0 leaves the provider default. Higher values surface consistency, not
	// just accuracy.
	Temperature float64
	// Model, when set, overrides the provider's default model and labels the report
	// (so a baseline is pinned to an exact model, not just a provider).
	Model string
}

// Run replays every scenario in the suite against the provider — clean-room (the
// conversation is built from the scenario's turns and optional system prompt
// alone, never from any session memory) — and returns a Report. A transport error
// on a turn fails that run (the conversation cannot continue) but never aborts the
// whole calibration: the point is to measure, not to stop at the first stumble.
func Run(ctx context.Context, p llm.Provider, suite *Suite, opts Options) *Report {
	rep := &Report{Suite: suite.Name, Provider: p.Name(), Model: opts.Model, When: time.Now().UTC()}

	var allLatency []float64
	type acc struct {
		sum float64
		n   int
	}
	byCat := map[string]*acc{}
	var overallSum float64

	for i := range suite.Scenarios {
		sr, lat := runScenario(ctx, p, &suite.Scenarios[i], opts)
		rep.Scenarios = append(rep.Scenarios, sr)
		allLatency = append(allLatency, lat...)
		overallSum += sr.PassRate
		a := byCat[sr.Category]
		if a == nil {
			a = &acc{}
			byCat[sr.Category] = a
		}
		a.sum += sr.PassRate
		a.n++
	}

	if n := len(suite.Scenarios); n > 0 {
		rep.Overall = overallSum / float64(n)
	}
	rep.Latency = newStats(allLatency)
	for cat, a := range byCat {
		rep.Categories = append(rep.Categories, CategoryResult{
			Category: cat, PassRate: a.sum / float64(a.n), Scenarios: a.n,
		})
	}
	sort.Slice(rep.Categories, func(i, j int) bool { return rep.Categories[i].Category < rep.Categories[j].Category })
	return rep
}

// runScenario replays one scenario `runs` times and returns its aggregated result
// plus every Chat latency (ms) it measured.
func runScenario(ctx context.Context, p llm.Provider, sc *Scenario, opts Options) (ScenarioResult, []float64) {
	runs := sc.Runs
	if runs <= 0 {
		runs = opts.DefaultRuns
	}
	if runs <= 0 {
		runs = 1
	}
	temp := opts.Temperature
	if sc.Temperature > 0 {
		temp = sc.Temperature
	}
	llmOpts := llm.Options{Temperature: temp, Model: opts.Model}

	nTurns := len(sc.Turns)
	turnPass := make([]int, nTurns)
	turnReason := make([]string, nTurns)
	scenarioPass, errors := 0, 0
	var latency []float64

	for r := 0; r < runs; r++ {
		var msgs []llm.Message
		if sc.System != "" {
			msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: sc.System})
		}
		allPassed := true
		for ti := range sc.Turns {
			t := &sc.Turns[ti]
			msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: t.User})

			start := time.Now()
			reply, err := llm.Collect(ctx, p, msgs, llmOpts)
			latency = append(latency, float64(time.Since(start).Microseconds())/1000.0)
			if err != nil {
				errors++
				allPassed = false
				break // no reply → the conversation can't continue this run.
			}
			if ok, reason := t.Expect.Check(reply); ok {
				turnPass[ti]++
			} else {
				allPassed = false
				turnReason[ti] = reason
			}
			msgs = append(msgs, llm.Message{Role: llm.RoleAssistant, Content: reply})
		}
		if allPassed {
			scenarioPass++
		}
	}

	sr := ScenarioResult{
		ID:       sc.ID,
		Category: categoryOf(sc),
		Risk:     sc.Risk,
		Runs:     runs,
		PassRate: float64(scenarioPass) / float64(runs),
		Latency:  newStats(latency),
		Errors:   errors,
	}
	for ti := range sc.Turns {
		sr.Turns = append(sr.Turns, TurnResult{
			Index:        ti + 1,
			PassRate:     float64(turnPass[ti]) / float64(runs),
			SampleReason: turnReason[ti],
		})
	}
	return sr, latency
}

// categoryOf returns the scenario's category, defaulting to "uncategorized" so
// aggregation is stable.
func categoryOf(sc *Scenario) string {
	if sc.Category == "" {
		return "uncategorized"
	}
	return sc.Category
}

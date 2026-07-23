package calibration

import (
	"context"
	"fmt"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
)

// fakeProvider returns canned replies in call order, so calibration runs are
// deterministic without a live model. errAt (>=0) makes one Chat call fail.
type fakeProvider struct {
	replies  []string
	call     int
	errAt    int
	lastTemp float64
}

func newFake(replies ...string) *fakeProvider { return &fakeProvider{replies: replies, errAt: -1} }

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Chat(_ context.Context, _ []llm.Message, opts llm.Options) (<-chan llm.Chunk, error) {
	i := f.call
	f.call++
	f.lastTemp = opts.Temperature
	if i == f.errAt {
		return nil, fmt.Errorf("boom")
	}
	reply := ""
	if i < len(f.replies) {
		reply = f.replies[i]
	}
	ch := make(chan llm.Chunk, 1)
	ch <- llm.Chunk{Content: reply}
	close(ch)
	return ch, nil
}

func oneTurn(id, category, user string, expect Assert) Scenario {
	return Scenario{ID: id, Category: category, Turns: []Turn{{User: user, Expect: expect}}}
}

func TestRunAllPass(t *testing.T) {
	suite := &Suite{Name: "s", Scenarios: []Scenario{
		oneTurn("arith", "arithmetic", "137*4?", Assert{Equals: sp("548")}),
	}}
	rep := Run(context.Background(), newFake("548"), suite, Options{})
	if rep.Overall != 1.0 {
		t.Errorf("overall = %v, want 1.0", rep.Overall)
	}
	if rep.Scenarios[0].PassRate != 1.0 || rep.Latency.N != 1 {
		t.Errorf("scenario/latency mismatch: %+v (lat n=%d)", rep.Scenarios[0], rep.Latency.N)
	}
}

func TestRunFlakyPassRate(t *testing.T) {
	// Two runs, replies differ: run1 passes (548), run2 fails (549) → 0.5.
	suite := &Suite{Name: "s", Scenarios: []Scenario{
		{ID: "flaky", Category: "arithmetic", Runs: 2, Turns: []Turn{
			{User: "137*4?", Expect: Assert{Equals: sp("548")}},
		}},
	}}
	rep := Run(context.Background(), newFake("548", "549"), suite, Options{})
	if got := rep.Scenarios[0].PassRate; got != 0.5 {
		t.Errorf("flaky pass-rate = %v, want 0.5", got)
	}
	if got := rep.Scenarios[0].Turns[0].PassRate; got != 0.5 {
		t.Errorf("turn pass-rate = %v, want 0.5", got)
	}
}

func TestRunMultiTurn(t *testing.T) {
	suite := &Suite{Name: "s", Scenarios: []Scenario{
		{ID: "chain", Category: "arithmetic", Turns: []Turn{
			{User: "137*4?", Expect: Assert{Equals: sp("548")}},
			{User: "/2?", Expect: Assert{Number: &NumberMatch{Equals: 274}}},
		}},
	}}
	rep := Run(context.Background(), newFake("548", "the answer is 274"), suite, Options{})
	if rep.Scenarios[0].PassRate != 1.0 {
		t.Errorf("multi-turn should fully pass, got %v", rep.Scenarios[0].PassRate)
	}
	if len(rep.Scenarios[0].Turns) != 2 {
		t.Fatalf("want 2 turn results, got %d", len(rep.Scenarios[0].Turns))
	}
}

func TestRunTransportError(t *testing.T) {
	f := newFake("548")
	f.errAt = 0 // fail the only Chat call
	suite := &Suite{Name: "s", Scenarios: []Scenario{
		oneTurn("x", "arithmetic", "137*4?", Assert{Equals: sp("548")}),
	}}
	rep := Run(context.Background(), f, suite, Options{})
	if rep.Scenarios[0].PassRate != 0 {
		t.Errorf("errored scenario pass-rate = %v, want 0", rep.Scenarios[0].PassRate)
	}
	if rep.Scenarios[0].Errors == 0 {
		t.Error("expected the transport error to be counted")
	}
}

func TestRunTemperatureOverride(t *testing.T) {
	f := newFake("ok")
	suite := &Suite{Name: "s", Scenarios: []Scenario{
		{ID: "x", Category: "c", Temperature: 0.9, Turns: []Turn{{User: "hi", Expect: Assert{Contains: sp("ok")}}}},
	}}
	Run(context.Background(), f, suite, Options{Temperature: 0.1})
	if f.lastTemp != 0.9 {
		t.Errorf("scenario temperature override not applied: got %v, want 0.9", f.lastTemp)
	}
}

func TestRunCategoryAggregation(t *testing.T) {
	suite := &Suite{Name: "s", Scenarios: []Scenario{
		oneTurn("a1", "arithmetic", "1+1?", Assert{Equals: sp("2")}),
		oneTurn("a2", "arithmetic", "2+2?", Assert{Equals: sp("4")}),
		oneTurn("f1", "format", "json?", Assert{JSONValid: bp(true)}),
	}}
	// a1 pass, a2 fail, f1 fail → arithmetic 50%, format 0%.
	rep := Run(context.Background(), newFake("2", "5", "not json"), suite, Options{})
	byCat := map[string]float64{}
	for _, c := range rep.Categories {
		byCat[c.Category] = c.PassRate
	}
	if byCat["arithmetic"] != 0.5 {
		t.Errorf("arithmetic category = %v, want 0.5", byCat["arithmetic"])
	}
	if byCat["format"] != 0.0 {
		t.Errorf("format category = %v, want 0.0", byCat["format"])
	}
}

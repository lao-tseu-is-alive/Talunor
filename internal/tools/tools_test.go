package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/tools"
)

func TestCalculator(t *testing.T) {
	c := tools.Calculator{}
	cases := []struct {
		expr, want string
	}{
		{"2 + 2", "4"},
		{"12 * (3 + 4)", "84"},
		{"10 / 4", "2.5"},
		{"-3 + 5", "2"},
		{"2 * 3 + 4", "10"}, // precedence via the parser
	}
	for _, tc := range cases {
		args, _ := json.Marshal(map[string]string{"expression": tc.expr})
		got, err := c.Execute(context.Background(), args)
		if err != nil {
			t.Errorf("%q: unexpected error %v", tc.expr, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q = %q; want %q", tc.expr, got, tc.want)
		}
	}
}

func TestCalculatorRejectsNonArithmetic(t *testing.T) {
	c := tools.Calculator{}
	for _, expr := range []string{"os.Exit(1)", "1 + foo", "\"hi\"", "1 << 2"} {
		args, _ := json.Marshal(map[string]string{"expression": expr})
		if _, err := c.Execute(context.Background(), args); err == nil {
			t.Errorf("expected %q to be rejected", expr)
		}
	}
	// Division by zero is an error, not a panic.
	args, _ := json.Marshal(map[string]string{"expression": "1/0"})
	if _, err := c.Execute(context.Background(), args); err == nil {
		t.Error("expected division by zero error")
	}
}

func TestClockDefaultsUTC(t *testing.T) {
	got, err := tools.Clock{}.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("clock: %v", err)
	}
	if !strings.HasSuffix(got, "UTC") {
		t.Errorf("time %q should end in UTC by default", got)
	}
}

func TestClockBadTimezone(t *testing.T) {
	args, _ := json.Marshal(map[string]string{"timezone": "Mars/Olympus"})
	if _, err := (tools.Clock{}).Execute(context.Background(), args); err == nil {
		t.Error("expected error for unknown timezone")
	}
}

func TestRegistry(t *testing.T) {
	r := tools.NewRegistry(tools.Calculator{}, tools.Clock{})
	if r.Len() != 2 {
		t.Fatalf("len = %d; want 2", r.Len())
	}
	// Defs are sorted by name.
	defs := r.Defs()
	if len(defs) != 2 || defs[0].Name != "calculator" || defs[1].Name != "current_time" {
		t.Errorf("defs not sorted/complete: %+v", defs)
	}
	// Execute routes to the tool and returns its result.
	args, _ := json.Marshal(map[string]string{"expression": "6 * 7"})
	if got := r.Execute(context.Background(), "calculator", args); got != "42" {
		t.Errorf("registry execute = %q; want 42", got)
	}
	// Unknown tool becomes an observation, not a crash.
	if got := r.Execute(context.Background(), "nope", nil); !strings.Contains(got, "no such tool") {
		t.Errorf("unknown tool = %q; want a 'no such tool' observation", got)
	}
	// A tool error becomes an "error:" observation.
	bad, _ := json.Marshal(map[string]string{"expression": "1/0"})
	if got := r.Execute(context.Background(), "calculator", bad); !strings.HasPrefix(got, "error:") {
		t.Errorf("tool error = %q; want an 'error:' observation", got)
	}
}

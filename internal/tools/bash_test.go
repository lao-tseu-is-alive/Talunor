package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/lao-tseu-is-alive/Talunor/internal/sandbox"
)

// stubSandbox is a Sandbox that echoes back what it was asked to run, so the
// Bash tool can be tested without a real backend.
type stubSandbox struct {
	lastScript string
	lastLim    sandbox.Limits
	out        string
	err        error
}

func (s *stubSandbox) Name() string { return "stub" }
func (s *stubSandbox) Run(_ context.Context, script string, lim sandbox.Limits) (string, error) {
	s.lastScript = script
	s.lastLim = lim
	return s.out, s.err
}

func TestBashRequiresApproval(t *testing.T) {
	b := NewBash(&stubSandbox{}, sandbox.DefaultLimits())
	if !b.RequiresApproval() {
		t.Fatal("bash tool must require approval")
	}
	// It must satisfy the Approvable interface the agent checks for.
	var _ Approvable = b
}

func TestBashExecutePassesCommandAndReturnsOutput(t *testing.T) {
	stub := &stubSandbox{out: "hello\n"}
	lim := sandbox.DefaultLimits()
	lim.Timeout = 3 * time.Second
	b := NewBash(stub, lim)

	out, err := b.Execute(context.Background(), json.RawMessage(`{"command":"echo hello"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "hello\n" {
		t.Errorf("output = %q; want %q", out, "hello\n")
	}
	if stub.lastScript != "echo hello" {
		t.Errorf("script passed = %q; want %q", stub.lastScript, "echo hello")
	}
	if stub.lastLim.Timeout != 3*time.Second {
		t.Errorf("limits not forwarded: got timeout %s", stub.lastLim.Timeout)
	}
}

func TestBashRejectsEmptyCommand(t *testing.T) {
	b := NewBash(&stubSandbox{}, sandbox.DefaultLimits())
	if _, err := b.Execute(context.Background(), json.RawMessage(`{"command":"  "}`)); err == nil {
		t.Error("expected an error for an empty command")
	}
}

func TestBashEmptyOutputIsReported(t *testing.T) {
	b := NewBash(&stubSandbox{out: "   "}, sandbox.DefaultLimits())
	out, err := b.Execute(context.Background(), json.RawMessage(`{"command":"true"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "(no output)" {
		t.Errorf("output = %q; want %q", out, "(no output)")
	}
}

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lao-tseu-is-alive/Talunor/internal/sandbox"
)

// Bash runs a shell command inside a sandbox (see internal/sandbox) and returns
// its combined stdout+stderr as the observation. It is the agent's most powerful
// — and most dangerous — tool, so it is gated two ways: it is only registered
// when TALUNOR_BASH=1 (see cmd/talunor), and it implements [Approvable] so every
// call pauses the ReAct loop for explicit human y/n approval (the v0.8.0 gate).
// The sandbox has no network by default.
type Bash struct {
	sb  sandbox.Sandbox
	lim sandbox.Limits
}

// NewBash builds the tool over a sandbox backend and the limits each run gets.
func NewBash(sb sandbox.Sandbox, lim sandbox.Limits) *Bash {
	return &Bash{sb: sb, lim: lim}
}

func (*Bash) Name() string { return "bash" }

func (b *Bash) Description() string {
	return "Run a shell command in an isolated, throwaway sandbox (" + b.sb.Name() +
		" backend) and get its combined stdout+stderr. There is NO network and NO " +
		"access to the host filesystem; only /tmp is writable and everything is " +
		"discarded when the command finishes. Use it for quick computation, text " +
		"processing, or inspecting data you already have — not for reaching the " +
		"internet. Each call requires the user's explicit approval."
}

func (*Bash) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"command": {
				"type": "string",
				"description": "The shell command to run, interpreted by /bin/sh (e.g. \"echo $((2**10))\" or \"seq 1 5 | paste -sd+ | bc\")."
			}
		},
		"required": ["command"]
	}`)
}

// RequiresApproval marks the tool as human-gated: the agent must get a y/n before
// every run. See [Approvable].
func (*Bash) RequiresApproval() bool { return true }

func (b *Bash) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.Command) == "" {
		return "", fmt.Errorf("command is required")
	}
	out, err := b.sb.Run(ctx, in.Command, b.lim)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(out) == "" {
		return "(no output)", nil
	}
	return out, nil
}

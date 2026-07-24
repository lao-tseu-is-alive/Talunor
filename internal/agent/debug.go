package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
)

// This file implements the interactive, on-screen half of the agent's
// observability: the /debug toggle. Config.Debug logs the same decisions to a
// file/stderr (good for `tail -f`); screen debug streams them into the transcript
// as dimmed notes so a user can *watch* recall and reflection happen live — which
// is exactly what turns "it didn't remember me" into a one-command diagnosis.
//
// The notes ride the existing Reasoning channel of llm.Chunk, which both
// front-ends already render dimmed (the same channel tool activity uses), so no
// renderer changes are needed. Answer text is accumulated from Content only, so
// these notes never leak into the stored reply or the reflection input.

// SetScreenDebug turns inline debug notes on or off and reports the new state.
// Call it between turns (single-user; not safe to flip mid-turn).
func (a *Agent) SetScreenDebug(on bool) bool { a.screenDebug = on; return a.screenDebug }

// ToggleScreenDebug flips inline debug notes and reports the new state.
func (a *Agent) ToggleScreenDebug() bool { return a.SetScreenDebug(!a.screenDebug) }

// ScreenDebug reports whether inline debug notes are on.
func (a *Agent) ScreenDebug() bool { return a.screenDebug }

// DebugCommand applies a /debug slash command (fields already split on
// whitespace: "/debug", "/debug on", "/debug off") and returns a display-ready
// status line. With no argument it toggles. Shared by the REPL and the TUI.
func (a *Agent) DebugCommand(fields []string) string {
	if len(fields) > 1 {
		switch strings.ToLower(fields[1]) {
		case "on", "1", "true", "yes":
			a.SetScreenDebug(true)
		case "off", "0", "false", "no":
			a.SetScreenDebug(false)
		default:
			return "usage: /debug [on|off]  (no argument toggles)"
		}
	} else {
		a.ToggleScreenDebug()
	}
	if a.screenDebug {
		return "debug: ON — recall rankings & reflection now show inline (dimmed). /debug off to stop."
	}
	return "debug: off"
}

// debugPrefix marks a debug note so it is recognisable amid model reasoning.
const debugPrefix = "· "

// sendDebug streams one dimmed debug note when screen debug is on. It returns
// false only if the context was cancelled while sending (so callers can stop);
// with screen debug off it is a no-op and returns true.
func (a *Agent) sendDebug(ctx context.Context, out chan<- llm.Chunk, format string, args ...any) bool {
	if !a.screenDebug {
		return true
	}
	return a.send(ctx, out, llm.Chunk{Reasoning: debugPrefix + fmt.Sprintf(format, args...) + "\n"})
}

// emitRecallDebug prints the recall ranking (query, budget, and each hit with its
// distance and kind) that shaped this turn's prompt. This is the view that was
// missing when memory silently failed to recall a fact.
func (a *Agent) emitRecallDebug(ctx context.Context, out chan<- llm.Chunk, input string, hits []memory.Hit) {
	if !a.screenDebug {
		return
	}
	a.sendDebug(ctx, out, "recall: q=%q k=%d max≤%.2f → %d hit(s)",
		oneLine(input, 50), a.cfg.RecallK, a.cfg.RecallMaxDistance, len(hits))
	for _, h := range hits {
		a.sendDebug(ctx, out, "    #%d d=%.4f score=%.3f sal=%.2f %s %q",
			h.ID, h.Distance, h.Score, h.Salience, h.Kind, oneLine(h.Content, 50))
	}
}

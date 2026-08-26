// This file is the slash-command surface shared by the TUI and the --plain REPL,
// plus the formatting helpers they render with. None of it is part of the cognitive
// loop; it is the read-only window onto what the loop has stored.
//
// Split out of agent.go in v0.22.5 — same package, same code, no behaviour change.

package agent

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
)

// ShortTermLen reports how many turns are currently in immediate context.
func (a *Agent) ShortTermLen() int { return a.short.Len() }

// MemoryCount reports how many long-term memories are stored.
func (a *Agent) MemoryCount(ctx context.Context) (int, error) { return a.store.Count(ctx) }

// HelpText lists the slash commands understood by both the TUI and the REPL.
const HelpText = `Commands:
  /help        show this help
  /mem         memory stats (count + database file + embedding provenance)
  /list [n]    list the most recent n memories (default 10)
  /forget <id> delete the memory with that #id (as shown by /list)
  /why <id>    show a fact's evidence trail (which turns/sources support it)
  /plan        show the most recent plan (when TALUNOR_PLANNER=1)
  /debug [on|off]  toggle inline trace of recall rankings & reflection
  /clear       clear the on-screen transcript (TUI only; does not erase memory)
  /exit, /quit quit
Keys (TUI): enter = send · ctrl+c / esc = quit · ↑/↓ or PgUp/PgDn = scroll
(Mouse selection works: click-drag to select and copy text.)`

// Help returns the command help text.
func (a *Agent) Help() string { return HelpText }

// MemoryStats returns a one-line summary of stored memory and where it lives,
// plus the embedding-provenance status when it is not OK (a heads-up that recall
// may be degraded until a re-embed).
func (a *Agent) MemoryStats(ctx context.Context) (string, error) {
	n, err := a.store.Count(ctx)
	if err != nil {
		return "", err
	}
	msg := fmt.Sprintf("%d memories stored in %s\nembedding model: %s (dim %d), provenance: %s",
		n, a.store.Path(), a.store.EmbedModelName(), a.store.Dim(), a.store.Provenance())
	if a.store.Provenance() != memory.ProvenanceOK {
		msg += "\n⚠ recall of older memories may be degraded — run `talunor --reembed` to realign"
	}
	// LAYER 22: say which arms recall is actually running on. A vector-only build
	// still answers everything — it is just worse at exact identifiers — so this
	// is a status line, not a warning.
	msg += fmt.Sprintf("\nrecall: %s", recallMode(a.store.Lexical()))
	return msg, nil
}

// recallMode describes the retrieval strategy in one line (LAYER 22).
func recallMode(st memory.LexicalStatus) string {
	if st == memory.LexicalOK {
		return "hybrid (vector + lexical/BM25)"
	}
	return "vector only — " + st.String()
}

// ListMemories returns a formatted listing of the most recent n memories.
func (a *Agent) ListMemories(ctx context.Context, n int) (string, error) {
	mems, err := a.store.List(ctx, n)
	if err != nil {
		return "", err
	}
	return FormatMemories(mems), nil
}

// MemoryID parses the id argument of a slash command whose fields have been
// split on whitespace (e.g. "/forget 7" → 7). It reports ok=false when the id
// is missing or not a valid integer, so callers can show usage help.
func MemoryID(fields []string) (id int64, ok bool) {
	if len(fields) < 2 {
		return 0, false
	}
	id, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// ForgetMemory deletes the long-term memory with the given id (the #id shown by
// ListMemories) and returns a one-line, display-ready result. Forgetting a
// long-term memory does not alter the current session's short-term context.
func (a *Agent) ForgetMemory(ctx context.Context, id int64) (string, error) {
	ok, err := a.store.Forget(ctx, id)
	if err != nil {
		return "", err
	}
	if !ok {
		return fmt.Sprintf("no memory #%d to forget", id), nil
	}
	return fmt.Sprintf("forgot memory #%d", id), nil
}

// WhyMemory returns a display-ready view of a fact and its evidence trail: which
// turns, from which sources, supported it (Layer 20). A memory with no recorded
// evidence (e.g. one learned before this layer) shows an empty trail.
func (a *Agent) WhyMemory(ctx context.Context, id int64) (string, error) {
	m, ok, err := a.store.MemoryByID(ctx, id)
	if err != nil {
		return "", err
	}
	if !ok {
		return fmt.Sprintf("no memory #%d", id), nil
	}
	ev, err := a.store.EvidenceFor(ctx, id)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "#%d [%s] %s\n", m.ID, m.Kind, oneLine(m.Content, 80))
	if m.Kind == memory.KindFact {
		// Attribution is provenance AND subject (Layer 23): "who said it" alone does
		// not say whether it may correct anything — see memory.Supersedes.
		fmt.Fprintf(&b, "  %s (about: %s), confidence %.0f%%, salience %.1f (×%d)\n",
			m.Provenance, m.Subject, m.Confidence*100, m.Salience, m.AccessCount)
	}
	if m.SupersededBy > 0 {
		fmt.Fprintf(&b, "  ⚠ superseded by #%d (retired from recall; kept for audit)\n", m.SupersededBy)
	}
	if len(ev) == 0 {
		b.WriteString("  evidence: (none recorded)")
		return b.String(), nil
	}
	fmt.Fprintf(&b, "  evidence (%d):\n", len(ev))
	for _, e := range ev {
		turn := "—"
		if e.TurnID > 0 {
			turn = fmt.Sprintf("turn #%d", e.TurnID)
		}
		fmt.Fprintf(&b, "    - %-8s %-14s %s\n", turn, e.Source, e.CreatedAt.Format("2006-01-02 15:04"))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// FormatMemories renders memories (newest first) as a compact, readable list.
func FormatMemories(mems []memory.Memory) string {
	if len(mems) == 0 {
		return "(no memories yet)"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Most recent %d memories (newest first):\n", len(mems))
	for _, m := range mems {
		label := m.Role
		if label == "" {
			label = string(m.Kind)
		}
		// Facts carry an attribution — provenance + subject (Layers 16 & 23) — a
		// confidence, and a salience that grows with reinforcement (Layer 17); show
		// them so the user can see how much the agent trusts a learned statement,
		// what it takes that statement to be ABOUT, and how much it currently matters.
		meta := ""
		if m.Kind == memory.KindFact {
			meta = fmt.Sprintf(" (%s %.0f%%, sal %.1f×%d)",
				memory.Attr(m.Provenance, m.Subject), m.Confidence*100, m.Salience, m.AccessCount)
		}
		// A superseded fact is retired from recall but still listed (marked), so its
		// history stays inspectable — /why <id> shows what replaced it (Layer 21).
		if m.SupersededBy > 0 {
			meta += fmt.Sprintf(" ⚠→#%d", m.SupersededBy)
		}
		fmt.Fprintf(&b, "  #%d [%s]%s %s  %s\n",
			m.ID, label, meta, m.CreatedAt.Format("2006-01-02 15:04"), oneLine(m.Content, 66))
	}
	return b.String()
}

// oneLine collapses whitespace and truncates s to at most max runes.
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max-1]) + "…"
	}
	return s
}

package tui_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lao-tseu-is-alive/Talunor/internal/agent"
	"github.com/lao-tseu-is-alive/Talunor/internal/history"
	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
	"github.com/lao-tseu-is-alive/Talunor/internal/tools"
	"github.com/lao-tseu-is-alive/Talunor/internal/tui"
)

type fakeProvider struct{ reply string }

func (f fakeProvider) Name() string { return "fake" }

func (f fakeProvider) Chat(_ context.Context, _ []llm.Message, _ llm.Options) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk, 1)
	ch <- llm.Chunk{Content: f.reply}
	close(ch)
	return ch, nil
}

// scriptedProvider returns one canned response per Chat call (for tool flows).
type scriptedProvider struct {
	steps [][]llm.Chunk
	call  int
}

func (p *scriptedProvider) Name() string { return "scripted" }

func (p *scriptedProvider) Chat(_ context.Context, _ []llm.Message, _ llm.Options) (<-chan llm.Chunk, error) {
	step := p.steps[p.call]
	p.call++
	ch := make(chan llm.Chunk, len(step))
	for _, c := range step {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// dangerTool is a gated fake tool that records whether it ran.
type dangerTool struct{ ran *bool }

func (dangerTool) Name() string                    { return "danger" }
func (dangerTool) Description() string              { return "side effects" }
func (dangerTool) Schema() json.RawMessage          { return json.RawMessage(`{"type":"object"}`) }
func (dangerTool) RequiresApproval() bool           { return true }
func (d dangerTool) Execute(context.Context, json.RawMessage) (string, error) {
	*d.ran = true
	return "ok", nil
}

// newAgent builds an agent for UI tests with reflection disabled: these tests
// drive the Bubble Tea loop and assert exact stored-turn counts, so the extra
// LLM extraction call (which would run over the fake provider) is turned off.
func newAgent(store *memory.Store, reply string) *agent.Agent {
	cfg := agent.DefaultConfig()
	cfg.Extractor = agent.DisableReflection()
	return agent.New(store, fakeProvider{reply: reply}, cfg)
}

func testStore(t *testing.T) *memory.Store {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	cfg := memory.Config{
		DBPath:         ":memory:",
		VectorExtPath:  filepath.Join(root, "ext", "vector"),
		AIExtPath:      filepath.Join(root, "ext", "ai"),
		EmbedModelPath: filepath.Join(root, "ext", "models", "all-MiniLM-L6-v2.f16.gguf"),
	}
	if _, err := os.Stat(cfg.VectorExtPath + ".so"); err != nil {
		t.Skip("extensions/model missing — run `make deps` first")
	}
	store, err := memory.Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// TestModelDriveTurn drives a full turn through the Bubble Tea Update loop with
// no real terminal: size the window, type a prompt, press enter, then pump the
// stream commands to completion. The rendered view must contain the reply.
func TestModelDriveTurn(t *testing.T) {
	store := testStore(t)
	ag := newAgent(store, "**teal** is your colour")

	var m tea.Model = tui.New(context.Background(), ag, nil, "fake", "test-model", 0)

	// Terminal size arrives first; without it the model is not ready.
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if strings.Contains(m.View(), "initializing") {
		t.Fatal("model still initializing after WindowSizeMsg")
	}

	// Type "hi" and submit.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Pump the reply stream to completion: each command yields the next msg.
	for cmd != nil {
		m, cmd = m.Update(cmd())
	}

	view := m.View()
	if !strings.Contains(view, "hi") {
		t.Errorf("view missing the user message:\n%s", view)
	}
	if !strings.Contains(view, "teal") {
		t.Errorf("view missing the assistant reply:\n%s", view)
	}
	// The turn must have been persisted (user + assistant).
	if n, err := store.Count(context.Background()); err != nil || n != 2 {
		t.Errorf("stored count = %d, err = %v; want 2", n, err)
	}
}

// TestHistoryRecallKeys drives ↑/↓ through the Update loop and checks the input
// line shows the recalled prompt: ↑ walks back to older entries, ↓ walks forward
// and restores the empty draft past the newest one.
func TestHistoryRecallKeys(t *testing.T) {
	store := testStore(t)
	ag := newAgent(store, "unused")

	hist := history.New(filepath.Join(t.TempDir(), "history.jsonl"))
	hist.Add("alpha")
	hist.Add("beta") // newest.

	var m tea.Model = tui.New(context.Background(), ag, hist, "fake", "test-model", 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// The transcript is empty here, so a recalled prompt can only appear in the
	// input line (rendered as "you> <value>").
	press := func(k tea.KeyType) string {
		m, _ = m.Update(tea.KeyMsg{Type: k})
		return m.View()
	}

	if v := press(tea.KeyUp); !strings.Contains(v, "you> beta") {
		t.Errorf("first ↑ should recall newest 'beta'; input line missing it:\n%s", v)
	}
	if v := press(tea.KeyUp); !strings.Contains(v, "you> alpha") {
		t.Errorf("second ↑ should recall older 'alpha':\n%s", v)
	}
	if v := press(tea.KeyDown); !strings.Contains(v, "you> beta") {
		t.Errorf("↓ should walk forward to 'beta':\n%s", v)
	}
	if v := press(tea.KeyDown); strings.Contains(v, "you> beta") || strings.Contains(v, "you> alpha") {
		t.Errorf("↓ past newest should restore the empty draft, not a history entry:\n%s", v)
	}
}

// TestSlashCommandDoesNotHitProvider ensures /help runs locally: it shows output
// and never starts a turn (so nothing is stored and no stream command runs).
func TestSlashCommandDoesNotHitProvider(t *testing.T) {
	store := testStore(t)
	ag := newAgent(store, "should not be called")
	var m tea.Model = tui.New(context.Background(), ag, nil, "fake", "test-model", 0)

	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/help")})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if cmd != nil {
		t.Error("/help should not return a stream command")
	}
	if !strings.Contains(m.View(), "Commands:") {
		t.Errorf("view missing help text:\n%s", m.View())
	}
	if n, _ := store.Count(context.Background()); n != 0 {
		t.Errorf("stored count = %d; want 0 (command must not persist a turn)", n)
	}
}

// TestTUIApprovalFlow drives a tool that needs approval through the Bubble Tea
// loop: the prompt must appear and pause the stream, pressing 'y' must run the
// tool and resume to the final answer.
func TestTUIApprovalFlow(t *testing.T) {
	store := testStore(t)
	ran := false
	prov := &scriptedProvider{steps: [][]llm.Chunk{
		{{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "danger", Args: `{}`}}}},
		{{Content: "all finished"}},
	}}
	cfg := agent.DefaultConfig()
	cfg.Tools = tools.NewRegistry(dangerTool{ran: &ran})
	cfg.Extractor = agent.DisableReflection()
	ag := agent.New(store, prov, cfg)

	var m tea.Model = tui.New(context.Background(), ag, nil, "fake", "test-model", 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("do it")})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Pump until the loop pauses for approval (the approval msg returns no cmd).
	for cmd != nil {
		m, cmd = m.Update(cmd())
	}
	if !strings.Contains(m.View(), "Allow tool") {
		t.Fatalf("expected an approval prompt; view:\n%s", m.View())
	}
	if ran {
		t.Fatal("tool ran before approval")
	}

	// Approve with 'y' and pump to completion.
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	for cmd != nil {
		m, cmd = m.Update(cmd())
	}
	if !ran {
		t.Error("tool should have run after approval")
	}
	// Glamour styles per cell, so strip ANSI before matching the answer text.
	if got := stripANSI(m.View()); !strings.Contains(got, "all finished") {
		t.Errorf("expected the final answer after approval; view:\n%s", got)
	}
}

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// TestEnterIgnoredWhileStreaming ensures a second submit mid-stream is a no-op.
func TestEnterIgnoredWhileStreaming(t *testing.T) {
	store := testStore(t)
	ag := newAgent(store, "ok")
	var m tea.Model = tui.New(context.Background(), ag, nil, "fake", "test-model", 0)

	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("first")})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// While streaming (cmd still pending), an extra Enter must not start a turn.
	m, extra := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if extra != nil {
		t.Error("Enter during streaming should be ignored (no command)")
	}
	for cmd != nil {
		m, cmd = m.Update(cmd())
	}
	if n, _ := store.Count(context.Background()); n != 2 {
		t.Errorf("stored count = %d; want 2 (one turn only)", n)
	}
}

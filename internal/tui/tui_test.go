package tui_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lao-tseu-is-alive/Talunor/internal/agent"
	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
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
	ag := agent.New(store, fakeProvider{reply: "**teal** is your colour"}, agent.DefaultConfig())

	var m tea.Model = tui.New(context.Background(), ag, "fake", "test-model", 0)

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

// TestSlashCommandDoesNotHitProvider ensures /help runs locally: it shows output
// and never starts a turn (so nothing is stored and no stream command runs).
func TestSlashCommandDoesNotHitProvider(t *testing.T) {
	store := testStore(t)
	ag := agent.New(store, fakeProvider{reply: "should not be called"}, agent.DefaultConfig())
	var m tea.Model = tui.New(context.Background(), ag, "fake", "test-model", 0)

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

// TestEnterIgnoredWhileStreaming ensures a second submit mid-stream is a no-op.
func TestEnterIgnoredWhileStreaming(t *testing.T) {
	store := testStore(t)
	ag := agent.New(store, fakeProvider{reply: "ok"}, agent.DefaultConfig())
	var m tea.Model = tui.New(context.Background(), ag, "fake", "test-model", 0)

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

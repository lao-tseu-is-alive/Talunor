package agent

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
)

// fakeProvider records the messages it receives and replays a canned reply.
type fakeProvider struct {
	reply   string
	gotMsgs []llm.Message
}

func (f *fakeProvider) Name() string { return "fake" }

func (f *fakeProvider) Chat(_ context.Context, msgs []llm.Message, _ llm.Options) (<-chan llm.Chunk, error) {
	f.gotMsgs = msgs
	ch := make(chan llm.Chunk, 1)
	ch <- llm.Chunk{Content: f.reply}
	close(ch)
	return ch, nil
}

// TestBuildMessagesOrder checks prompt assembly without needing a store or model.
func TestBuildMessagesOrder(t *testing.T) {
	a := &Agent{short: memory.NewShortTerm(6), cfg: DefaultConfig()}
	a.short.Add(llm.RoleUser, "earlier question")
	a.short.Add(llm.RoleAssistant, "earlier answer")

	hits := []memory.Hit{{Memory: memory.Memory{Content: "the sky is blue"}}}
	msgs := a.buildMessages(hits, "new question")

	if len(msgs) != 5 {
		t.Fatalf("got %d messages; want 5 (system, memories, 2 recent, input)", len(msgs))
	}
	if msgs[0].Role != llm.RoleSystem {
		t.Errorf("msg[0] role = %q; want system", msgs[0].Role)
	}
	if !strings.Contains(msgs[1].Content, "the sky is blue") {
		t.Errorf("msg[1] should contain the recalled memory, got %q", msgs[1].Content)
	}
	if msgs[2].Content != "earlier question" || msgs[3].Content != "earlier answer" {
		t.Errorf("recent turns not in order: %q, %q", msgs[2].Content, msgs[3].Content)
	}
	last := msgs[len(msgs)-1]
	if last.Role != llm.RoleUser || last.Content != "new question" {
		t.Errorf("last msg = %+v; want user/new question", last)
	}
}

// TestBuildMessagesNoHits omits the memory block when nothing is recalled.
func TestBuildMessagesNoHits(t *testing.T) {
	a := &Agent{short: memory.NewShortTerm(6), cfg: DefaultConfig()}
	msgs := a.buildMessages(nil, "hello")
	if len(msgs) != 2 {
		t.Fatalf("got %d messages; want 2 (system, input)", len(msgs))
	}
	if msgs[1].Content != "hello" {
		t.Errorf("msg[1] = %q; want the input", msgs[1].Content)
	}
}

// --- integration test (needs `make deps`) ---------------------------------

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

// TestTurnRecallsAndStores drives a full turn: a pre-seeded fact must be recalled
// into the prompt, and both turns must be persisted afterwards.
func TestTurnRecallsAndStores(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)

	// Seed a fact as long-term memory.
	if _, err := store.Remember(ctx, memory.KindDocChunk, "", "The user's favourite colour is teal."); err != nil {
		t.Fatalf("seed: %v", err)
	}

	fake := &fakeProvider{reply: "Your favourite colour is teal."}
	ag := New(store, fake, DefaultConfig())

	out, err := ag.Turn(ctx, "what is my favourite colour?")
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	got, err := drain(out)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if got != fake.reply {
		t.Errorf("reply = %q; want %q", got, fake.reply)
	}

	// The recalled fact must have been injected into the prompt.
	var joined strings.Builder
	for _, m := range fake.gotMsgs {
		joined.WriteString(m.Content)
		joined.WriteByte('\n')
	}
	if !strings.Contains(joined.String(), "teal") {
		t.Errorf("recalled memory not injected into prompt:\n%s", joined.String())
	}

	// Both the user and assistant turns must now be stored (seed + 2 = 3).
	if n, err := store.Count(ctx); err != nil || n != 3 {
		t.Errorf("stored count = %d, err = %v; want 3", n, err)
	}
	if ag.ShortTermLen() != 2 {
		t.Errorf("short-term len = %d; want 2 (user + assistant)", ag.ShortTermLen())
	}
}

func drain(ch <-chan llm.Chunk) (string, error) {
	var sb strings.Builder
	for c := range ch {
		if c.Err != nil {
			return sb.String(), c.Err
		}
		sb.WriteString(c.Content)
	}
	return sb.String(), nil
}

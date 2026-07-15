package memory_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
)

// testConfig builds a Config pointing at the repo-root ext/ artifacts (this
// test file lives two directories below the root) and an ephemeral database.
// It skips the test if `make deps` has not been run.
func testConfig(t *testing.T) memory.Config {
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
	return cfg
}

var corpus = []string{
	"The cat slept on the warm windowsill all afternoon.",
	"Go compiles to a single static binary with no runtime dependencies.",
	"Photosynthesis converts sunlight into chemical energy in plants.",
	"The Eiffel Tower was completed in Paris in 1889.",
	"SQLite stores an entire relational database in a single file.",
}

func openWithCorpus(t *testing.T) (*memory.Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	store, err := memory.Open(testConfig(t))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	for _, text := range corpus {
		if _, err := store.Remember(ctx, memory.KindDocChunk, "", text); err != nil {
			t.Fatalf("remember %q: %v", text, err)
		}
	}
	if n, err := store.Count(ctx); err != nil || n != len(corpus) {
		t.Fatalf("count = %d, err = %v; want %d", n, err, len(corpus))
	}
	return store, ctx
}

func TestRecallRanksBySemantics(t *testing.T) {
	store, ctx := openWithCorpus(t)

	// Query shares no keywords with the target sentence.
	hits, err := store.Recall(ctx, "Which technology keeps a whole database in one file?", 3, 0)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits, got none")
	}
	if !strings.Contains(hits[0].Content, "SQLite") {
		t.Errorf("top hit = %q; want the SQLite sentence", hits[0].Content)
	}
	// Distances must be sorted ascending (nearest first).
	for i := 1; i < len(hits); i++ {
		if hits[i].Distance < hits[i-1].Distance {
			t.Errorf("hits not sorted by distance: %v", hits)
		}
	}
}

func TestRecallThresholdFiltersUnrelated(t *testing.T) {
	store, ctx := openWithCorpus(t)

	// A tight threshold should keep the one clearly-relevant memory and drop the
	// unrelated ones (which sit well above ~0.8 cosine distance).
	hits, err := store.Recall(ctx, "Tell me about a famous French landmark.", 5, 0.75)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least the Eiffel Tower hit")
	}
	if !strings.Contains(hits[0].Content, "Eiffel") {
		t.Errorf("top hit = %q; want the Eiffel Tower sentence", hits[0].Content)
	}
	for _, h := range hits {
		if h.Distance > 0.75 {
			t.Errorf("hit above threshold leaked: d=%.4f %q", h.Distance, h.Content)
		}
	}
}

// TestRecallExcludesAssistantTurns reproduces the "stuck loop" bug: after a
// conversation where the user stated a fact once and the assistant then asked
// for it several times, recalling on a re-ask of that question must surface the
// user's original fact — not the assistant's own repeated clarifying questions,
// which are the closest semantic matches and used to crowd the fact out of the
// top-k.
func TestRecallExcludesAssistantTurns(t *testing.T) {
	ctx := context.Background()
	store, err := memory.Open(testConfig(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// Replay the exact shape of the reported session.
	convo := []struct{ role, content string }{
		{"user", "hy my name is Carlos and i like to develop in Go and Typescript with Bun. and you ?"},
		{"assistant", "Hello, Carlos! Nice to meet you. I'm Talunor. What can I help you build?"},
		{"user", "write me a hello [my name here] in my favorite language please"},
		{"assistant", "Sure! Could you please let me know your name and your favorite programming language?"},
		{"user", "hum i did already tell you"},
		{"assistant", "Ah, I see! Could you please share your name and your favorite language again?"},
	}
	for _, m := range convo {
		if _, err := store.Remember(ctx, memory.KindTurn, m.role, m.content); err != nil {
			t.Fatalf("remember %q: %v", m.content, err)
		}
	}

	// The user re-asks; recall must retrieve the fact and never an assistant turn.
	hits, err := store.Recall(ctx, "can you write me an hello word using my favorite languages ?", 8, 0.75)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	var foundFact bool
	for _, h := range hits {
		if h.Role == "assistant" {
			t.Errorf("assistant turn leaked into recall: %q", h.Content)
		}
		if strings.Contains(h.Content, "Go and Typescript") {
			foundFact = true
		}
	}
	if !foundFact {
		t.Errorf("the user's stated languages were not recalled; got %d hits:\n%s",
			len(hits), formatHits(hits))
	}
}

func formatHits(hits []memory.Hit) string {
	var b strings.Builder
	for _, h := range hits {
		b.WriteString("  - [")
		b.WriteString(h.Role)
		b.WriteString("] ")
		b.WriteString(h.Content)
		b.WriteByte('\n')
	}
	return b.String()
}

func TestForgetDeletesByID(t *testing.T) {
	store, ctx := openWithCorpus(t)

	mems, err := store.List(ctx, 1) // newest row.
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	id := mems[0].ID

	ok, err := store.Forget(ctx, id)
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	if !ok {
		t.Fatalf("forget reported no row removed for existing id %d", id)
	}
	if n, err := store.Count(ctx); err != nil || n != len(corpus)-1 {
		t.Errorf("count after forget = %d, err = %v; want %d", n, err, len(corpus)-1)
	}

	// Forgetting a non-existent id is a no-op that reports ok=false.
	ok, err = store.Forget(ctx, id)
	if err != nil {
		t.Fatalf("forget missing: %v", err)
	}
	if ok {
		t.Errorf("forget reported a removal for an already-deleted id %d", id)
	}
}

func TestRememberReturnsRow(t *testing.T) {
	ctx := context.Background()
	store, err := memory.Open(testConfig(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	m, err := store.Remember(ctx, memory.KindTurn, "user", "hello world")
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	if m.ID == 0 {
		t.Error("expected non-zero id")
	}
	if m.Kind != memory.KindTurn || m.Role != "user" || m.Content != "hello world" {
		t.Errorf("unexpected memory: %+v", m)
	}
	if m.CreatedAt.IsZero() {
		t.Error("expected a parsed created_at timestamp")
	}
}

func TestListReturnsNewestFirst(t *testing.T) {
	store, ctx := openWithCorpus(t)
	mems, err := store.List(ctx, 3)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(mems) != 3 {
		t.Fatalf("got %d; want 3", len(mems))
	}
	// Newest first ⇒ descending ids.
	for i := 1; i < len(mems); i++ {
		if mems[i].ID >= mems[i-1].ID {
			t.Errorf("not newest-first: %d then %d", mems[i-1].ID, mems[i].ID)
		}
	}
	// The last-inserted corpus entry must be first.
	if mems[0].Content != corpus[len(corpus)-1] {
		t.Errorf("first = %q; want %q", mems[0].Content, corpus[len(corpus)-1])
	}
}

func TestShortTermRingBuffer(t *testing.T) {
	st := memory.NewShortTerm(3)
	if st.Len() != 0 {
		t.Fatalf("new buffer len = %d; want 0", st.Len())
	}
	for i, c := range []string{"one", "two", "three", "four", "five"} {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		st.Add(role, c)
	}
	got := st.Recent()
	if len(got) != 3 {
		t.Fatalf("len = %d; want 3 (capacity)", len(got))
	}
	want := []string{"three", "four", "five"}
	for i, w := range want {
		if got[i].Content != w {
			t.Errorf("turn %d = %q; want %q", i, got[i].Content, w)
		}
	}
	// Recent must return a copy — mutating it must not affect the buffer.
	got[0].Content = "mutated"
	if again := st.Recent(); again[0].Content != "three" {
		t.Errorf("Recent did not return a copy: %q", again[0].Content)
	}
}

func TestShortTermClampsCapacity(t *testing.T) {
	st := memory.NewShortTerm(0) // clamped to 1
	st.Add("user", "a")
	st.Add("user", "b")
	if got := st.Recent(); len(got) != 1 || got[0].Content != "b" {
		t.Errorf("clamped buffer = %+v; want single [b]", got)
	}
}

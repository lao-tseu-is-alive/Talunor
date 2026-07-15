package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/llm"
	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
	"github.com/lao-tseu-is-alive/Talunor/internal/tools"
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

// scriptedProvider returns one canned response per Chat call (each a slice of
// chunks), recording the messages of the most recent call. It lets a test drive
// a multi-step tool loop deterministically.
type scriptedProvider struct {
	steps    [][]llm.Chunk
	call     int
	lastMsgs []llm.Message
}

func (p *scriptedProvider) Name() string { return "scripted" }

func (p *scriptedProvider) Chat(_ context.Context, msgs []llm.Message, _ llm.Options) (<-chan llm.Chunk, error) {
	p.lastMsgs = msgs
	step := p.steps[p.call]
	p.call++
	ch := make(chan llm.Chunk, len(step))
	for _, c := range step {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// fakeTool is a tool that records whether it ran; it can require approval.
type fakeTool struct {
	approval bool
	ran      *bool
}

func (fakeTool) Name() string                 { return "danger" }
func (fakeTool) Description() string           { return "a side-effecting tool" }
func (fakeTool) Schema() json.RawMessage       { return json.RawMessage(`{"type":"object"}`) }
func (f fakeTool) RequiresApproval() bool      { return f.approval }
func (f fakeTool) Execute(context.Context, json.RawMessage) (string, error) {
	*f.ran = true
	return "did the thing", nil
}

// drives a tool-requesting turn, calling respond on the approval request.
func runApprovalTurn(t *testing.T, allow bool) (ran bool, final string, lastMsgs []llm.Message) {
	t.Helper()
	ctx := context.Background()
	store := testStore(t)

	prov := &scriptedProvider{steps: [][]llm.Chunk{
		{{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "danger", Args: `{}`}}}},
		{{Content: "All done."}},
	}}
	cfg := DefaultConfig()
	cfg.Tools = tools.NewRegistry(fakeTool{approval: true, ran: &ran})
	cfg.Extractor = DisableReflection()
	ag := New(store, prov, cfg)

	out, err := ag.Turn(ctx, "do the dangerous thing")
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	var b strings.Builder
	var sawApproval bool
	for c := range out {
		if c.Approval != nil {
			sawApproval = true
			if c.Approval.Tool != "danger" {
				t.Errorf("approval tool = %q; want danger", c.Approval.Tool)
			}
			c.Approval.Respond(allow)
			continue
		}
		b.WriteString(c.Content)
	}
	if !sawApproval {
		t.Fatal("expected an approval request for the gated tool")
	}
	return ran, b.String(), prov.lastMsgs
}

// TestApprovalGateAllow: approving runs the tool, and its result reaches the model.
func TestApprovalGateAllow(t *testing.T) {
	ran, final, _ := runApprovalTurn(t, true)
	if !ran {
		t.Error("tool should have run after approval")
	}
	if !strings.Contains(final, "done") && !strings.Contains(final, "All done") {
		t.Errorf("final answer = %q; want the completion", final)
	}
}

// TestApprovalGateDeny: denying skips the tool; the model sees a denial observation.
func TestApprovalGateDeny(t *testing.T) {
	ran, _, lastMsgs := runApprovalTurn(t, false)
	if ran {
		t.Error("tool must NOT run when denied")
	}
	var sawDenial bool
	for _, m := range lastMsgs {
		if m.Role == llm.RoleTool && strings.Contains(m.Content, "denied") {
			sawDenial = true
		}
	}
	if !sawDenial {
		t.Errorf("model should see a denial observation; msgs=%+v", lastMsgs)
	}
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
	// Disable reflection here: this test pins the base loop (recall + store both
	// turns). Reflection is exercised separately, below.
	cfg := DefaultConfig()
	cfg.Extractor = DisableReflection()
	ag := New(store, fake, cfg)

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

// fakeExtractor returns canned facts, ignoring the input — lets us drive the
// reflection path deterministically without an LLM.
type fakeExtractor struct{ facts []string }

func (f fakeExtractor) Extract(context.Context, string) ([]string, error) { return f.facts, nil }

// TestParseFacts checks the model-output parser without any model or store.
func TestParseFacts(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"none sentinel", "NONE", nil},
		{"none lowercase", "none", nil},
		{"one fact", "User's name is Carlos.", []string{"User's name is Carlos."}},
		{"strips bullets", "- User likes Go.\n* User likes TypeScript.", []string{"User likes Go.", "User likes TypeScript."}},
		{"strips numbering + blanks", "1. User uses Bun.\n\n2. User codes in Go.\n", []string{"User uses Bun.", "User codes in Go."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseFacts(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("parseFacts(%q) = %v; want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("fact %d = %q; want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestReflectionStoresAndRecallsFact proves Fix B end to end: after a turn whose
// message states a durable fact, a *distilled* fact is stored (memory.KindFact),
// and a later, differently-worded question recalls it. A fake extractor stands
// in for the LLM so the test is deterministic; retrieval uses the real embedder.
func TestReflectionStoresAndRecallsFact(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)

	cfg := DefaultConfig()
	cfg.Extractor = fakeExtractor{facts: []string{"User's favourite languages are Go and TypeScript."}}
	ag := New(store, &fakeProvider{reply: "Nice to meet you, Carlos!"}, cfg)

	// Turn 1: the user states the fact amid chit-chat.
	out, err := ag.Turn(ctx, "hy my name is Carlos and i like to develop in Go and Typescript with Bun")
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	if _, err := drain(out); err != nil { // draining waits for reflection to finish.
		t.Fatalf("drain: %v", err)
	}

	// A fact memory must now exist (user turn + assistant turn + 1 fact = 3).
	facts := factContents(t, ctx, store)
	if len(facts) != 1 || !strings.Contains(facts[0], "Go and TypeScript") {
		t.Fatalf("expected one distilled fact about the languages, got %v", facts)
	}

	// Turn 2 (differently worded): recall must surface the fact.
	hits, err := store.Recall(ctx, "can you write me an hello word using my favorite languages ?", 8, 0.75)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	var recalledFact bool
	for _, h := range hits {
		if h.Kind == memory.KindFact && strings.Contains(h.Content, "Go and TypeScript") {
			recalledFact = true
		}
	}
	if !recalledFact {
		t.Errorf("distilled fact was not recalled for the re-ask; got %d hits", len(hits))
	}
}

// TestReflectionDeduplicates checks that restating a known fact does not pile up
// near-duplicate fact rows.
func TestReflectionDeduplicates(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)

	cfg := DefaultConfig()
	cfg.Extractor = fakeExtractor{facts: []string{"User's favourite language is Go."}}
	ag := New(store, &fakeProvider{reply: "ok"}, cfg)

	for i := range 3 { // same fact extracted three times.
		out, err := ag.Turn(ctx, "reminder that i love Go")
		if err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
		if _, err := drain(out); err != nil {
			t.Fatalf("drain %d: %v", i, err)
		}
	}
	if facts := factContents(t, ctx, store); len(facts) != 1 {
		t.Errorf("dedup failed: %d fact rows, want 1: %v", len(facts), facts)
	}
}

// factContents returns the content of every stored KindFact memory.
func factContents(t *testing.T, ctx context.Context, store *memory.Store) []string {
	t.Helper()
	mems, err := store.List(ctx, 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var out []string
	for _, m := range mems {
		if m.Kind == memory.KindFact {
			out = append(out, m.Content)
		}
	}
	return out
}

// TestReActToolLoop drives a full act→observe loop: the model asks to call the
// calculator, the agent executes it and feeds the observation back, and the
// model produces a final answer using it. Uses a real tool + store (embeddings)
// but a scripted provider so the two model turns are deterministic.
func TestReActToolLoop(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)

	prov := &scriptedProvider{steps: [][]llm.Chunk{
		// Turn 1: the model requests a tool call (terminal tool-call chunk).
		{{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "calculator", Args: `{"expression":"12*7"}`}}}},
		// Turn 2: with the observation in context, it answers.
		{{Content: "12 times 7 is 84."}},
	}}

	cfg := DefaultConfig()
	cfg.Tools = tools.NewRegistry(tools.Calculator{})
	cfg.Extractor = DisableReflection() // keep the scripted call count exact.
	ag := New(store, prov, cfg)

	out, err := ag.Turn(ctx, "what is 12*7?")
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	got, err := drain(out)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if !strings.Contains(got, "84") {
		t.Errorf("final answer = %q; want it to contain 84", got)
	}

	// The model must have been called twice, and the second call's messages must
	// include the tool's observation ("84") as a RoleTool message.
	if prov.call != 2 {
		t.Errorf("provider called %d times; want 2 (tool step + answer)", prov.call)
	}
	var sawObservation bool
	for _, m := range prov.lastMsgs {
		if m.Role == llm.RoleTool && strings.Contains(m.Content, "84") {
			sawObservation = true
		}
	}
	if !sawObservation {
		t.Errorf("second call missing the tool observation; msgs=%+v", prov.lastMsgs)
	}

	// Only the user turn and the final answer are persisted (tool messages are
	// ephemeral scratch), so count = 2.
	if n, err := store.Count(ctx); err != nil || n != 2 {
		t.Errorf("stored count = %d, err = %v; want 2", n, err)
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

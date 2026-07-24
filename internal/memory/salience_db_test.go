package memory_test

import (
	"context"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
)

// findFact returns the stored fact whose content matches, for inspecting its
// salience/confidence/access bookkeeping after reinforcement.
func findFact(t *testing.T, ctx context.Context, store *memory.Store, content string) memory.Memory {
	t.Helper()
	mems, err := store.List(ctx, 100)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, m := range mems {
		if m.Content == content {
			return m
		}
	}
	t.Fatalf("fact %q not found", content)
	return memory.Memory{}
}

func TestReinforceBumpsSalience(t *testing.T) {
	ctx := context.Background()
	store, err := memory.Open(testConfig(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	const fact = "User lives in Lausanne."
	m, err := store.RememberFact(ctx, fact, memory.ProvenanceUserStated, 0.9)
	if err != nil {
		t.Fatalf("remember fact: %v", err)
	}
	if m.Salience != 1.0 || m.AccessCount != 0 {
		t.Fatalf("fresh fact salience=%v access=%d, want 1.0/0", m.Salience, m.AccessCount)
	}

	if err := store.Reinforce(ctx, []int64{m.ID}); err != nil {
		t.Fatalf("reinforce: %v", err)
	}
	got := findFact(t, ctx, store, fact)
	if got.Salience <= 1.0 {
		t.Errorf("salience not bumped: %v", got.Salience)
	}
	if got.AccessCount != 1 {
		t.Errorf("access_count = %d, want 1", got.AccessCount)
	}
	if got.LastAccessed.IsZero() {
		t.Errorf("last_accessed not set after reinforce")
	}
	// Reinforce must NOT touch confidence — being recalled is not new evidence.
	if got.Confidence != 0.9 {
		t.Errorf("confidence changed by salience-only reinforce: %v", got.Confidence)
	}
}

func TestReinforceFactRaisesConfidenceOnlyOnEvidence(t *testing.T) {
	ctx := context.Background()
	store, err := memory.Open(testConfig(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// Independent evidence (gain > 0): both salience and confidence rise, and
	// confidence stays below the ceiling.
	const indep = "User's favourite editor is Neovim."
	mi, err := store.RememberFact(ctx, indep, memory.ProvenanceUserStated, 0.9)
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	if err := store.ReinforceFact(ctx, mi.ID, 0.34); err != nil {
		t.Fatalf("reinforce fact: %v", err)
	}
	got := findFact(t, ctx, store, indep)
	if !(got.Confidence > 0.9 && got.Confidence < 1.0) {
		t.Errorf("confidence = %v, want in (0.9, 1.0)", got.Confidence)
	}
	if got.Salience <= 1.0 {
		t.Errorf("salience not bumped: %v", got.Salience)
	}

	// No independent evidence (gain == 0, e.g. the model echoing itself): salience
	// still rises, but confidence must not move.
	const echo = "User prefers tabs over spaces."
	me, err := store.RememberFact(ctx, echo, memory.ProvenanceModelInferred, 0.5)
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	if err := store.ReinforceFact(ctx, me.ID, 0); err != nil {
		t.Fatalf("reinforce fact: %v", err)
	}
	got = findFact(t, ctx, store, echo)
	if got.Confidence != 0.5 {
		t.Errorf("confidence moved without independent evidence: %v", got.Confidence)
	}
	if got.Salience <= 1.0 {
		t.Errorf("salience not bumped for echo restatement: %v", got.Salience)
	}
}

// TestRecallForgetFloorAndRevival proves soft forgetting and revival: with a
// forget floor above a fresh memory's salience, recall drops it; reinforcing it
// past the floor brings it back (the row was never deleted).
func TestRecallForgetFloorAndRevival(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)
	cfg.ForgetFloor = 1.4 // above the default salience of 1.0 → fresh facts are "faded".

	store, err := memory.Open(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	const fact = "User drives a red bicycle."
	m, err := store.RememberFact(ctx, fact, memory.ProvenanceUserStated, 0.9)
	if err != nil {
		t.Fatalf("remember: %v", err)
	}

	// Below the floor → soft-forgotten (not returned), though the row exists.
	hits, err := store.Recall(ctx, "what does the user ride?", 5, 0)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if recallContains(hits, fact) {
		t.Fatalf("faded memory should be soft-forgotten, but was recalled")
	}
	if n, _ := store.Count(ctx); n != 1 {
		t.Fatalf("row was deleted, not soft-forgotten: count=%d", n)
	}

	// Reinforce past the floor (1.0 → 1.5) → revived.
	if err := store.Reinforce(ctx, []int64{m.ID}); err != nil {
		t.Fatalf("reinforce: %v", err)
	}
	hits, err = store.Recall(ctx, "what does the user ride?", 5, 0)
	if err != nil {
		t.Fatalf("recall after reinforce: %v", err)
	}
	if !recallContains(hits, fact) {
		t.Errorf("reinforced memory was not revived above the forget floor")
	}
}

// TestReinforcementRaisesRecallScore checks that reinforcing a memory increases the
// combined recall score it is ranked by (same query, same distance, more salient).
func TestReinforcementRaisesRecallScore(t *testing.T) {
	ctx := context.Background()
	store, err := memory.Open(testConfig(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	const fact = "User plays the cello."
	m, err := store.RememberFact(ctx, fact, memory.ProvenanceUserStated, 0.9)
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	const q = "which instrument does the user play?"

	before := scoreOf(t, mustRecall(t, ctx, store, q), fact)
	if err := store.Reinforce(ctx, []int64{m.ID}); err != nil {
		t.Fatalf("reinforce: %v", err)
	}
	after := scoreOf(t, mustRecall(t, ctx, store, q), fact)
	if !(after > before) {
		t.Errorf("recall score did not rise after reinforcement: %v -> %v", before, after)
	}
}

func mustRecall(t *testing.T, ctx context.Context, store *memory.Store, q string) []memory.Hit {
	t.Helper()
	hits, err := store.Recall(ctx, q, 8, 0)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	return hits
}

func recallContains(hits []memory.Hit, content string) bool {
	for _, h := range hits {
		if h.Content == content {
			return true
		}
	}
	return false
}

func scoreOf(t *testing.T, hits []memory.Hit, content string) float64 {
	t.Helper()
	for _, h := range hits {
		if h.Content == content {
			return h.Score
		}
	}
	t.Fatalf("content %q not in recall hits", content)
	return 0
}

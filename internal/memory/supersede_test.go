package memory_test

import (
	"context"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
)

// TestSupersedesTrustModel pins the DEFAULT (personal-assistant) trust model: the
// user and a Verified tool are authoritative and may retire equal-or-lower beliefs;
// the model's own inference retires nothing.
func TestSupersedesTrustModel(t *testing.T) {
	u, ti, mi, un := memory.ProvenanceUserStated, memory.ProvenanceToolObserved,
		memory.ProvenanceModelInferred, memory.ProvenanceUnspecified

	allowed := [][2]memory.Provenance{
		{u, mi}, {u, u}, {u, un}, // the user retires the model / an older user claim / legacy
		{ti, mi}, {ti, un}, {ti, u}, // a verified tool retires the model / legacy / (its domain)
	}
	denied := [][2]memory.Provenance{
		{mi, u}, {mi, ti}, {mi, mi}, {mi, un}, // the model's inference retires NOTHING
	}
	for _, c := range allowed {
		if !memory.Supersedes(c[0], c[1]) {
			t.Errorf("Supersedes(%s, %s) = false, want true", c[0], c[1])
		}
	}
	for _, c := range denied {
		if memory.Supersedes(c[0], c[1]) {
			t.Errorf("Supersedes(%s, %s) = true, want false", c[0], c[1])
		}
	}
}

// TestSupersedeExcludesFromRecall proves soft-supersession: a retired fact vanishes
// from recall but survives (with its pointer) for audit; a fact can't supersede itself.
func TestSupersedeExcludesFromRecall(t *testing.T) {
	ctx := context.Background()
	store, err := memory.Open(testConfig(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	if v, _ := store.SchemaVersion(ctx); v < 5 {
		t.Fatalf("schema version = %d, want >= 5 (supersession migration)", v)
	}

	old, _ := store.RememberFact(ctx, "User lives in Lausanne.", memory.ProvenanceModelInferred, 0.5)
	fresh, _ := store.RememberFact(ctx, "User lives in Geneva.", memory.ProvenanceUserStated, 0.9)

	if err := store.Supersede(ctx, old.ID, fresh.ID); err != nil {
		t.Fatalf("supersede: %v", err)
	}

	const q = "where does the user live?"
	hits, err := store.Recall(ctx, q, 8, 0)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if recallContains(hits, "User lives in Lausanne.") {
		t.Error("superseded fact is still recalled")
	}
	if !recallContains(hits, "User lives in Geneva.") {
		t.Error("active fact should still be recalled")
	}

	// The row survives, with the pointer set (audit + reversibility).
	got, ok, err := store.MemoryByID(ctx, old.ID)
	if err != nil || !ok {
		t.Fatalf("by id: ok=%v err=%v", ok, err)
	}
	if got.SupersededBy != fresh.ID {
		t.Errorf("SupersededBy = %d, want %d", got.SupersededBy, fresh.ID)
	}

	if err := store.Supersede(ctx, old.ID, old.ID); err == nil {
		t.Error("a fact must not be allowed to supersede itself")
	}
}

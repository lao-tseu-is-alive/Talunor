package memory_test

import (
	"context"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
)

func TestEvidenceTrail(t *testing.T) {
	ctx := context.Background()
	store, err := memory.Open(testConfig(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// Migration 4 (the evidence table) is applied on Open.
	if v, _ := store.SchemaVersion(ctx); v < 4 {
		t.Fatalf("schema version = %d, want >= 4 (evidence migration)", v)
	}

	f, err := store.RememberFact(ctx, "User lives in Lausanne.", memory.UserSaid(), 0.9)
	if err != nil {
		t.Fatalf("remember fact: %v", err)
	}

	// Support from two turns and two sources, plus one with an unknown turn (→ NULL).
	for _, e := range []struct {
		turn int64
		src  memory.Provenance
	}{
		{11, memory.ProvenanceUserStated},
		{22, memory.ProvenanceToolObserved},
		{0, memory.ProvenanceModelInferred},
	} {
		if err := store.RecordEvidence(ctx, f.ID, e.turn, e.src); err != nil {
			t.Fatalf("record evidence: %v", err)
		}
	}

	ev, err := store.EvidenceFor(ctx, f.ID)
	if err != nil {
		t.Fatalf("evidence: %v", err)
	}
	if len(ev) != 3 {
		t.Fatalf("evidence rows = %d, want 3", len(ev))
	}
	// Oldest first; turn id and source preserved.
	if ev[0].TurnID != 11 || ev[0].Source != memory.ProvenanceUserStated {
		t.Errorf("ev[0] = %+v, want turn 11 / user_stated", ev[0])
	}
	if ev[1].TurnID != 22 || ev[1].Source != memory.ProvenanceToolObserved {
		t.Errorf("ev[1] = %+v, want turn 22 / tool_observed", ev[1])
	}
	if ev[2].TurnID != 0 { // unknown turn stored as NULL → 0
		t.Errorf("ev[2] turn id = %d, want 0 (NULL)", ev[2].TurnID)
	}

	// A fact with no recorded evidence has an empty trail (e.g. a legacy fact).
	f2, _ := store.RememberFact(ctx, "User likes cheese.", memory.UserSaid(), 0.9)
	if ev2, _ := store.EvidenceFor(ctx, f2.ID); len(ev2) != 0 {
		t.Errorf("fresh fact evidence = %d, want 0", len(ev2))
	}
}

func TestMemoryByID(t *testing.T) {
	ctx := context.Background()
	store, err := memory.Open(testConfig(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	f, _ := store.RememberFact(ctx, "User plays chess.", memory.UserSaid(), 0.9)
	got, ok, err := store.MemoryByID(ctx, f.ID)
	if err != nil {
		t.Fatalf("by id: %v", err)
	}
	if !ok || got.Content != "User plays chess." || got.Kind != memory.KindFact {
		t.Errorf("MemoryByID = %+v ok=%v", got, ok)
	}
	if _, ok, _ := store.MemoryByID(ctx, 999999); ok {
		t.Error("MemoryByID(999999) ok=true, want false")
	}
}

// TestCounterEvidenceMakesAFactContested is the core of Layer 24: a refused
// correction is recorded rather than dropped, and "contested" is DERIVED from that
// record rather than stored beside it (ADR 0005, decision 3).
func TestCounterEvidenceMakesAFactContested(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	fact, err := store.RememberFact(ctx, "The earth is round.",
		memory.Observed(true), 0.9)
	if err != nil {
		t.Fatalf("remember fact: %v", err)
	}
	if err := store.RecordEvidence(ctx, fact.ID, 0, memory.ProvenanceToolObserved); err != nil {
		t.Fatalf("record evidence: %v", err)
	}

	// A supported-only fact is not contested.
	m, ok, err := store.MemoryByID(ctx, fact.ID)
	if err != nil || !ok {
		t.Fatalf("MemoryByID: %v ok=%v", err, ok)
	}
	if m.Contested {
		t.Fatal("a fact with only supporting evidence must not be contested")
	}

	// A refused correction is recorded as counter-evidence.
	const claim = "The earth is flat."
	if err := store.RecordCounterEvidence(ctx, fact.ID, 0, memory.ProvenanceUserStated, claim); err != nil {
		t.Fatalf("record counter-evidence: %v", err)
	}

	m, ok, err = store.MemoryByID(ctx, fact.ID)
	if err != nil || !ok {
		t.Fatalf("MemoryByID: %v ok=%v", err, ok)
	}
	if !m.Contested {
		t.Error("a fact with a contradicting evidence row must report Contested")
	}
	// Decision 5: contestation is visible, not corrosive. The source that lost the
	// authority argument must not win a partial one through arithmetic.
	if m.Confidence != 0.9 {
		t.Errorf("confidence = %.2f; want 0.9 unchanged — counter-evidence must not move it", m.Confidence)
	}
	// Decision 2: the refused claim is NOT a memory of its own.
	all, err := store.List(ctx, 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, mm := range all {
		if mm.Content == claim {
			t.Fatalf("the refused claim was stored as memory #%d — it must live only as evidence detail", mm.ID)
		}
	}

	// Both sides are readable, with the refused text.
	ev, err := store.EvidenceFor(ctx, fact.ID)
	if err != nil {
		t.Fatalf("evidence: %v", err)
	}
	if len(ev) != 2 {
		t.Fatalf("evidence rows = %d, want 2", len(ev))
	}
	if ev[0].Polarity != memory.PolaritySupports || ev[0].Detail != "" {
		t.Errorf("row 0 = %v/%q; want supports with no detail", ev[0].Polarity, ev[0].Detail)
	}
	if ev[1].Polarity != memory.PolarityContradicts {
		t.Errorf("row 1 polarity = %v; want contradicts", ev[1].Polarity)
	}
	if ev[1].Detail != claim {
		t.Errorf("row 1 detail = %q; want the refused claim", ev[1].Detail)
	}
}

// TestContestedFactIsStillRecalled pins decision 4: contestation flags a belief, it
// does not retire it. That is the difference from supersession — and the reason a
// contested fact must keep reaching the prompt (marked), not vanish from it.
func TestContestedFactIsStillRecalled(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	fact, err := store.RememberFact(ctx, "The user's favourite colour is teal.",
		memory.UserSaid(), 0.9)
	if err != nil {
		t.Fatalf("remember fact: %v", err)
	}
	if err := store.RecordCounterEvidence(ctx, fact.ID, 0,
		memory.ProvenanceModelInferred, "The user's favourite colour is red."); err != nil {
		t.Fatalf("counter-evidence: %v", err)
	}

	hits, err := store.Recall(ctx, "what colour does the user like?", 5, 0.9)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	var found *memory.Hit
	for i := range hits {
		if hits[i].ID == fact.ID {
			found = &hits[i]
		}
	}
	if found == nil {
		t.Fatal("a contested fact must still be recalled — it is still the belief")
	}
	if !found.Contested {
		t.Error("the recalled hit must carry Contested so the prompt can mark it")
	}
}

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

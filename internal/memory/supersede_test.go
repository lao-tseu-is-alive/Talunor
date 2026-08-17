package memory_test

import (
	"context"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
)

// TestSupersedesTrustModel pins the DEFAULT (personal-assistant) trust model: the
// user and a Verified tool are authoritative and may retire equal-or-lower beliefs;
// the model's own inference retires nothing; and (Layer 23) authority is scoped to a
// SUBJECT, so nothing retires a claim from another domain.
func TestSupersedesTrustModel(t *testing.T) {
	u := func(s memory.Subject) memory.Attribution { return memory.Attr(memory.ProvenanceUserStated, s) }
	tool := func(s memory.Subject) memory.Attribution { return memory.Attr(memory.ProvenanceToolObserved, s) }
	model := func(s memory.Subject) memory.Attribution { return memory.Attr(memory.ProvenanceModelInferred, s) }
	legacy := memory.Attr(memory.ProvenanceUnspecified, memory.SubjectUnspecified)

	usr, wld := memory.SubjectUser, memory.SubjectWorld

	allowed := [][2]memory.Attribution{
		{u(usr), model(usr)},    // the user corrects the model about themselves
		{u(usr), u(usr)},        // …and their own earlier claim
		{u(usr), legacy},        // …and a pre-Layer-23 row (weaker guarantee, preserved)
		{tool(wld), model(wld)}, // a Verified tool retires a stale inference — ADR 0003's
		{tool(wld), u(wld)},     //   attack-signature case, in its own domain
		{tool(usr), model(usr)},
	}
	denied := [][2]memory.Attribution{
		// The model's inference retires NOTHING, in any domain.
		{model(usr), u(usr)}, {model(wld), tool(wld)}, {model(usr), model(usr)}, {model(wld), legacy},
		// LAYER 23 — the two that were reachable before, and are the reason this
		// layer exists. A user's claim about the world is not authority over the
		// world, whatever the arbiter says…
		{u(wld), tool(wld)}, {u(wld), model(wld)},
		// …and a claim about a DIFFERENT subject is not a contradiction at all:
		// "User believes the earth is flat" cannot retire "The earth is round",
		// even though the user is fully authoritative about themselves.
		{u(usr), tool(wld)}, {u(usr), model(wld)}, {tool(wld), u(usr)},
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

// TestSameSubjectTreatsLegacyAsComparable pins the legacy-data decision: a row
// written before Layer 23 has no subject, and is deliberately comparable with
// anything — freezing such rows (nothing may ever correct them) or guessing their
// subject would both be worse than keeping their old, weaker guarantee.
func TestSameSubjectTreatsLegacyAsComparable(t *testing.T) {
	cases := []struct {
		a, b memory.Subject
		want bool
	}{
		{memory.SubjectUser, memory.SubjectUser, true},
		{memory.SubjectWorld, memory.SubjectWorld, true},
		{memory.SubjectUser, memory.SubjectWorld, false},
		{memory.SubjectWorld, memory.SubjectUser, false},
		{memory.SubjectUnspecified, memory.SubjectWorld, true},
		{memory.SubjectUser, memory.SubjectUnspecified, true},
		{"", memory.SubjectWorld, true},         // unset normalises to unspecified
		{"nonsense", memory.SubjectWorld, true}, // …as does anything invalid
	}
	for _, c := range cases {
		if got := memory.SameSubject(c.a, c.b); got != c.want {
			t.Errorf("SameSubject(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
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

	old, _ := store.RememberFact(ctx, "User lives in Lausanne.", memory.Attr(memory.ProvenanceModelInferred, memory.SubjectUser), 0.5)
	fresh, _ := store.RememberFact(ctx, "User lives in Geneva.", memory.UserSaid(), 0.9)

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

package memory_test

import (
	"context"
	"strings"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
	"github.com/lao-tseu-is-alive/Talunor/internal/testenv"
)

// requireHybrid opens a store and asserts the lexical arm is actually live —
// skipping (or failing, on a host that declared TALUNOR_REQUIRE=fts5) when the
// binary was built without -tags sqlite_fts5. Without this guard the hybrid
// tests would "pass" on a vector-only build by testing nothing, which is the
// failure mode Lesson 22 is about.
func requireHybrid(t *testing.T) *memory.Store {
	t.Helper()
	store := testStore(t)
	if st := store.Lexical(); st != memory.LexicalOK {
		testenv.Require(t, testenv.CapFTS5, errFTS5(st))
	}
	return store
}

type ftsErr string

func (e ftsErr) Error() string { return string(e) }

func errFTS5(st memory.LexicalStatus) error {
	return ftsErr("lexical arm is " + st.String())
}

// TestHybridRecallFindsAnIdentifierVectorsMiss is the reason Layer 22 exists.
// An identifier carries no meaning, so the embedding of a query mentioning it
// lands nowhere near the memory holding it — while BM25, which cannot generalise
// at all, matches the exact token immediately.
func TestHybridRecallFindsAnIdentifierVectorsMiss(t *testing.T) {
	store := requireHybrid(t)
	ctx := context.Background()

	// A realistic corpus: several memories about the same *topic*, one of which
	// carries the identifier. Semantically they are near-indistinguishable.
	corpus := []string{
		"The quarterly planning meeting was moved to the main conference room.",
		"Budget review sessions happen every second Tuesday of the month.",
		"The contract reference for the Lausanne renovation is AFF-2024-113.",
		"Meeting notes are archived in the shared drive after each session.",
		"Travel expenses must be submitted before the end of the quarter.",
	}
	for _, c := range corpus {
		if _, err := store.RememberFact(ctx, c, memory.ProvenanceUserStated, 0.9); err != nil {
			t.Fatalf("remember: %v", err)
		}
	}

	const query = "AFF-2024-113"
	hits, err := store.Recall(ctx, query, 3, 0.75)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("hybrid recall returned nothing for an exact identifier")
	}
	if !strings.Contains(hits[0].Content, "AFF-2024-113") {
		t.Errorf("top hit = %q; want the memory carrying the identifier", hits[0].Content)
	}
	if !hits[0].FromLexical() {
		t.Errorf("the identifier hit should come from the lexical arm; ranks: vector=%d lexical=%d",
			hits[0].VectorRank, hits[0].LexicalRank)
	}
}

// TestVectorOnlyRecallStillWorksSemantically: the lexical arm must not cost us
// the meaning-based retrieval that made the memory useful in the first place.
func TestHybridKeepsSemanticRecall(t *testing.T) {
	store := requireHybrid(t)
	ctx := context.Background()

	for _, c := range []string{
		"SQLite stores an entire relational database in a single file.",
		"The Eiffel Tower was completed in Paris in 1889.",
	} {
		if _, err := store.RememberFact(ctx, c, memory.ProvenanceUserStated, 0.9); err != nil {
			t.Fatalf("remember: %v", err)
		}
	}
	// No shared keyword with the stored sentence — only meaning connects them.
	hits, err := store.Recall(ctx, "Which technology keeps a whole database in one file?", 2, 0.75)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(hits) == 0 || !strings.Contains(hits[0].Content, "SQLite") {
		t.Fatalf("semantic recall broke: hits = %+v", hits)
	}
	if !hits[0].HasVector() {
		t.Error("the semantic hit should carry a vector distance")
	}
}

// TestLexicalArmRespectsSupersession: a retired memory must not walk back in
// through the lexical door just because it contains the words you typed.
func TestLexicalArmRespectsSupersession(t *testing.T) {
	store := requireHybrid(t)
	ctx := context.Background()

	old, err := store.RememberFact(ctx, "The deployment token is DEPL-OLD-77.", memory.ProvenanceUserStated, 0.9)
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	fresh, err := store.RememberFact(ctx, "The deployment token is DEPL-NEW-91.", memory.ProvenanceUserStated, 0.9)
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	if err := store.Supersede(ctx, old.ID, fresh.ID); err != nil {
		t.Fatalf("supersede: %v", err)
	}
	hits, err := store.Recall(ctx, "DEPL-OLD-77", 5, 0.75)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	for _, h := range hits {
		if h.ID == old.ID {
			t.Errorf("superseded memory #%d came back through the lexical arm: %q", h.ID, h.Content)
		}
	}
}

// TestLexicalArmRespectsSoftForgetting: same door, the other Layer-17 guarantee.
// A memory faded below the forget floor stays out of the prompt path — but
// consolidation, which is allowed to see faded rows, must still find it.
func TestLexicalArmRespectsSoftForgetting(t *testing.T) {
	store := testStoreWithConfig(t, func(c *memory.Config) {
		c.SalienceHalfLife = 1 // effectively instant decay
		c.ForgetFloor = 0.9    // and a floor almost nothing clears
	})
	if st := store.Lexical(); st != memory.LexicalOK {
		testenv.Require(t, testenv.CapFTS5, errFTS5(st))
	}
	ctx := context.Background()

	if _, err := store.RememberFact(ctx, "Legacy ticket ZZ-4242 was closed years ago.",
		memory.ProvenanceUserStated, 0.9); err != nil {
		t.Fatalf("remember: %v", err)
	}
	hits, err := store.Recall(ctx, "ZZ-4242", 5, 0.75)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("soft-forgotten memory returned to the prompt path via the lexical arm: %+v", hits)
	}
	consolidation, err := store.RecallForConsolidation(ctx, "ZZ-4242", 5, 0.75)
	if err != nil {
		t.Fatalf("recall for consolidation: %v", err)
	}
	if len(consolidation) == 0 {
		t.Error("consolidation must still see the faded memory, or a restatement duplicates it")
	}
}

// TestConsolidationLookupIgnoresLexicalOverlap pins the boundary hybrid recall
// must not cross. RecallForConsolidation answers "do I already hold this fact?",
// and the answer has to be metric: two sentences can share every unusual word
// and still state different things. When the lexical arm was allowed into this
// path, reflection consolidated a new fact onto a merely word-similar one and
// stopped storing it at all — caught by an existing Layer-20 test, not this one.
func TestConsolidationLookupIgnoresLexicalOverlap(t *testing.T) {
	store := requireHybrid(t)
	ctx := context.Background()

	if _, err := store.RememberFact(ctx, "The Lausanne office network switch is model NX-9000.",
		memory.ProvenanceUserStated, 0.9); err != nil {
		t.Fatalf("remember: %v", err)
	}
	// Shares the rare token "NX-9000" — the lexical arm would rank it first — but
	// is a different claim, far away in embedding space.
	const different = "The NX-9000 firmware upgrade is scheduled for the winter break."

	cons, err := store.RecallForConsolidation(ctx, different, 3, 0.25) // tight identity radius
	if err != nil {
		t.Fatalf("recall for consolidation: %v", err)
	}
	for _, h := range cons {
		if h.FromLexical() {
			t.Errorf("lexical hit reached the consolidation path: #%d %q", h.ID, h.Content)
		}
		if h.Distance > 0.25 {
			t.Errorf("hit outside the identity radius: d=%.4f %q", h.Distance, h.Content)
		}
	}
	// The prompt path, in contrast, is expected to surface it.
	hits, err := store.Recall(ctx, different, 3, 0.25)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	var sawLexical bool
	for _, h := range hits {
		if h.FromLexical() {
			sawLexical = true
		}
	}
	if !sawLexical {
		t.Error("the prompt path should still retrieve the shared identifier through the lexical arm")
	}
}

// TestForgetRemovesFromLexicalIndex proves the FTS triggers really fire: a
// deleted memory must vanish from the lexical index too, not linger as a ghost
// row pointing at an id that no longer exists.
func TestForgetRemovesFromLexicalIndex(t *testing.T) {
	store := requireHybrid(t)
	ctx := context.Background()

	m, err := store.RememberFact(ctx, "Serial number QX-8891 belongs to the old printer.",
		memory.ProvenanceUserStated, 0.9)
	if err != nil {
		t.Fatalf("remember: %v", err)
	}
	if hits, err := store.Recall(ctx, "QX-8891", 5, 0.75); err != nil || len(hits) == 0 {
		t.Fatalf("precondition: the memory should be findable (err=%v hits=%d)", err, len(hits))
	}
	if _, err := store.Forget(ctx, m.ID); err != nil {
		t.Fatalf("forget: %v", err)
	}
	hits, err := store.Recall(ctx, "QX-8891", 5, 0.75)
	if err != nil {
		t.Fatalf("recall after forget: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("forgotten memory still in the lexical index: %+v", hits)
	}
}

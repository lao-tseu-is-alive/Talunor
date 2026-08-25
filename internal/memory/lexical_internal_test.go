package memory

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/testenv"
)

// internalTestStore mirrors the package-external testStore helper, but inside the
// package so a test can reach Store.db and corrupt the lexical index on purpose.
func internalTestStore(t *testing.T) *Store {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	cfg := Config{
		DBPath:         ":memory:",
		VectorExtPath:  filepath.Join(root, "ext", "vector"),
		AIExtPath:      filepath.Join(root, "ext", "ai"),
		EmbedModelPath: filepath.Join(root, "ext", "models", "all-MiniLM-L6-v2.f16.gguf"),
	}
	_, err := os.Stat(cfg.VectorExtPath + ".so")
	testenv.Require(t, testenv.CapExt, err)
	store, err := Open(cfg)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// TestRecallSurvivesLexicalArmFailure is the regression test for the fail-open
// lexical arm. A build WITHOUT FTS5 degrades gracefully to vector-only — that is
// the package's stated posture — but a RUNTIME failure of the index (corruption,
// schema drift) used to return an error from Recall and lose an answer the vector
// arm had already produced. A degraded memory must still be a working memory.
//
// It must also stay an OBSERVABLE degradation: Lexical() reports LexicalFailed
// afterwards, so doctor and /mem can say why recall got quieter.
func TestRecallSurvivesLexicalArmFailure(t *testing.T) {
	store := internalTestStore(t)
	if store.Lexical() != LexicalOK {
		testenv.Require(t, testenv.CapFTS5, ftsUnavailable(store.Lexical()))
	}
	ctx := context.Background()

	if _, err := store.Remember(ctx, KindDocChunk, "", "The user's favourite colour is teal."); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Sanity: recall works while both arms are healthy.
	if hits, err := store.Recall(ctx, "what colour do I like?", 5, 0.9); err != nil || len(hits) == 0 {
		t.Fatalf("baseline recall: %d hits, err=%v", len(hits), err)
	}

	// Break the lexical arm underneath a live store, exactly as corruption would.
	if _, err := store.db.ExecContext(ctx, `DROP TABLE `+ftsTable); err != nil {
		t.Fatalf("drop fts table: %v", err)
	}

	hits, err := store.Recall(ctx, "what colour do I like?", 5, 0.9)
	if err != nil {
		t.Fatalf("recall must fail SOFT to the vector arm, got error: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("recall returned no hits; the vector arm alone should still answer")
	}
	if got := store.Lexical(); got != LexicalFailed {
		t.Errorf("Lexical() = %v; want LexicalFailed so the degradation is visible", got)
	}
}

type ftsUnavailableErr string

func (e ftsUnavailableErr) Error() string { return string(e) }

func ftsUnavailable(st LexicalStatus) error {
	return ftsUnavailableErr("lexical arm is " + st.String())
}

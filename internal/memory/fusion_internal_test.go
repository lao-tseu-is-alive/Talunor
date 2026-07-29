package memory

import (
	"strings"
	"testing"
)

// These tests are the reason Layer 22's core is two pure functions: matchExpression
// and fuse decide what recall retrieves and in what order, and neither needs a
// database, an embedding model or an FTS5 build to be pinned down. They run on
// every host, including one that cannot exercise the lexical arm at all.

func TestMatchExpressionNeutralisesFTS5Syntax(t *testing.T) {
	cases := []struct {
		name  string
		query string
		want  string
	}{
		{"plain words", "deployment token", `"deployment" OR "token"`},
		{"identifier keeps its parts", "AFF-2024-113", `"aff" OR "2024" OR "113"`},
		// Everything below would be a syntax error or an unintended operator if it
		// reached MATCH unquoted.
		{"apostrophe", "what's my name?", `"name"`},
		{"fts5 operators", `cat AND dog NOT bird`, `"cat" OR "dog" OR "bird"`},
		{"prefix star", "budget*", `"budget"`},
		{"quotes and parens", `"(rm -rf)"`, `"rm" OR "rf"`},
		{"column filter attempt", "content:secret", `"content" OR "secret"`},
		{"near operator", "NEAR(a b)", `"near"`}, // single letters dropped
		{"unicode is a word", "Genève réunion", `"genève" OR "réunion"`},
		{"duplicates collapse", "token token TOKEN", `"token"`},
		{"nothing usable", "?! - ' ()", ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchExpression(tc.query); got != tc.want {
				t.Errorf("matchExpression(%q) = %q; want %q", tc.query, got, tc.want)
			}
		})
	}
}

// TestMatchExpressionDropsFunctionWords is a regression test with a scar. Before
// the stopword filter, "what language does he like?" matched a memory on the
// pronoun "he" — and since the lexical arm has no distance threshold to fail,
// that junk arrived as a confident top hit for a question the vector arm had
// honestly answered with nothing. A lexical arm that fires on function words
// does not add recall, it adds lies.
func TestMatchExpressionDropsFunctionWords(t *testing.T) {
	cases := []struct{ query, want string }{
		{"what language does he like?", `"language" OR "like"`},
		{"where does he live?", `"live"`},
		// Accents survive into the MATCH expression on purpose: FTS5 tokenises the
		// query with the same remove_diacritics setting as the index, so
		// "référence" finds the indexed "reference" without us folding anything.
		{"quelle est la référence du contrat ?", `"référence" OR "contrat"`},
		{"is it in the office or at home", `"office" OR "home"`},
		// …but a digit-bearing token is never dropped, however short: that is the
		// whole reason the lexical arm exists.
		{"PostgreSQL 16.2", `"postgresql" OR "16" OR "2"`},
		{"room 4", `"room" OR "4"`},
		// A question made only of function words must produce nothing at all,
		// so the lexical arm is skipped rather than matching everything.
		{"what is it about?", ""},
		{"quand est-ce que je dois payer ?", `"dois" OR "payer"`},
	}
	for _, tc := range cases {
		if got := matchExpression(tc.query); got != tc.want {
			t.Errorf("matchExpression(%q) = %q; want %q", tc.query, got, tc.want)
		}
	}
}

func TestMatchExpressionCapsTermCount(t *testing.T) {
	long := strings.Repeat("alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu nu xi ", 3)
	got := matchExpression(long)
	if n := strings.Count(got, " OR ") + 1; n > maxMatchTerms {
		t.Errorf("kept %d terms; want at most %d", n, maxMatchTerms)
	}
}

// hit builds a candidate with just the fields ranking looks at.
func hit(id int64, distance, confidence, eff float64) Hit {
	h := Hit{Distance: distance, effSalience: eff}
	h.ID = id
	h.Confidence = confidence
	return h
}

// TestFuseWithOneArmKeepsLayer17Ranking is the compatibility guarantee: on a
// build without FTS5, recall must rank exactly as it did before this layer —
// relevance × confidence × effective salience, with relevance = 1-distance.
// The case worth pinning is the one where that formula overrules proximity: a
// nearer but half-trusted memory (0.9 × 0.5 = 0.45) must lose to a slightly
// further, fully-trusted one (0.7 × 1.0 = 0.70). Layer 16 buys exactly this.
func TestFuseWithOneArmKeepsLayer17Ranking(t *testing.T) {
	near := hit(1, 0.10, 0.5, 1.0)  // very close, half-trusted
	solid := hit(2, 0.30, 1.0, 1.0) // slightly further, fully trusted

	got := fuse([]Hit{near, solid}, nil, 2)
	if len(got) != 2 {
		t.Fatalf("got %d hits; want 2", len(got))
	}
	if got[0].ID != 2 {
		t.Errorf("top hit = #%d; want #2: 0.7×1.0 must beat 0.9×0.5", got[0].ID)
	}
	if want := (1 - 0.30) * 1.0 * 1.0; got[0].Score != want {
		t.Errorf("top score = %v; want the classic (1-d)·conf·eff = %v", got[0].Score, want)
	}
	if want := (1 - 0.10) * 0.5 * 1.0; got[1].Score != want {
		t.Errorf("second score = %v; want %v", got[1].Score, want)
	}
	// And the vector rank is recorded even when it is the only arm.
	if got[0].VectorRank == 0 {
		t.Error("vector rank not stamped")
	}
}

// TestFuseRewardsCorroboration: a memory both arms found should outrank one that
// only a single arm found, all else equal. That is the whole point of summing
// reciprocal ranks rather than picking a winner.
func TestFuseRewardsCorroboration(t *testing.T) {
	both := hit(1, 0.40, 1.0, 1.0)
	vectorOnly := hit(2, 0.35, 1.0, 1.0) // slightly nearer, but lexically silent

	lexical := []Hit{hit(1, noVectorDistance, 1.0, 1.0)}
	got := fuse([]Hit{vectorOnly, both}, lexical, 2)

	if got[0].ID != 1 {
		t.Errorf("top hit = #%d; want #1, found by both arms", got[0].ID)
	}
	if got[0].VectorRank == 0 || got[0].LexicalRank == 0 {
		t.Errorf("corroborated hit should carry both ranks, got vector=%d lexical=%d",
			got[0].VectorRank, got[0].LexicalRank)
	}
}

// TestFuseAdmitsLexicalOnlyHits: the identifier case. A memory the vector arm
// never returned must still reach the result — otherwise the layer buys nothing.
func TestFuseAdmitsLexicalOnlyHits(t *testing.T) {
	vector := []Hit{hit(1, 0.50, 1.0, 1.0)}
	lexical := []Hit{hit(9, noVectorDistance, 1.0, 1.0)}

	got := fuse(vector, lexical, 5)
	var found bool
	for _, h := range got {
		if h.ID == 9 {
			found = true
			if h.HasVector() {
				t.Error("a lexical-only hit must not claim a vector distance")
			}
			if !h.FromLexical() {
				t.Error("a lexical-only hit must report its lexical rank")
			}
		}
	}
	if !found {
		t.Error("lexical-only hit was dropped by fusion")
	}
}

// TestFuseStillHonoursTrustAndSalience: fusion changes only the relevance term.
// A perfectly-matched memory that the agent barely trusts must not outrank a
// well-corroborated one — Layers 16 and 17 keep their say.
func TestFuseStillHonoursTrustAndSalience(t *testing.T) {
	doubtful := hit(1, 0.05, 0.1, 1.0) // rank 1 in both arms, but 10% confidence
	trusted := hit(2, 0.40, 1.0, 1.0)  // rank 2, fully trusted

	got := fuse([]Hit{doubtful, trusted}, []Hit{hit(1, noVectorDistance, 0.1, 1.0)}, 2)
	if got[0].ID != 2 {
		t.Errorf("top hit = #%d; want #2 — confidence must still discount a doubtful match", got[0].ID)
	}
}

func TestFuseTruncatesToK(t *testing.T) {
	var vector []Hit
	for i := int64(1); i <= 10; i++ {
		vector = append(vector, hit(i, float64(i)/20, 1.0, 1.0))
	}
	if got := fuse(vector, nil, 3); len(got) != 3 {
		t.Errorf("len = %d; want 3", len(got))
	}
	if got := fuse(vector, nil, 0); len(got) != 10 {
		t.Errorf("k=0 should not truncate, got %d", len(got))
	}
}

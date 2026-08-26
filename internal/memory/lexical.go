package memory

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

// LAYER 22 — the lexical half of hybrid recall.
//
// Embeddings retrieve by MEANING, which is exactly wrong for the things that
// carry no meaning: an identifier (`AFF-2024-113`), a rare proper noun, a
// version number, an error code. "AFF-2024-113" and "AFF-2024-114" sit at almost
// the same point in vector space, and neither is near the sentence you typed to
// look them up. A lexical index has the opposite bias — it cannot generalise, but
// it never confuses two strings that differ.
//
// So Recall runs BOTH and fuses the results (see fuse in hybrid.go). This file
// owns the lexical arm: an SQLite FTS5 index over memories.content, kept in sync
// by triggers, queried with BM25 ranking.
//
// # Why this index is not a migration
//
// Everything here is a DERIVED index: every byte can be rebuilt from the
// memories table. Putting it in the ordered migration list would make
// schema_version claim something the database may not be able to honour — see
// LexicalStatus below — so the index is (re)created idempotently at Open
// instead, like vector_init. Migrations stay reserved for source data.
//
// # The FTS5 build tag
//
// mattn/go-sqlite3 compiles FTS5 in ONLY under `-tags sqlite_fts5`. A Talunor
// built without it has no `fts5` module and no `bm25()` — the driver's build
// tags are part of this feature's contract, as load-bearing as any schema. The
// Makefile, Dockerfile and CI all pass the tag; when it is missing anyway,
// recall degrades to vector-only and SAYS SO (doctor, /mem, /debug) rather than
// quietly retrieving less. Tests declare the capability through
// internal/testenv, so a host that should have it fails instead of skipping.
const (
	ftsTable = "memories_fts"

	// lexicalCandidateFactor over-fetches lexical candidates for the same reason
	// the vector arm over-fetches: assistant turns, soft-forgotten and superseded
	// rows are dropped after the query, and the survivors are re-ranked.
	lexicalCandidateFactor = 4

	// maxMatchTerms caps how many terms of a query reach FTS5. A long question
	// contributes mostly noise words; the rare term that makes lexical search
	// worth having is nearly always in the first few.
	maxMatchTerms = 12

	// minTermLen drops one-character tokens ("a", "à", punctuation debris), which
	// match everywhere and rank nothing.
	minTermLen = 2
)

// stopwords are the function words that must never reach MATCH.
//
// This list is not an optimisation, it is a correctness fix. BM25 down-weights
// common terms but does not refuse them, so a question like "what language does
// he like?" happily matched a memory on the pronoun "he" — and because the
// lexical arm has no distance threshold to fail, that junk match arrived as a
// confident top hit where the vector arm had honestly returned nothing. The
// lexical arm earns its place on rare tokens; common ones only let it lie.
//
// English and French together, because this memory holds both (see the
// tokenizer's remove_diacritics setting). Deliberately short: it covers
// articles, pronouns, prepositions, auxiliaries and question words, and stops
// there — anything longer starts discarding terms a user might genuinely be
// searching for.
var stopwords = map[string]bool{
	// English
	"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
	"is": true, "are": true, "was": true, "were": true, "be": true, "been": true,
	"do": true, "does": true, "did": true, "have": true, "has": true, "had": true,
	"i": true, "you": true, "he": true, "she": true, "it": true, "we": true, "they": true,
	"my": true, "your": true, "his": true, "her": true, "its": true, "our": true, "their": true,
	"me": true, "him": true, "them": true, "this": true, "that": true, "these": true, "those": true,
	"of": true, "in": true, "on": true, "at": true, "to": true, "for": true, "with": true,
	"from": true, "by": true, "as": true, "about": true, "into": true, "over": true,
	"what": true, "which": true, "who": true, "whom": true, "when": true, "where": true,
	"why": true, "how": true, "can": true, "could": true, "would": true, "should": true,
	"will": true, "not": true, "no": true, "yes": true, "please": true, "there": true,
	// French
	"le": true, "la": true, "les": true, "un": true, "une": true, "des": true, "du": true,
	"de": true, "et": true, "ou": true, "mais": true, "est": true, "sont": true, "etait": true,
	"ete": true, "avoir": true, "etre": true, "je": true, "tu": true, "il": true, "elle": true,
	"nous": true, "vous": true, "ils": true, "elles": true, "mon": true, "ma": true, "mes": true,
	"ton": true, "ta": true, "son": true, "sa": true, "ses": true, "notre": true, "votre": true,
	"leur": true, "ce": true, "cet": true, "cette": true, "ces": true, "qui": true, "que": true,
	"quoi": true, "quel": true, "quelle": true, "quand": true, "comment": true,
	"pourquoi": true, "dans": true, "sur": true, "pour": true, "avec": true, "par": true,
	"au": true, "aux": true, "en": true, "ne": true, "pas": true, "plus": true, "moins": true,
	"si": true, "svp": true,
	// Accented forms matter here: FTS5's tokenizer folds diacritics, but this Go
	// filter sees the raw query, so "était" never matches the entry "etait".
	// Both spellings are listed rather than pulling in a Unicode-folding
	// dependency for a fixed, short list.
	"était": true, "été": true, "où": true, "à": true, "ça": true, "déjà": true,
	"très": true, "être": true, "près": true,
}

// keepTerm decides whether a query token is worth searching for.
func keepTerm(t string) bool {
	// A token carrying a digit is almost always the reason to run a lexical
	// search at all — an identifier, a version, a year, a room number. Keep it
	// whatever its length: "16" in "PostgreSQL 16.2" is the discriminating part.
	if hasDigit(t) {
		return true
	}
	return len([]rune(t)) >= minTermLen && !stopwords[t]
}

func hasDigit(s string) bool {
	for _, r := range s {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// LexicalStatus reports whether the lexical arm of recall is available, and why
// not when it is not. It is set once at Open — except LexicalFailed, which a
// runtime failure latches later (see Store.Lexical).
type LexicalStatus int

const (
	// LexicalUnknown is the zero value: Open has not run.
	LexicalUnknown LexicalStatus = iota
	// LexicalOK means the FTS5 index exists and recall is hybrid.
	LexicalOK
	// LexicalUnavailable means this binary was built without `-tags sqlite_fts5`
	// (no fts5 module), so recall is vector-only. Everything still works; exact
	// identifiers and rare tokens are simply harder to retrieve.
	LexicalUnavailable
	// LexicalDisabled means the operator turned hybrid recall off
	// (TALUNOR_RECALL=vector).
	LexicalDisabled
	// LexicalFailed means the arm was available at Open but FAILED AT RUNTIME (a
	// corrupt index, schema drift). Recall degraded itself to vector-only rather
	// than failing outright; this status is how that degradation stays visible.
	LexicalFailed
)

func (l LexicalStatus) String() string {
	switch l {
	case LexicalOK:
		return "ok"
	case LexicalUnavailable:
		return "unavailable (built without -tags sqlite_fts5)"
	case LexicalDisabled:
		return "disabled (TALUNOR_RECALL=vector)"
	case LexicalFailed:
		return "failed at runtime — recall degraded to vector-only"
	default:
		return "unknown"
	}
}

// Lexical reports the state of the lexical arm (see LexicalStatus). An arm that
// was healthy at Open but has since failed at runtime reports LexicalFailed —
// recall keeps working on the vector arm alone, and this is what says so.
func (s *Store) Lexical() LexicalStatus {
	if s.lexicalBroken.Load() {
		return LexicalFailed
	}
	return s.lexical
}

// initLexical creates the FTS5 index and its synchronisation triggers, and
// records whether the lexical arm is usable. A missing fts5 module is NOT an
// error: it degrades recall to vector-only, which is a reduced service, not a
// broken one. Called from bootstrap, after the migrations.
func (s *Store) initLexical(ctx context.Context) error {
	if !s.cfg.hybridEnabled() {
		s.lexical = LexicalDisabled
		return nil
	}
	// An external-content index: FTS5 stores only the inverted index and reads the
	// text from memories, so the content is never duplicated and can never drift
	// out of sync with the row it indexes.
	//
	// unicode61 + remove_diacritics 2 folds "Genève"/"Geneve" together and is
	// language-neutral — deliberately NO stemmer, because this memory holds French
	// and English side by side and the porter stemmer would mangle the French.
	create := fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(
			content,
			content='memories',
			content_rowid='id',
			tokenize="unicode61 remove_diacritics 2"
		)`, ftsTable)
	if _, err := s.db.ExecContext(ctx, create); err != nil {
		if isMissingFTS5(err) {
			s.lexical = LexicalUnavailable
			return nil
		}
		return fmt.Errorf("create %s: %w", ftsTable, err)
	}
	// External-content tables do not follow their source automatically: these are
	// the standard triggers from the FTS5 documentation. Writing them by hand (in
	// SQL, once) beats remembering to update an index in every Go write path —
	// Remember, Forget, Supersede and ReEmbed all stay unaware of this file.
	triggers := fmt.Sprintf(`
		CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
			INSERT INTO %[1]s(rowid, content) VALUES (new.id, new.content);
		END;
		CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
			INSERT INTO %[1]s(%[1]s, rowid, content) VALUES('delete', old.id, old.content);
		END;
		CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE OF content ON memories BEGIN
			INSERT INTO %[1]s(%[1]s, rowid, content) VALUES('delete', old.id, old.content);
			INSERT INTO %[1]s(rowid, content) VALUES (new.id, new.content);
		END;`, ftsTable)
	if _, err := s.db.ExecContext(ctx, triggers); err != nil {
		return fmt.Errorf("create %s triggers: %w", ftsTable, err)
	}
	// A database written before this layer (or by a build without the tag) has
	// rows the index never saw. 'rebuild' re-reads them all from memories; it is
	// O(rows) and runs once per open, which for a personal memory of thousands of
	// rows is milliseconds — and it is also the repair for any index that ever
	// falls behind.
	if err := s.rebuildLexical(ctx); err != nil {
		return err
	}
	s.lexical = LexicalOK
	return nil
}

// rebuildLexical re-indexes every memory from scratch.
func (s *Store) rebuildLexical(ctx context.Context) error {
	stmt := fmt.Sprintf(`INSERT INTO %[1]s(%[1]s) VALUES('rebuild')`, ftsTable)
	if _, err := s.db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("rebuild %s: %w", ftsTable, err)
	}
	return nil
}

// isMissingFTS5 recognises the one failure that means "this binary was built
// without the FTS5 module" — as opposed to a real SQL error, which must surface.
func isMissingFTS5(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "no such module: fts5")
}

// matchExpression turns arbitrary user text into a safe FTS5 MATCH expression.
//
// This is not cosmetic. FTS5 has its own query language — `AND`, `OR`, `NOT`,
// `NEAR`, `*`, `^`, `"…"`, `(`, `)` — so feeding a raw question to MATCH either
// throws a syntax error ("what's my name?" — a bare apostrophe) or silently
// means something the user never asked for. So: tokenise here, keep only
// alphanumeric runs, quote each token (which makes every FTS5 operator inert),
// and OR them together — keeping only the terms worth searching for (keepTerm:
// no function words, but always anything carrying a digit).
//
// OR rather than AND is deliberate: with AND, one absent word rejects the
// document. With OR, BM25's inverse-document-frequency does the discriminating
// on its own — a rare identifier outweighs a dozen common words, which is
// exactly the retrieval we are missing from the vector arm. Returns "" when
// nothing usable is left, and the caller then skips the lexical arm.
func matchExpression(query string) string {
	fields := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	terms := make([]string, 0, len(fields))
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		t := strings.ToLower(f)
		if seen[t] || !keepTerm(t) {
			continue
		}
		seen[t] = true
		terms = append(terms, `"`+t+`"`)
		if len(terms) == maxMatchTerms {
			break
		}
	}
	return strings.Join(terms, " OR ")
}

// lexicalCandidates runs the lexical arm: the BM25-ranked rows matching any term
// of the query, newest-BM25-first, in the same shape the vector arm returns.
// Superseded rows are excluded in SQL (as in the vector arm); role and salience
// filtering happen in the shared post-processing.
//
// bm25() returns a NEGATIVE number (more negative = better match), which is
// SQLite's convention for "ORDER BY rank ascending". Callers only use the
// resulting ORDER, so the sign never leaks further than this query.
func (s *Store) lexicalCandidates(ctx context.Context, query string, limit int) ([]Hit, error) {
	if s.lexical != LexicalOK {
		return nil, nil
	}
	match := matchExpression(query)
	if match == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT m.id, m.kind, COALESCE(m.role, ''), m.content,
		       COALESCE(m.provenance, 'unspecified'), COALESCE(m.subject, 'unspecified'),
		       COALESCE(m.confidence, 1.0),
		       COALESCE(m.salience, 1.0), m.last_accessed, COALESCE(m.access_count, 0),
		       m.created_at, `+contestedExpr("m")+`, bm25(%[1]s)
		FROM %[1]s
		JOIN memories m ON m.id = %[1]s.rowid
		WHERE %[1]s MATCH ? AND m.superseded_by IS NULL
		ORDER BY bm25(%[1]s)
		LIMIT ?`, ftsTable), match, limit)
	if err != nil {
		return nil, fmt.Errorf("lexical recall: %w", err)
	}
	defer rows.Close()

	var out []Hit
	for rows.Next() {
		h, err := scanHit(rows, scanLexical)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

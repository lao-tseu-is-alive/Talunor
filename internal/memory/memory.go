package memory

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Kind classifies a stored memory.
type Kind string

const (
	// KindTurn is a single conversation message (has a role). This is *episodic*
	// memory: the verbatim record of what was said, and when.
	KindTurn Kind = "turn"
	// KindDocChunk is a chunk of an ingested document (no role).
	KindDocChunk Kind = "doc_chunk"
	// KindFact is a durable, distilled statement the agent chose to remember —
	// written by its reflection step, not copied verbatim from a message. This is
	// *semantic* memory: timeless knowledge ("User likes Go"), independent of the
	// turn that produced it. It has no role.
	KindFact Kind = "fact"
)

// Provenance records where a stored memory came from — the basis for how much to
// trust it. Confidence is assigned by the SYSTEM from the provenance (and, for
// learned facts, scaled by the model's calibration), never self-reported by the
// model: a model's own confidence is not calibrated (see the calibration lessons).
type Provenance string

const (
	// ProvenanceUserStated: grounded in the user's own words (a user turn, or a
	// fact distilled from what the user said).
	ProvenanceUserStated Provenance = "user_stated"
	// ProvenanceModelInferred: produced by the model (an assistant turn, or a fact
	// the model inferred beyond what the user stated) — trust it less.
	ProvenanceModelInferred Provenance = "model_inferred"
	// ProvenanceToolObserved: from a verified tool result.
	ProvenanceToolObserved Provenance = "tool_observed"
	// ProvenanceUnspecified: legacy rows, or a source not otherwise classified.
	// (Named to avoid colliding with the embedding-stack ProvenanceUnknown of
	// provenance.go, which is a different concept — the embedding fingerprint.)
	ProvenanceUnspecified Provenance = "unspecified"
)

// BaseConfidence is the confidence a provenance earns before any model-calibration
// scaling. A fact grounded in the user's own words outranks one the model inferred;
// a verified tool result outranks both. Unknown/legacy is left at 1.0 so existing
// rows are not retroactively distrusted.
func BaseConfidence(p Provenance) float64 {
	switch p {
	case ProvenanceToolObserved:
		return 0.95
	case ProvenanceUserStated:
		return 0.9
	case ProvenanceModelInferred:
		return 0.5
	default:
		return 1.0
	}
}

// sqliteTimeLayout is how SQLite's datetime('now') formats timestamps (UTC).
const sqliteTimeLayout = "2006-01-02 15:04:05"

// roleAssistant is the stored role value for assistant turns (matches
// llm.RoleAssistant). Recall excludes these: the assistant's own replies —
// especially clarifying questions like "what is your favourite language?" — are
// the strongest semantic match to a re-asked question, so retrieving them
// crowds out the user's actual answer and lets a stuck exchange reinforce
// itself. Only user turns and document chunks are semantically recalled.
const roleAssistant = "assistant"

// recallCandidateFactor over-fetches KNN neighbours before role filtering, so
// dropping assistant turns does not starve the k user-relevant results. The
// scan is brute-force over every stored vector regardless of the limit, so a
// larger candidate set costs only a few extra rows scanned in Go.
const recallCandidateFactor = 6

// Memory is one persisted long-term memory row.
type Memory struct {
	ID         int64
	Kind       Kind
	Role       string // "user"/"assistant" for turns; "" for doc chunks.
	Content    string
	Provenance Provenance // where it came from (Layer 16)
	// Subject is what the memory is ABOUT (Layer 23) — the other half of its
	// credential. Provenance and Subject together form its Attribution, which is
	// what the trust model reads; see subject.go. Turns and pre-Layer-23 rows are
	// SubjectUnspecified.
	Subject    Subject
	Confidence float64 // [0,1], system-assigned from provenance (× model calibration)
	// Retention bookkeeping (Layer 17). Salience is the stored value as of
	// LastAccessed (or CreatedAt if never recalled); the effective salience at read
	// time decays from there (see effectiveSalience). AccessCount is how many times
	// it has been reinforced.
	Salience     float64
	LastAccessed time.Time // zero if never recalled
	AccessCount  int64
	CreatedAt    time.Time
	// SupersededBy is the id of the fact that retired this one (0 = active). A
	// superseded fact is excluded from recall but kept for audit (Layer 21).
	SupersededBy int64
}

// Hit is a memory returned by a similarity search, with its distance to the query
// and the combined recall score it was ranked by. Distance is cosine distance
// (smaller = more similar); Score folds similarity, confidence, and effective
// (decayed) salience together (larger = ranked higher).
type Hit struct {
	Memory
	// Distance is the cosine distance from the query embedding, or
	// noVectorDistance when the vector arm did not retrieve this memory (LAYER
	// 22) — ask HasVector rather than testing the number.
	Distance float64
	// Score ranks the hit within one recall: relevance × confidence × effective
	// salience. What "relevance" means depends on which arms ran — see fuse.
	Score float64

	// LAYER 22 — which arm(s) found this memory, and where in each. Zero means
	// "not retrieved by that arm"; both are 1-based. They are the honest way to
	// read a hybrid result: a memory at rank 1 by meaning and rank 40 by wording
	// is a different kind of hit from one both arms put first.
	VectorRank  int
	LexicalRank int
	// BM25 is the lexical arm's raw score (negative; more negative = better).
	BM25 float64

	// effSalience is the decayed salience computed during this recall. Unexported:
	// it is an intermediate of ranking, not part of the memory.
	effSalience float64
}

// decayReference is the instant a memory's salience decays from: when it was
// last recalled, or when it was created if it never has been.
func (h Hit) decayReference() time.Time {
	if h.LastAccessed.IsZero() {
		return h.CreatedAt
	}
	return h.LastAccessed
}

// EffectiveSalience returns the decayed salience this hit was ranked with.
func (h Hit) EffectiveSalience() float64 { return h.effSalience }

// Remember stores a conversation turn (or doc chunk), deriving its provenance and
// base confidence from the role (a user turn is user-stated; an assistant turn is
// model-inferred). For a distilled fact, use RememberFact, which takes an explicit
// provenance and a (calibration-scaled) confidence.
func (s *Store) Remember(ctx context.Context, kind Kind, role, content string) (*Memory, error) {
	prov := provenanceForTurn(kind, role)
	// A turn is episodic text, not a claim about a subject: SubjectUnspecified.
	return s.remember(ctx, kind, role, content, Attr(prov, SubjectUnspecified), BaseConfidence(prov))
}

// RememberFact stores a durable fact (KindFact) with an explicit ATTRIBUTION — who
// stated it and what it is about — and a confidence. The caller assigns confidence
// (typically BaseConfidence(prov) scaled by the model's calibration) so a fact learned
// from an unreliable model does not silently gain the authority of an established one.
//
// Taking an Attribution rather than a bare Provenance is deliberate (Layer 23): every
// call site must now name the subject, because that is the field the trust model needs
// and the one only the caller — which knows the SOURCE — is entitled to decide.
func (s *Store) RememberFact(ctx context.Context, content string, attr Attribution, confidence float64) (*Memory, error) {
	return s.remember(ctx, KindFact, "", content, attr, confidence)
}

// provenanceForTurn maps a stored turn to its provenance. A fact stored via the
// generic Remember (rather than RememberFact) is Unspecified.
func provenanceForTurn(kind Kind, role string) Provenance {
	if kind == KindTurn {
		if role == roleAssistant {
			return ProvenanceModelInferred
		}
		return ProvenanceUserStated
	}
	return ProvenanceUnspecified
}

// remember embeds content and inserts one memory row with its provenance and
// confidence, returning the persisted row.
func (s *Store) remember(ctx context.Context, kind Kind, role, content string, attr Attribution, confidence float64) (*Memory, error) {
	prov, subject := attr.Provenance, attr.Subject.normalize()
	emb, err := s.Embed(ctx, content)
	if err != nil {
		return nil, err
	}
	var (
		id        int64
		createdAt string
	)
	// RETURNING gives us the generated id and timestamp in a single round trip.
	err = s.db.QueryRowContext(ctx,
		`INSERT INTO memories(kind, role, content, embedding, provenance, subject, confidence)
		 VALUES(?, ?, ?, ?, ?, ?, ?)
		 RETURNING id, created_at`,
		string(kind), role, content, emb, string(prov), string(subject), confidence).Scan(&id, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("insert memory: %w", err)
	}
	ts, err := time.Parse(sqliteTimeLayout, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at %q: %w", createdAt, err)
	}
	// A fresh row starts fully salient and unaccessed (the column defaults).
	return &Memory{ID: id, Kind: kind, Role: role, Content: content, Provenance: prov,
		Subject: subject, Confidence: confidence, Salience: 1.0, CreatedAt: ts}, nil
}

// Recall returns up to k long-term memories most relevant to query. Relevance is
// gated by semantic similarity first (assistant turns are excluded — see
// roleAssistant; if maxDistance > 0, memories farther than that cosine distance are
// dropped), then, among the relevant neighbourhood, memories are RANKED by a
// combined score = similarity × confidence × effective salience, so a trusted,
// reinforced memory outranks a barely-relevant or long-faded one at a similar
// distance. A memory whose salience has decayed below the store's ForgetFloor is
// soft-forgotten (dropped here; the row survives and a restatement revives it).
// Recall performs NO writes — decay is computed on the fly (see effectiveSalience),
// which keeps it a pure read on the pinned single connection. This is the
// semantic-retrieval step injected before each LLM call.
func (s *Store) Recall(ctx context.Context, query string, k int, maxDistance float64) ([]Hit, error) {
	return s.recall(ctx, query, k, maxDistance, recallOptions{lexical: true})
}

// RecallForConsolidation is Recall for the reflection/consolidation path: it is
// identical EXCEPT that it also returns soft-forgotten memories (those whose
// effective salience has decayed below the forget floor). The turn's prompt path
// must NOT see faded memories — that is the point of soft forgetting — but the
// consolidation path must, otherwise a restatement of a long-neglected fact can
// never find the old row and revives nothing: it would insert a near-duplicate
// instead, contradicting the documented promise that "a restatement revives it".
// Callers should reinforce whatever hit they get back (which resets the decay
// clock and lifts the fact back above the floor).
// It is also deliberately VECTOR-ONLY (LAYER 22). Hybrid recall answers "what
// might help me answer this?", where a lexical match on an unusual word is a
// welcome extra candidate. Consolidation asks a different question — "is this
// the SAME fact as one I already hold?" — and that is a question about semantic
// distance, which BM25 cannot answer: two sentences sharing the words "capital"
// and "France" may state opposite things, and the caller's maxDistance is a
// cosine radius that a lexical hit has no coordinate in. Letting word overlap
// nominate consolidation candidates made reflection consolidate onto unrelated
// facts instead of storing new ones. Retrieval is hybrid; IDENTITY stays metric.
func (s *Store) RecallForConsolidation(ctx context.Context, query string, k int, maxDistance float64) ([]Hit, error) {
	return s.recall(ctx, query, k, maxDistance, recallOptions{includeForgotten: true})
}

// recall is the shared implementation of Recall and RecallForConsolidation.
// includeForgotten decides whether memories below the forget floor are kept
// (consolidation) or dropped (the prompt path — the default via Recall).
// LAYER 22: recall now has two arms. The vector arm below is unchanged; the
// lexical arm (lexical.go) runs beside it and the two are fused (hybrid.go).
// With no lexical arm — a build without FTS5, or TALUNOR_RECALL=vector — this is
// exactly the Layer 17 path, including its ranking.
func (s *Store) recall(ctx context.Context, query string, k int, maxDistance float64, opt recallOptions) ([]Hit, error) {
	vector, err := s.vectorCandidates(ctx, query, k, maxDistance, opt.includeForgotten)
	if err != nil {
		return nil, err
	}
	var lexical []Hit
	if opt.lexical {
		lexical, err = s.lexicalCandidates(ctx, query, k*lexicalCandidateFactor)
		if err != nil {
			return nil, err
		}
		lexical = s.keepRecallable(lexical, opt.includeForgotten)
	}
	return fuse(vector, lexical, k), nil
}

// recallOptions are the two axes on which the callers of recall differ.
type recallOptions struct {
	// includeForgotten keeps memories whose salience decayed below the forget
	// floor (the consolidation path — see RecallForConsolidation).
	includeForgotten bool
	// lexical runs the BM25 arm beside the vector one. On for the prompt path,
	// off for anything asking whether two memories are the SAME.
	lexical bool
}

// keepRecallable applies the filters that cannot be expressed in the arm's SQL:
// assistant turns pollute recall, and a memory whose salience has decayed below
// the forget floor is soft-forgotten (hidden from the prompt path, still visible
// to consolidation). It also stamps each hit's effective salience, which the
// ranking then reuses.
//
// The vector arm inlines these checks because it must also honour the distance
// threshold as it streams; the lexical arm has no such threshold, so it filters
// here. Both end up with the same guarantees — that is the point of one shared
// helper: a memory the user soft-forgot must not come back through a side door
// just because it happens to contain the word they typed.
func (s *Store) keepRecallable(hits []Hit, includeForgotten bool) []Hit {
	now := time.Now().UTC()
	halfLife := s.resolvedHalfLife()
	forgetFloor := s.resolvedForgetFloor()

	kept := hits[:0]
	for _, h := range hits {
		if h.Role == roleAssistant {
			continue
		}
		h.effSalience = effectiveSalience(h.Salience, h.decayReference(), now, halfLife)
		if h.effSalience < forgetFloor && !includeForgotten {
			continue
		}
		kept = append(kept, h)
	}
	return kept
}

// vectorCandidates is the KNN arm: the nearest neighbours of the query
// embedding, gated by maxDistance.
func (s *Store) vectorCandidates(ctx context.Context, query string, k int, maxDistance float64, includeForgotten bool) ([]Hit, error) {
	qvec, err := s.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	// Over-fetch neighbours: assistant turns and faded memories are filtered out
	// below, and the survivors are re-ranked by score, so the raw KNN limit must
	// exceed k to still yield k good hits.
	// see : https://docs.sqlitecloud.io/docs/sqlite-vector-api-reference
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.kind, COALESCE(m.role, ''), m.content,
		       COALESCE(m.provenance, 'unspecified'), COALESCE(m.subject, 'unspecified'),
		       COALESCE(m.confidence, 1.0),
		       COALESCE(m.salience, 1.0), m.last_accessed, COALESCE(m.access_count, 0),
		       m.created_at, v.distance
		FROM vector_full_scan('memories', 'embedding', ?, ?) AS v
		JOIN memories m ON m.id = v.rowid
		WHERE m.superseded_by IS NULL
		ORDER BY v.distance`, qvec, k*recallCandidateFactor)
	if err != nil {
		return nil, fmt.Errorf("recall scan: %w", err)
	}
	defer rows.Close()

	candidates := make([]Hit, 0, k*recallCandidateFactor)
	for rows.Next() {
		h, err := scanHit(rows, scanVector)
		if err != nil {
			return nil, err
		}
		// Rows are ordered nearest-first, so the first over-threshold hit means
		// every remaining hit is too — stop the relevance gate here.
		if maxDistance > 0 && h.Distance > maxDistance {
			break
		}
		candidates = append(candidates, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Assistant turns, decay and soft forgetting are applied identically to both
	// arms (see keepRecallable); ranking happens in fuse.
	return s.keepRecallable(candidates, includeForgotten), nil
}

// Forget deletes the memory with the given id. It reports whether a row was
// actually removed (false means no such id existed), so callers can tell the
// user. The embedding lives in the same row, and vector_full_scan reads that
// column live, so a plain DELETE also removes it from KNN results — there is no
// separate index to maintain.
func (s *Store) Forget(ctx context.Context, id int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("forget memory %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("forget memory %d: %w", id, err)
	}
	return n > 0, nil
}

// Count returns the number of stored long-term memories.
func (s *Store) Count(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM memories`).Scan(&n)
	return n, err
}

// List returns the most recent memories, newest first (limit clamped to a
// sensible default when non-positive). It reads only text columns, so it works
// as a plain inspection of what is stored.
func (s *Store) List(ctx context.Context, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, COALESCE(role, ''), content,
		       COALESCE(provenance, 'unspecified'), COALESCE(subject, 'unspecified'),
		       COALESCE(confidence, 1.0),
		       COALESCE(salience, 1.0), last_accessed, COALESCE(access_count, 0), created_at,
		       COALESCE(superseded_by, 0)
		FROM memories
		ORDER BY id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list memories: %w", err)
	}
	defer rows.Close()

	var out []Memory
	for rows.Next() {
		var (
			m            Memory
			kind         string
			prov         string
			subject      string
			lastAccessed sql.NullString
			createdAt    string
		)
		if err := rows.Scan(&m.ID, &kind, &m.Role, &m.Content, &prov, &subject, &m.Confidence,
			&m.Salience, &lastAccessed, &m.AccessCount, &createdAt, &m.SupersededBy); err != nil {
			return nil, err
		}
		m.Kind = Kind(kind)
		m.Provenance = Provenance(prov)
		m.Subject = Subject(subject).normalize()
		if ts, err := time.Parse(sqliteTimeLayout, createdAt); err == nil {
			m.CreatedAt = ts
		}
		if lastAccessed.Valid {
			if ts, err := time.Parse(sqliteTimeLayout, lastAccessed.String); err == nil {
				m.LastAccessed = ts
			}
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// VersionAI returns the sqlite-ai extension version string, e.g. "0.1.0".
func (s *Store) VersionAI(ctx context.Context) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT ai_version()`).Scan(&v)
	return v, err
}

// VersionVector returns the sqlite-vector extension version string, e.g. "0.1.0".
func (s *Store) VersionVector(ctx context.Context) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT vector_version()`).Scan(&v)
	return v, err
}

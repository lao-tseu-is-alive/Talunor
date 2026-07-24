package memory

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
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
	Confidence float64    // [0,1], system-assigned from provenance (× model calibration)
	// Retention bookkeeping (Layer 17). Salience is the stored value as of
	// LastAccessed (or CreatedAt if never recalled); the effective salience at read
	// time decays from there (see effectiveSalience). AccessCount is how many times
	// it has been reinforced.
	Salience     float64
	LastAccessed time.Time // zero if never recalled
	AccessCount  int64
	CreatedAt    time.Time
}

// Hit is a memory returned by a similarity search, with its distance to the query
// and the combined recall score it was ranked by. Distance is cosine distance
// (smaller = more similar); Score folds similarity, confidence, and effective
// (decayed) salience together (larger = ranked higher).
type Hit struct {
	Memory
	Distance float64
	Score    float64
}

// Remember stores a conversation turn (or doc chunk), deriving its provenance and
// base confidence from the role (a user turn is user-stated; an assistant turn is
// model-inferred). For a distilled fact, use RememberFact, which takes an explicit
// provenance and a (calibration-scaled) confidence.
func (s *Store) Remember(ctx context.Context, kind Kind, role, content string) (*Memory, error) {
	prov := provenanceForTurn(kind, role)
	return s.remember(ctx, kind, role, content, prov, BaseConfidence(prov))
}

// RememberFact stores a durable fact (KindFact) with an explicit provenance and
// confidence. The caller assigns confidence — typically BaseConfidence(prov) scaled
// by the model's calibration — so a fact learned from an unreliable model does not
// silently gain the authority of an established one.
func (s *Store) RememberFact(ctx context.Context, content string, prov Provenance, confidence float64) (*Memory, error) {
	return s.remember(ctx, KindFact, "", content, prov, confidence)
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
func (s *Store) remember(ctx context.Context, kind Kind, role, content string, prov Provenance, confidence float64) (*Memory, error) {
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
		`INSERT INTO memories(kind, role, content, embedding, provenance, confidence)
		 VALUES(?, ?, ?, ?, ?, ?)
		 RETURNING id, created_at`,
		string(kind), role, content, emb, string(prov), confidence).Scan(&id, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("insert memory: %w", err)
	}
	ts, err := time.Parse(sqliteTimeLayout, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at %q: %w", createdAt, err)
	}
	// A fresh row starts fully salient and unaccessed (the column defaults).
	return &Memory{ID: id, Kind: kind, Role: role, Content: content, Provenance: prov, Confidence: confidence, Salience: 1.0, CreatedAt: ts}, nil
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
		       COALESCE(m.provenance, 'unspecified'), COALESCE(m.confidence, 1.0),
		       COALESCE(m.salience, 1.0), m.last_accessed, COALESCE(m.access_count, 0),
		       m.created_at, v.distance
		FROM vector_full_scan('memories', 'embedding', ?, ?) AS v
		JOIN memories m ON m.id = v.rowid
		ORDER BY v.distance`, qvec, k*recallCandidateFactor)
	if err != nil {
		return nil, fmt.Errorf("recall scan: %w", err)
	}
	defer rows.Close()

	now := time.Now().UTC()
	halfLife := s.resolvedHalfLife()
	forgetFloor := s.resolvedForgetFloor()

	candidates := make([]Hit, 0, k*recallCandidateFactor)
	for rows.Next() {
		var (
			h            Hit
			kind         string
			prov         string
			lastAccessed sql.NullString
			createdAt    string
		)
		if err := rows.Scan(&h.ID, &kind, &h.Role, &h.Content, &prov, &h.Confidence,
			&h.Salience, &lastAccessed, &h.AccessCount, &createdAt, &h.Distance); err != nil {
			return nil, err
		}
		// Rows are ordered nearest-first, so the first over-threshold hit means
		// every remaining hit is too — stop the relevance gate here.
		if maxDistance > 0 && h.Distance > maxDistance {
			break
		}
		// Skip assistant turns: they pollute recall (see roleAssistant).
		if h.Role == roleAssistant {
			continue
		}
		h.Kind = Kind(kind)
		h.Provenance = Provenance(prov)
		if ts, err := time.Parse(sqliteTimeLayout, createdAt); err == nil {
			h.CreatedAt = ts
		}
		if lastAccessed.Valid {
			if ts, err := time.Parse(sqliteTimeLayout, lastAccessed.String); err == nil {
				h.LastAccessed = ts
			}
		}
		// Decay salience lazily from when the memory was last touched, then drop it
		// if it has faded below the forget floor (soft forgetting).
		ref := h.LastAccessed
		if ref.IsZero() {
			ref = h.CreatedAt
		}
		eff := effectiveSalience(h.Salience, ref, now, halfLife)
		if eff < forgetFloor {
			continue
		}
		// Combined recall score: relevance × trust × how-much-it-matters-now.
		h.Score = (1 - h.Distance) * h.Confidence * eff
		candidates = append(candidates, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Rank by combined score (relevance was the gate; salience/confidence break
	// ties within the relevant neighbourhood), then keep the top k.
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})
	if len(candidates) > k {
		candidates = candidates[:k]
	}
	return candidates, nil
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
		       COALESCE(provenance, 'unspecified'), COALESCE(confidence, 1.0),
		       COALESCE(salience, 1.0), last_accessed, COALESCE(access_count, 0), created_at
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
			lastAccessed sql.NullString
			createdAt    string
		)
		if err := rows.Scan(&m.ID, &kind, &m.Role, &m.Content, &prov, &m.Confidence,
			&m.Salience, &lastAccessed, &m.AccessCount, &createdAt); err != nil {
			return nil, err
		}
		m.Kind = Kind(kind)
		m.Provenance = Provenance(prov)
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

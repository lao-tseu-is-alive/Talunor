package memory

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Layer 20 — the evidence trail. A fact carries a provenance and a confidence
// (Layer 16), but not *where* it came from. The evidence table records, for each
// fact, every turn+source that supported it: the first store and each later
// reinforcement. That turns "the agent believes X (90%)" into "the agent believes
// X because the user said so in turns #3 and #9" — auditable, and the raw material
// a later supersession step (Layer 21) will arbitrate on.
//
// It is deliberately append-only and decoupled from the memories table: recording
// evidence is best-effort bookkeeping (a failure must never break a turn or lose a
// stored fact), and a fact with no evidence rows (e.g. a legacy fact from before
// this layer) simply has an empty trail.

// Polarity says whether an evidence row backs a fact or challenges it (Layer 24).
// Before that layer the trail could only record support, so every pre-existing row
// is PolaritySupports — which is what it always meant.
type Polarity string

const (
	// PolaritySupports is the default: this turn/source backed the fact.
	PolaritySupports Polarity = "supports"
	// PolarityContradicts records a claim that CONTRADICTED the fact and was refused
	// by the trust model (see agent.learnOneFact and ADR 0005). The refused claim is
	// deliberately not stored as a memory of its own — it would be recallable, which
	// is the authority the gate just denied it — so its text lives in Detail.
	PolarityContradicts Polarity = "contradicts"
)

// Evidence is one row of a fact's trail: a turn, the source (provenance) that turn
// contributed it from, and whether it supported or challenged the fact.
type Evidence struct {
	ID       int64
	FactID   int64
	TurnID   int64 // 0 when the supporting turn is unknown (stored as NULL)
	Source   Provenance
	Polarity Polarity
	// Detail is the text of a refused, contradicting claim (empty for support rows).
	// It is what lets /why show both sides of a disagreement.
	Detail    string
	CreatedAt time.Time
}

// RecordEvidence appends one support row for a fact: it was supported by turnID
// (0 → unknown/NULL) from source. Call it on both the first store of a fact and
// every reinforcement, so the trail reflects how belief accumulated. It is
// best-effort — the caller treats a failure as non-fatal.
func (s *Store) RecordEvidence(ctx context.Context, factID, turnID int64, source Provenance) error {
	return s.recordEvidence(ctx, factID, turnID, source, PolaritySupports, "")
}

// RecordCounterEvidence appends one CHALLENGE row: a claim from turnID (0 → unknown)
// and source contradicted this fact, and the trust model refused to let it retire the
// fact (memory.Supersedes said no). claim is the refused text, kept so the
// disagreement can be read later.
//
// Recording it is the whole of Layer 24: the refusal is correct, but discarding what
// was refused left the memory unable to represent a disputed belief. One or more of
// these rows is what makes a fact report Contested — the flag is derived from them,
// never stored separately (ADR 0005). Best-effort, like all evidence.
func (s *Store) RecordCounterEvidence(ctx context.Context, factID, turnID int64, source Provenance, claim string) error {
	return s.recordEvidence(ctx, factID, turnID, source, PolarityContradicts, claim)
}

func (s *Store) recordEvidence(ctx context.Context, factID, turnID int64, source Provenance, pol Polarity, detail string) error {
	var turn any // NULL when unknown, so the trail doesn't invent a turn id.
	if turnID > 0 {
		turn = turnID
	}
	var det any // NULL rather than "" on support rows.
	if detail != "" {
		det = detail
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO evidence(fact_id, turn_id, source, polarity, detail) VALUES(?, ?, ?, ?, ?)`,
		factID, turn, string(source), string(pol), det)
	if err != nil {
		return fmt.Errorf("record %s evidence for fact %d: %w", pol, factID, err)
	}
	return nil
}

// EvidenceFor returns a fact's support rows, oldest first (the order belief
// accumulated). An empty slice means no recorded evidence (e.g. a legacy fact).
func (s *Store) EvidenceFor(ctx context.Context, factID int64) ([]Evidence, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, fact_id, turn_id, source,
		       COALESCE(polarity, 'supports'), COALESCE(detail, ''), created_at
		FROM evidence
		WHERE fact_id = ?
		ORDER BY id`, factID)
	if err != nil {
		return nil, fmt.Errorf("evidence for fact %d: %w", factID, err)
	}
	defer rows.Close()

	var out []Evidence
	for rows.Next() {
		var (
			ev        Evidence
			turnID    sql.NullInt64
			source    string
			polarity  string
			createdAt string
		)
		if err := rows.Scan(&ev.ID, &ev.FactID, &turnID, &source, &polarity, &ev.Detail, &createdAt); err != nil {
			return nil, err
		}
		if turnID.Valid {
			ev.TurnID = turnID.Int64
		}
		ev.Source = Provenance(source)
		ev.Polarity = Polarity(polarity)
		if ts, err := time.Parse(sqliteTimeLayout, createdAt); err == nil {
			ev.CreatedAt = ts
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// MemoryByID returns the single memory with the given id (ok=false if none). It
// is used by the "why?" command to show a fact alongside its evidence trail.
func (s *Store) MemoryByID(ctx context.Context, id int64) (Memory, bool, error) {
	var (
		m            Memory
		kind         string
		prov         string
		subject      string
		lastAccessed sql.NullString
		createdAt    string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, kind, COALESCE(role, ''), content,
		       COALESCE(provenance, 'unspecified'), COALESCE(subject, 'unspecified'),
		       COALESCE(confidence, 1.0),
		       COALESCE(salience, 1.0), last_accessed, COALESCE(access_count, 0), created_at,
		       COALESCE(superseded_by, 0), `+contestedExpr("memories")+`
		FROM memories
		WHERE id = ?`, id).Scan(&m.ID, &kind, &m.Role, &m.Content, &prov, &subject, &m.Confidence,
		&m.Salience, &lastAccessed, &m.AccessCount, &createdAt, &m.SupersededBy, &m.Contested)
	if err == sql.ErrNoRows {
		return Memory{}, false, nil
	}
	if err != nil {
		return Memory{}, false, fmt.Errorf("get memory %d: %w", id, err)
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
	return m, true, nil
}

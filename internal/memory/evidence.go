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

// Evidence is one row of a fact's support: a turn, and the source (provenance)
// that turn contributed it from.
type Evidence struct {
	ID        int64
	FactID    int64
	TurnID    int64 // 0 when the supporting turn is unknown (stored as NULL)
	Source    Provenance
	CreatedAt time.Time
}

// RecordEvidence appends one support row for a fact: it was supported by turnID
// (0 → unknown/NULL) from source. Call it on both the first store of a fact and
// every reinforcement, so the trail reflects how belief accumulated. It is
// best-effort — the caller treats a failure as non-fatal.
func (s *Store) RecordEvidence(ctx context.Context, factID, turnID int64, source Provenance) error {
	var turn any // NULL when unknown, so the trail doesn't invent a turn id.
	if turnID > 0 {
		turn = turnID
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO evidence(fact_id, turn_id, source) VALUES(?, ?, ?)`,
		factID, turn, string(source))
	if err != nil {
		return fmt.Errorf("record evidence for fact %d: %w", factID, err)
	}
	return nil
}

// EvidenceFor returns a fact's support rows, oldest first (the order belief
// accumulated). An empty slice means no recorded evidence (e.g. a legacy fact).
func (s *Store) EvidenceFor(ctx context.Context, factID int64) ([]Evidence, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, fact_id, turn_id, source, created_at
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
			createdAt string
		)
		if err := rows.Scan(&ev.ID, &ev.FactID, &turnID, &source, &createdAt); err != nil {
			return nil, err
		}
		if turnID.Valid {
			ev.TurnID = turnID.Int64
		}
		ev.Source = Provenance(source)
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
		lastAccessed sql.NullString
		createdAt    string
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, kind, COALESCE(role, ''), content,
		       COALESCE(provenance, 'unspecified'), COALESCE(confidence, 1.0),
		       COALESCE(salience, 1.0), last_accessed, COALESCE(access_count, 0), created_at
		FROM memories
		WHERE id = ?`, id).Scan(&m.ID, &kind, &m.Role, &m.Content, &prov, &m.Confidence,
		&m.Salience, &lastAccessed, &m.AccessCount, &createdAt)
	if err == sql.ErrNoRows {
		return Memory{}, false, nil
	}
	if err != nil {
		return Memory{}, false, fmt.Errorf("get memory %d: %w", id, err)
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
	return m, true, nil
}

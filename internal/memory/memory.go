package memory

import (
	"context"
	"fmt"
	"time"
)

// Kind classifies a stored memory.
type Kind string

const (
	// KindTurn is a single conversation message (has a role).
	KindTurn Kind = "turn"
	// KindDocChunk is a chunk of an ingested document (no role).
	KindDocChunk Kind = "doc_chunk"
)

// sqliteTimeLayout is how SQLite's datetime('now') formats timestamps (UTC).
const sqliteTimeLayout = "2006-01-02 15:04:05"

// Memory is one persisted long-term memory row.
type Memory struct {
	ID        int64
	Kind      Kind
	Role      string // "user"/"assistant" for turns; "" for doc chunks.
	Content   string
	CreatedAt time.Time
}

// Hit is a memory returned by a similarity search, with its distance to the
// query. Distance is cosine distance: smaller means more similar.
type Hit struct {
	Memory
	Distance float64
}

// Remember embeds content in-DB and stores it as a long-term memory, returning
// the persisted row (with its id and timestamp).
func (s *Store) Remember(ctx context.Context, kind Kind, role, content string) (*Memory, error) {
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
		`INSERT INTO memories(kind, role, content, embedding)
		 VALUES(?, ?, ?, ?)
		 RETURNING id, created_at`,
		string(kind), role, content, emb).Scan(&id, &createdAt)
	if err != nil {
		return nil, fmt.Errorf("insert memory: %w", err)
	}
	ts, err := time.Parse(sqliteTimeLayout, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at %q: %w", createdAt, err)
	}
	return &Memory{ID: id, Kind: kind, Role: role, Content: content, CreatedAt: ts}, nil
}

// Recall returns up to k long-term memories most similar to query, nearest
// first. If maxDistance > 0, memories farther than that cosine distance are
// dropped, so only genuinely relevant ones are returned (a value of 0 keeps all
// k). This is the semantic-retrieval step injected before each LLM call.
func (s *Store) Recall(ctx context.Context, query string, k int, maxDistance float64) ([]Hit, error) {
	qvec, err := s.Embed(ctx, query)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.kind, COALESCE(m.role, ''), m.content, m.created_at, v.distance
		FROM vector_full_scan('memories', 'embedding', ?, ?) AS v
		JOIN memories m ON m.id = v.rowid
		ORDER BY v.distance`, qvec, k)
	if err != nil {
		return nil, fmt.Errorf("recall scan: %w", err)
	}
	defer rows.Close()

	var hits []Hit
	for rows.Next() {
		var (
			h         Hit
			kind      string
			createdAt string
		)
		if err := rows.Scan(&h.ID, &kind, &h.Role, &h.Content, &createdAt, &h.Distance); err != nil {
			return nil, err
		}
		// Rows are ordered nearest-first, so the first over-threshold hit means
		// every remaining hit is too — stop here.
		if maxDistance > 0 && h.Distance > maxDistance {
			break
		}
		h.Kind = Kind(kind)
		if ts, err := time.Parse(sqliteTimeLayout, createdAt); err == nil {
			h.CreatedAt = ts
		}
		hits = append(hits, h)
	}
	return hits, rows.Err()
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
		SELECT id, kind, COALESCE(role, ''), content, created_at
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
			m         Memory
			kind      string
			createdAt string
		)
		if err := rows.Scan(&m.ID, &kind, &m.Role, &m.Content, &createdAt); err != nil {
			return nil, err
		}
		m.Kind = Kind(kind)
		if ts, err := time.Parse(sqliteTimeLayout, createdAt); err == nil {
			m.CreatedAt = ts
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

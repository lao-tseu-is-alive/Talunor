package memory

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"
)

// An embedding is only comparable with vectors produced by the *same* embedding
// stack — the same GGUF model, the same embedding config, the same extension
// build. Swap any of those and the stored vectors quietly land in a different
// space: KNN still runs, but distances become meaningless and recall of older
// memories silently degrades. This file records a fingerprint of the embedding
// stack in the database and verifies it on every Open, so a model change is
// detected instead of silently corrupting recall.

// embedCanaryText is a fixed sentinel that is embedded to fingerprint the
// embedding stack. Its stored vector is compared, on each Open, against a freshly
// computed one: any drift means the stack that wrote the existing memories
// differs from the one running now. Never change this string — doing so would
// make every existing store report a false mismatch.
const embedCanaryText = "Talunor embedding-provenance canary — do not change this string."

// canaryMatchEpsilon is the largest cosine distance between the stored and a
// fresh canary vector still treated as "same embedding space". Embedding is
// deterministic (identical text → identical vector), so a genuine match is ~0;
// this small epsilon only tolerates last-bit floating-point noise.
const canaryMatchEpsilon = 1e-4

// Meta keys holding the embedding-stack fingerprint.
const (
	metaEmbedModel  = "embed_model"  // model file basename (human-readable).
	metaEmbedDim    = "embed_dim"    // embedding dimension (human-readable).
	metaEmbedCanary = "embed_canary" // the canary vector BLOB (the actual guard).
)

// ProvenanceStatus reports whether the stored embeddings were written by the
// embedding stack currently loaded.
type ProvenanceStatus int

const (
	// ProvenanceOK: the store is fresh, or its canary matches the current
	// embedding stack — recall is trustworthy.
	ProvenanceOK ProvenanceStatus = iota
	// ProvenanceUnknown: the store predates provenance tracking and already holds
	// memories, so their embedding stack cannot be verified. A re-embed removes
	// the doubt.
	ProvenanceUnknown
	// ProvenanceStale: the canary no longer matches — the embedding model or its
	// config changed since these memories were written, so their vectors sit in a
	// different space and recall across the change is degraded. Re-embed to fix.
	ProvenanceStale
)

func (p ProvenanceStatus) String() string {
	switch p {
	case ProvenanceOK:
		return "ok"
	case ProvenanceUnknown:
		return "unknown (memories predate provenance tracking — run a re-embed to verify)"
	case ProvenanceStale:
		return "stale (embedding model changed — run a re-embed to realign old memories)"
	default:
		return "invalid"
	}
}

// metaSchemaSQL is the key/value side-table holding the embedding fingerprint.
const metaSchemaSQL = `
CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value BLOB NOT NULL
);`

// Provenance returns the embedding-provenance status derived at Open. It is a
// cheap accessor; the actual check runs once during bootstrap.
func (s *Store) Provenance() ProvenanceStatus { return s.provenance }

// EmbedModelName is the basename of the loaded embedding model, for display.
func (s *Store) EmbedModelName() string { return filepath.Base(s.cfg.EmbedModelPath) }

// initProvenance records or verifies the embedding-stack fingerprint. On a fresh
// store it stamps the current fingerprint (status OK). On an existing store it
// compares the stored canary with a freshly computed one and sets the status.
// It runs at the end of bootstrap, once the model and embedding context are live.
func (s *Store) initProvenance(ctx context.Context) error {
	current, err := s.Embed(ctx, embedCanaryText)
	if err != nil {
		return fmt.Errorf("provenance canary embed: %w", err)
	}
	stored, ok, err := s.metaGet(ctx, metaEmbedCanary)
	if err != nil {
		return err
	}
	if ok {
		if canaryMatches(stored, current) {
			s.provenance = ProvenanceOK
		} else {
			s.provenance = ProvenanceStale
		}
		return nil
	}
	// No canary recorded yet. If the store already holds memories, they predate
	// provenance tracking and cannot be verified; leave the canary unset so a
	// re-embed establishes it. A fresh store is stamped now.
	n, err := s.Count(ctx)
	if err != nil {
		return err
	}
	if n > 0 {
		s.provenance = ProvenanceUnknown
		return nil
	}
	if err := s.stampProvenance(ctx, current); err != nil {
		return err
	}
	s.provenance = ProvenanceOK
	return nil
}

// execer is the write subset shared by *sql.DB and *sql.Tx, so the meta upserts
// can run either on the pool or inside a transaction (see ReEmbed).
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// stampProvenance records the fingerprint of the embedding stack that produced
// canary (the current one) on the pool.
func (s *Store) stampProvenance(ctx context.Context, canary []byte) error {
	return stampProvenanceOn(ctx, s.db, s, canary)
}

// stampProvenanceOn writes the fingerprint via any execer (pool or transaction).
func stampProvenanceOn(ctx context.Context, e execer, s *Store, canary []byte) error {
	if err := metaSetOn(ctx, e, metaEmbedCanary, canary); err != nil {
		return err
	}
	if err := metaSetOn(ctx, e, metaEmbedModel, []byte(s.EmbedModelName())); err != nil {
		return err
	}
	return metaSetOn(ctx, e, metaEmbedDim, fmt.Appendf(nil, "%d", s.dim))
}

// ReEmbed recomputes and rewrites the embedding of every stored memory with the
// currently loaded model, then stamps the store with the current fingerprint.
// Use it after the embedding model changes (Provenance() is ProvenanceStale or
// ProvenanceUnknown) to bring old vectors back into the active space so recall
// works again. progress, if non-nil, is called after each row with (done, total).
// It returns the number of memories re-embedded.
func (s *Store) ReEmbed(ctx context.Context, progress func(done, total int)) (int, error) {
	// The pool is pinned to a single connection (per-connection model state), so a
	// live rows cursor would block the Embed queries below. Read every row into
	// memory first, close the cursor, then embed and update.
	rows, err := s.db.QueryContext(ctx, `SELECT id, content FROM memories ORDER BY id`)
	if err != nil {
		return 0, err
	}
	type item struct {
		id      int64
		content string
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.content); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	total := len(items)

	// Phase 1 — compute every embedding first, WITHOUT touching the DB. The pool is
	// pinned to one connection, so we cannot embed (which queries) while a
	// transaction holds that connection; computing up front also means a mid-way
	// embedding failure leaves the store completely untouched.
	embs := make([][]byte, total)
	for done, it := range items {
		emb, err := s.Embed(ctx, it.content)
		if err != nil {
			return 0, fmt.Errorf("re-embed #%d: %w", it.id, err)
		}
		embs[done] = emb
		if progress != nil {
			progress(done+1, total)
		}
	}
	canary, err := s.Embed(ctx, embedCanaryText)
	if err != nil {
		return 0, err
	}

	// Phase 2 — apply every update and the fingerprint stamp in ONE transaction, so
	// a failure can never leave the store with a mix of old- and new-space vectors
	// (which would corrupt recall silently). All-or-nothing.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() // no-op once Commit succeeds.
	for i, it := range items {
		if _, err := tx.ExecContext(ctx,
			`UPDATE memories SET embedding = ? WHERE id = ?`, embs[i], it.id); err != nil {
			return 0, fmt.Errorf("update #%d: %w", it.id, err)
		}
	}
	if err := stampProvenanceOn(ctx, tx, s, canary); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("re-embed commit: %w", err)
	}
	s.provenance = ProvenanceOK
	return total, nil
}

// metaGet reads a meta value; ok is false when the key is absent.
func (s *Store) metaGet(ctx context.Context, key string) (value []byte, ok bool, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return value, true, nil
}

// metaSet upserts a meta value on the pool.
func (s *Store) metaSet(ctx context.Context, key string, value []byte) error {
	return metaSetOn(ctx, s.db, key, value)
}

// metaSetOn upserts a meta value via any execer (pool or transaction).
func metaSetOn(ctx context.Context, e execer, key string, value []byte) error {
	_, err := e.ExecContext(ctx,
		`INSERT INTO meta(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// canaryMatches reports whether two canary vectors describe the same embedding
// space (cosine distance within canaryMatchEpsilon).
func canaryMatches(a, b []byte) bool {
	d, ok := cosineDistanceBlob(a, b)
	return ok && d <= canaryMatchEpsilon
}

// cosineDistanceBlob computes 1 − cosine similarity between two FLOAT32 BLOBs as
// produced by Embed. Vectors are L2-normalised at embed time, so the similarity
// is simply their dot product; ok is false if the blobs are malformed or of
// mismatched length.
func cosineDistanceBlob(a, b []byte) (dist float64, ok bool) {
	if len(a) == 0 || len(a) != len(b) || len(a)%4 != 0 {
		return 0, false
	}
	var dot float64
	for i := 0; i < len(a); i += 4 {
		fa := math.Float32frombits(binary.LittleEndian.Uint32(a[i:]))
		fb := math.Float32frombits(binary.LittleEndian.Uint32(b[i:]))
		dot += float64(fa) * float64(fb)
	}
	return 1 - dot, true
}

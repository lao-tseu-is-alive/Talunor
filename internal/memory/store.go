// Package memory is Talunor's persistence substrate. It wraps a single SQLite
// database with two loadable extensions:
//
//   - sqlite-ai     (ai.so)     — runs a GGUF embedding model in-process, so
//     embeddings are produced with plain SQL (no external service).
//   - sqlite-vector (vector.so) — stores embeddings as FLOAT32 BLOBs in an
//     ordinary column and provides brute-force KNN over them.
//
// Layer 1 only proves the substrate works end to end (load → embed → KNN).
// The higher-level short-term / long-term memory API is built on top of this
// in Layer 2.
package memory

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	sqlite3 "github.com/mattn/go-sqlite3"
)

// driverName is the custom database/sql driver we register so that every new
// connection loads the two extensions through a ConnectHook.
const driverName = "sqlite3_talunor"

var registerOnce sync.Once

// Config holds the filesystem paths Talunor's memory needs.
type Config struct {
	// DBPath is the SQLite database file (":memory:" for an ephemeral store).
	DBPath string
	// VectorExtPath is the path to the sqlite-vector shared object (vector.so).
	VectorExtPath string
	// AIExtPath is the path to the sqlite-ai shared object (ai.so).
	AIExtPath string
	// EmbedModelPath is the GGUF embedding model loaded by sqlite-ai.
	EmbedModelPath string
}

// DefaultConfig returns a Config pointing at the artifacts fetched by
// `make deps` and a local talunor.db database file.
func DefaultConfig() Config {
	return Config{
		DBPath: "talunor.db",
		// No ".so" suffix: SQLite's load_extension appends the platform suffix.
		VectorExtPath:  "ext/vector",
		AIExtPath:      "ext/ai",
		EmbedModelPath: "ext/models/all-MiniLM-L6-v2.f16.gguf",
	}
}

// Store owns the database handle and the resident embedding model.
//
// The sqlite-ai extension keeps the loaded model and embedding context in
// per-connection state; sqlite-vector's vector_init is likewise per-connection.
// To keep that state valid we pin the pool to a single connection. For a
// single-user agent this is a fine trade-off and sidesteps a whole class of
// concurrency issues. (Multi-connection support is a later concern.)
type Store struct {
	db  *sql.DB
	cfg Config
	dim int // embedding dimension, discovered from the model at open time.
}

// registerDriver registers the custom driver exactly once. The ConnectHook
// loads both extensions into every connection as it is opened.
func registerDriver(cfg Config) {
	registerOnce.Do(func() {
		sql.Register(driverName, &sqlite3.SQLiteDriver{
			ConnectHook: func(conn *sqlite3.SQLiteConn) error {
				// The entry point must be passed explicitly: mattn's
				// LoadExtension forwards an empty string as an empty (not NULL)
				// entry-point name, which makes SQLite dlsym("") and fail with
				// an empty "undefined symbol" error.
				if err := conn.LoadExtension(cfg.VectorExtPath, "sqlite3_vector_init"); err != nil {
					return fmt.Errorf("load sqlite-vector (%s): %w", cfg.VectorExtPath, err)
				}
				if err := conn.LoadExtension(cfg.AIExtPath, "sqlite3_ai_init"); err != nil {
					return fmt.Errorf("load sqlite-ai (%s): %w", cfg.AIExtPath, err)
				}
				return nil
			},
		})
	})
}

// Open opens (creating if needed) the database, loads the extensions and the
// embedding model, applies the schema, and initialises vector search.
func Open(cfg Config) (*Store, error) {
	registerDriver(cfg)

	db, err := sql.Open(driverName, cfg.DBPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1) // keep per-connection extension state valid.

	s := &Store{db: db, cfg: cfg}
	if err := s.bootstrap(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// schemaSQL is the Layer 1 schema: a single flat table of memories. Each row is
// a piece of remembered text with its embedding stored as a FLOAT32 BLOB.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS memories (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    kind       TEXT NOT NULL,                              -- 'turn' | 'doc_chunk'
    role       TEXT,                                       -- 'user' | 'assistant' (turns only)
    content    TEXT NOT NULL,
    embedding  BLOB,                                       -- float32[dim] from sqlite-ai
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);`

// bootstrap loads the model, discovers the embedding dimension, applies the
// schema, and initialises vector search on the embedding column.
func (s *Store) bootstrap(ctx context.Context) error {
	// Load the embedding model. gpu_layers=0 → pure CPU inference.
	if _, err := s.db.ExecContext(ctx,
		`SELECT llm_model_load(?, 'gpu_layers=0')`, s.cfg.EmbedModelPath); err != nil {
		return fmt.Errorf("llm_model_load: %w", err)
	}
	// Create an embedding context. embedding_type=FLOAT32 matches the vector
	// column type; normalize_embedding=1 + pooling_type=mean is what
	// all-MiniLM-L6-v2 expects.
	if _, err := s.db.ExecContext(ctx,
		`SELECT llm_context_create_embedding('embedding_type=FLOAT32,normalize_embedding=1,pooling_type=mean')`); err != nil {
		return fmt.Errorf("llm_context_create_embedding: %w", err)
	}
	// Discover the embedding dimension from the loaded model (expect 384).
	if err := s.db.QueryRowContext(ctx, `SELECT llm_model_n_embd()`).Scan(&s.dim); err != nil {
		return fmt.Errorf("llm_model_n_embd: %w", err)
	}
	// Apply schema.
	if _, err := s.db.ExecContext(ctx, schemaSQL); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	// Initialise vector search on memories.embedding (per connection).
	initSQL := fmt.Sprintf(
		`SELECT vector_init('memories', 'embedding', 'dimension=%d,type=FLOAT32,distance=cosine')`, s.dim)
	if _, err := s.db.ExecContext(ctx, initSQL); err != nil {
		return fmt.Errorf("vector_init: %w", err)
	}
	return nil
}

// Dim returns the embedding dimension reported by the loaded model.
func (s *Store) Dim() int { return s.dim }

// Embed returns the embedding of text as a FLOAT32 BLOB, computed in-process by
// sqlite-ai. The BLOB is directly storable in memories.embedding and directly
// usable as a query vector for KNN search.
func (s *Store) Embed(ctx context.Context, text string) ([]byte, error) {
	var blob []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT llm_embed_generate(?, 'json_output=0')`, text).Scan(&blob)
	if err != nil {
		return nil, fmt.Errorf("llm_embed_generate: %w", err)
	}
	return blob, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

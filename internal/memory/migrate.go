package memory

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// The store evolves its schema through an ordered, append-only list of migrations.
// The applied version is a single integer kept in the meta table (metaSchemaVersion),
// so a database created by an older build is upgraded in place on the next Open, and
// a database that predates versioning is *baselined* automatically: it starts at
// version 0, and migration 1 (which creates the memories table with IF NOT EXISTS)
// is a harmless no-op on it before the version is stamped.
//
// Rules for adding a migration (read before you touch this):
//   - Append only. NEVER reorder, renumber, or edit a shipped migration — users have
//     already run it. A mistake is fixed by a *new* migration, not by editing an old one.
//   - Prefer additive, idempotent DDL (ADD COLUMN, CREATE ... IF NOT EXISTS).
//   - Each migration runs in its own transaction together with its version stamp, so
//     it is all-or-nothing and a crash mid-run resumes cleanly.

// metaSchemaVersion is the meta key holding the applied schema version (an integer).
const metaSchemaVersion = "schema_version"

// migration is one ordered, versioned schema change.
type migration struct {
	version int
	name    string
	apply   func(ctx context.Context, e execer) error
}

// migrations is the append-only history. Version N is applied when the store's
// recorded version is < N.
var migrations = []migration{
	{
		version: 1,
		name:    "baseline: memories table",
		apply: func(ctx context.Context, e execer) error {
			_, err := e.ExecContext(ctx, schemaSQL)
			return err
		},
	},
	{
		version: 2,
		name:    "fact provenance + confidence",
		apply: func(ctx context.Context, e execer) error {
			// Existing rows default to 'unspecified' / 1.0 — don't retroactively
			// distrust what's already stored; new rows get proper values.
			if _, err := e.ExecContext(ctx,
				`ALTER TABLE memories ADD COLUMN provenance TEXT NOT NULL DEFAULT 'unspecified'`); err != nil {
				return err
			}
			_, err := e.ExecContext(ctx,
				`ALTER TABLE memories ADD COLUMN confidence REAL NOT NULL DEFAULT 1.0`)
			return err
		},
	},
	{
		version: 3,
		name:    "salience + decay bookkeeping",
		apply: func(ctx context.Context, e execer) error {
			// Retention bookkeeping (Layer 17): salience is how much a memory
			// "matters" (reinforced on recall, decayed lazily at read time);
			// last_accessed anchors the decay clock; access_count is how many times
			// it has been recalled. Existing rows start fully salient and unaccessed.
			for _, ddl := range []string{
				`ALTER TABLE memories ADD COLUMN salience REAL NOT NULL DEFAULT 1.0`,
				`ALTER TABLE memories ADD COLUMN last_accessed TEXT`,
				`ALTER TABLE memories ADD COLUMN access_count INTEGER NOT NULL DEFAULT 0`,
			} {
				if _, err := e.ExecContext(ctx, ddl); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 4,
		name:    "evidence trail",
		apply: func(ctx context.Context, e execer) error {
			// Layer 20 (Iteration 5): the auditable evidence chain. Each row records
			// that a fact was supported by a turn from a source (its provenance), on
			// first store and on every reinforcement — so "why do you believe this?"
			// can be answered. Append-only; nothing on the memories table changes.
			for _, ddl := range []string{
				`CREATE TABLE IF NOT EXISTS evidence (
				    id         INTEGER PRIMARY KEY AUTOINCREMENT,
				    fact_id    INTEGER NOT NULL,
				    turn_id    INTEGER,
				    source     TEXT NOT NULL,
				    created_at TEXT NOT NULL DEFAULT (datetime('now'))
				)`,
				`CREATE INDEX IF NOT EXISTS idx_evidence_fact ON evidence(fact_id)`,
			} {
				if _, err := e.ExecContext(ctx, ddl); err != nil {
					return err
				}
			}
			return nil
		},
	},
	{
		version: 5,
		name:    "supersession pointer",
		apply: func(ctx context.Context, e execer) error {
			// Layer 21: a fact can be RETIRED by a newer, higher-authority one. We
			// soft-supersede — mark, don't delete — so it stays auditable and
			// reversible: superseded_by points at the fact that replaced it. NULL =
			// active. Recall excludes superseded facts; /why still shows them.
			_, err := e.ExecContext(ctx,
				`ALTER TABLE memories ADD COLUMN superseded_by INTEGER`)
			return err
		},
	},
	{
		version: 6,
		name:    "fact subject (what it is about)",
		apply: func(ctx context.Context, e execer) error {
			// Layer 23: authority is per-domain, so the domain has to be data. A
			// fact now records WHAT it is about beside WHO stated it (see
			// subject.go); Supersedes reads both.
			//
			// Existing rows default to 'unspecified' and are deliberately NOT
			// backfilled. Guessing the subject of already-stored text would be the
			// model labelling data after the fact — the exact laundering this layer
			// prevents. Unspecified rows keep the pre-Layer-23 guarantee (provenance
			// alone); everything written from now on gets the stronger one.
			_, err := e.ExecContext(ctx,
				`ALTER TABLE memories ADD COLUMN subject TEXT NOT NULL DEFAULT 'unspecified'`)
			return err
		},
	},
	{
		version: 7,
		name:    "evidence polarity (contested claims)",
		apply: func(ctx context.Context, e execer) error {
			// Layer 24: a correction the trust model REFUSES is still information —
			// two sources disagreed about one subject. Until now that claim was
			// dropped, so memory could not represent a disputed belief (ADR 0005).
			//
			// The evidence trail gains a polarity. Every existing row is 'supports',
			// which is exactly what it was: before this migration the only thing the
			// trail could record was support. `detail` carries the text of a refused
			// claim, so the disagreement can be read later; it stays NULL on
			// supporting rows.
			//
			// Note what is NOT added: a `status`/`contested` column on memories.
			// Contested is DERIVED from these rows at read time, so the flag cannot
			// drift from the evidence that justifies it (ADR 0005, decision 3).
			if _, err := e.ExecContext(ctx,
				`ALTER TABLE evidence ADD COLUMN polarity TEXT NOT NULL DEFAULT 'supports'`); err != nil {
				return err
			}
			_, err := e.ExecContext(ctx, `ALTER TABLE evidence ADD COLUMN detail TEXT`)
			return err
		},
	},
	// Iteration 5 continues here, one migration per layer.
}

// latestSchemaVersion is the version a fully-migrated store reports.
func latestSchemaVersion() int {
	if len(migrations) == 0 {
		return 0
	}
	return migrations[len(migrations)-1].version
}

// runMigrations applies every migration newer than the store's recorded version,
// stamping the version after each. The meta table must already exist.
func (s *Store) runMigrations(ctx context.Context) error {
	current, err := s.schemaVersion(ctx)
	if err != nil {
		return err
	}
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if err := m.apply(ctx, tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d (%s): %w", m.version, m.name, err)
		}
		if err := metaSetOn(ctx, tx, metaSchemaVersion, []byte(strconv.Itoa(m.version))); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d stamp: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %d commit: %w", m.version, err)
		}
	}
	return nil
}

// schemaVersion reads the applied version from meta (0 if never stamped).
func (s *Store) schemaVersion(ctx context.Context) (int, error) {
	v, ok, err := s.metaGet(ctx, metaSchemaVersion)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(v)))
	if err != nil {
		return 0, fmt.Errorf("bad schema_version %q: %w", v, err)
	}
	return n, nil
}

// SchemaVersion returns the store's applied schema version (for doctor and tests).
func (s *Store) SchemaVersion(ctx context.Context) (int, error) { return s.schemaVersion(ctx) }

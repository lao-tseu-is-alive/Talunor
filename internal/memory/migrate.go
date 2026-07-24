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
	// Iteration 4 adds columns here, one migration per layer:
	//   {2, "fact provenance + confidence", ...},
	//   {3, "salience + decay bookkeeping", ...},
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

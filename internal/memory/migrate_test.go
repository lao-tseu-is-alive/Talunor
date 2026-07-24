package memory

import (
	"context"
	"testing"
)

func TestMigrateFreshStampsLatest(t *testing.T) {
	ctx := context.Background()
	s, err := Open(provConfig(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	v, err := s.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("schema version: %v", err)
	}
	if v != latestSchemaVersion() {
		t.Errorf("fresh store version = %d, want %d", v, latestSchemaVersion())
	}
	// The baseline migration created the memories table: the store must work.
	if _, err := s.Remember(ctx, KindFact, "", "User likes Go"); err != nil {
		t.Fatalf("remember after migrate: %v", err)
	}
}

func TestMigrateIdempotentAcrossReopen(t *testing.T) {
	ctx := context.Background()
	cfg := provConfig(t)

	s, err := Open(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := s.Remember(ctx, KindFact, "", "User is Carlos"); err != nil {
		t.Fatalf("remember: %v", err)
	}
	s.Close()

	// Reopening an already-migrated store must be a no-op (no re-apply, no error).
	s2, err := Open(cfg)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()

	if v, _ := s2.SchemaVersion(ctx); v != latestSchemaVersion() {
		t.Errorf("reopened version = %d, want %d", v, latestSchemaVersion())
	}
	if n, _ := s2.Count(ctx); n != 1 {
		t.Errorf("memory count after reopen = %d, want 1 (no data loss)", n)
	}
}

// TestMigrateBaselinesLegacy simulates a pre-versioning database (memories table
// present, no schema_version stamp): reopening must baseline it to the latest
// version without losing data.
func TestMigrateBaselinesLegacy(t *testing.T) {
	ctx := context.Background()
	cfg := provConfig(t)

	s, err := Open(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := s.Remember(ctx, KindFact, "", "User likes SQLite"); err != nil {
		t.Fatalf("remember: %v", err)
	}
	// Strip the version stamp to look like a legacy DB.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM meta WHERE key = ?`, metaSchemaVersion); err != nil {
		t.Fatalf("strip version: %v", err)
	}
	s.Close()

	s2, err := Open(cfg)
	if err != nil {
		t.Fatalf("reopen legacy: %v", err)
	}
	defer s2.Close()

	if v, _ := s2.SchemaVersion(ctx); v != latestSchemaVersion() {
		t.Errorf("baselined version = %d, want %d", v, latestSchemaVersion())
	}
	if n, _ := s2.Count(ctx); n != 1 {
		t.Errorf("memory count after baseline = %d, want 1 (no data loss)", n)
	}
}

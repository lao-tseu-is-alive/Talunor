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

func TestFactProvenanceAndConfidence(t *testing.T) {
	ctx := context.Background()
	s, err := Open(provConfig(t))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	// A fact stored with an explicit provenance and a (calibration-scaled) confidence.
	if _, err := s.RememberFact(ctx, "User's name is Carlos", ProvenanceUserStated, 0.63); err != nil {
		t.Fatalf("remember fact: %v", err)
	}
	hits, err := s.Recall(ctx, "what is my name?", 5, 0)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	var found bool
	for _, h := range hits {
		if h.Kind == KindFact {
			found = true
			if h.Provenance != ProvenanceUserStated {
				t.Errorf("provenance = %q, want %q", h.Provenance, ProvenanceUserStated)
			}
			if h.Confidence != 0.63 {
				t.Errorf("confidence = %v, want 0.63", h.Confidence)
			}
		}
	}
	if !found {
		t.Fatal("stored fact was not recalled")
	}

	// A turn derives its provenance from the role.
	if _, err := s.Remember(ctx, KindTurn, "user", "hi there"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Remember(ctx, KindTurn, "assistant", "hello back"); err != nil {
		t.Fatal(err)
	}
	mems, err := s.List(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	prov := map[string]Provenance{}
	for _, m := range mems {
		prov[m.Content] = m.Provenance
	}
	if prov["hi there"] != ProvenanceUserStated {
		t.Errorf("user turn provenance = %q, want user_stated", prov["hi there"])
	}
	if prov["hello back"] != ProvenanceModelInferred {
		t.Errorf("assistant turn provenance = %q, want model_inferred", prov["hello back"])
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
	// Look like a truly pre-versioning DB: the baseline schema, no version stamp and
	// none of the columns later migrations add. Reopening must migrate it forward
	// (baseline via migration 1's no-op, then migrations 2 & 3's ADD COLUMNs) losing
	// nothing.
	for _, stmt := range []string{
		`ALTER TABLE memories DROP COLUMN provenance`,
		`ALTER TABLE memories DROP COLUMN confidence`,
		`ALTER TABLE memories DROP COLUMN salience`,
		`ALTER TABLE memories DROP COLUMN last_accessed`,
		`ALTER TABLE memories DROP COLUMN access_count`,
		`DELETE FROM meta WHERE key = '` + metaSchemaVersion + `'`,
	} {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("simulate legacy (%s): %v", stmt, err)
		}
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

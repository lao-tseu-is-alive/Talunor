package memory

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// provConfig points at the repo-root ext/ artifacts and a fresh file-backed DB
// in a temp dir (provenance survives across Open, so :memory: won't do). It skips
// when `make deps` has not populated ext/.
func provConfig(t *testing.T) Config {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	cfg := Config{
		DBPath:         filepath.Join(t.TempDir(), "prov.db"),
		VectorExtPath:  filepath.Join(root, "ext", "vector"),
		AIExtPath:      filepath.Join(root, "ext", "ai"),
		EmbedModelPath: filepath.Join(root, "ext", "models", "all-MiniLM-L6-v2.f16.gguf"),
	}
	if _, err := os.Stat(cfg.VectorExtPath + ".so"); err != nil {
		t.Skip("extensions/model missing — run `make deps` first")
	}
	return cfg
}

// TestStoreFilePermissions: the memory database holds personal data, so Open
// creates its parent dir 0700 and the DB file 0600 (owner-only).
func TestStoreFilePermissions(t *testing.T) {
	cfg := provConfig(t)
	// A path whose parent does not exist yet, so Open must create it (t.TempDir's
	// own dir already exists and would bypass our MkdirAll).
	cfg.DBPath = filepath.Join(t.TempDir(), "sub", "talunor.db")

	s, err := Open(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()

	if info, err := os.Stat(filepath.Dir(cfg.DBPath)); err != nil {
		t.Fatal(err)
	} else if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("db dir mode = %o, want 700", perm)
	}
	if info, err := os.Stat(cfg.DBPath); err != nil {
		t.Fatal(err)
	} else if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("db file mode = %o, want 600", perm)
	}
}

// TestProvenanceFreshIsOK: a brand-new store stamps itself and reports OK, and
// the fingerprint survives a close/reopen (canary still matches).
func TestProvenanceFreshIsOK(t *testing.T) {
	ctx := context.Background()
	cfg := provConfig(t)

	s, err := Open(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if s.Provenance() != ProvenanceOK {
		t.Fatalf("fresh store: got %v, want ok", s.Provenance())
	}
	if _, err := s.Remember(ctx, KindFact, "", "User's name is Carlos."); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(cfg)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if s2.Provenance() != ProvenanceOK {
		t.Fatalf("reopen with matching stack: got %v, want ok", s2.Provenance())
	}
}

// TestProvenanceStaleThenReEmbed: corrupting the stored canary (as a real model
// swap would) makes the next Open report Stale; ReEmbed realigns and restores OK.
func TestProvenanceStaleThenReEmbed(t *testing.T) {
	ctx := context.Background()
	cfg := provConfig(t)

	s, err := Open(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	for _, txt := range []string{"alpha fact", "beta fact", "gamma fact"} {
		if _, err := s.Remember(ctx, KindFact, "", txt); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate a different embedding stack: overwrite the canary with the vector
	// of unrelated text, so the next Open's fresh canary won't match.
	bogus, err := s.Embed(ctx, "a completely different sentence in another space")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.metaSet(ctx, metaEmbedCanary, bogus); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(cfg)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if s2.Provenance() != ProvenanceStale {
		t.Fatalf("after canary corruption: got %v, want stale", s2.Provenance())
	}

	n, err := s2.ReEmbed(ctx, nil)
	if err != nil {
		t.Fatalf("re-embed: %v", err)
	}
	if n != 3 {
		t.Fatalf("re-embedded %d memories, want 3", n)
	}
	if s2.Provenance() != ProvenanceOK {
		t.Fatalf("after re-embed: got %v, want ok", s2.Provenance())
	}
	s2.Close()

	// The restored fingerprint must persist.
	s3, err := Open(cfg)
	if err != nil {
		t.Fatalf("reopen after re-embed: %v", err)
	}
	defer s3.Close()
	if s3.Provenance() != ProvenanceOK {
		t.Fatalf("reopen after re-embed: got %v, want ok", s3.Provenance())
	}
}

// TestProvenanceUnknownForLegacyDB: a store that already holds memories but has
// no recorded canary (i.e. it predates provenance tracking) reports Unknown, and
// a re-embed upgrades it to OK.
func TestProvenanceUnknownForLegacyDB(t *testing.T) {
	ctx := context.Background()
	cfg := provConfig(t)

	s, err := Open(cfg)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := s.Remember(ctx, KindFact, "", "legacy fact"); err != nil {
		t.Fatal(err)
	}
	// Erase the fingerprint to mimic a database created before this feature.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM meta WHERE key = ?`, metaEmbedCanary); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := Open(cfg)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if s2.Provenance() != ProvenanceUnknown {
		t.Fatalf("legacy DB without canary: got %v, want unknown", s2.Provenance())
	}
	if _, err := s2.ReEmbed(ctx, nil); err != nil {
		t.Fatalf("re-embed: %v", err)
	}
	if s2.Provenance() != ProvenanceOK {
		t.Fatalf("after re-embed: got %v, want ok", s2.Provenance())
	}
}

// TestCosineDistanceBlob checks the pure-Go distance helper against hand-built
// FLOAT32 vectors (identical, orthogonal, opposite) and malformed input.
func TestCosineDistanceBlob(t *testing.T) {
	v := func(fs ...float32) []byte {
		b := make([]byte, 0, len(fs)*4)
		for _, f := range fs {
			b = append(b, f32le(f)...)
		}
		return b
	}
	cases := []struct {
		name   string
		a, b   []byte
		want   float64
		wantOK bool
	}{
		{"identical", v(1, 0, 0), v(1, 0, 0), 0, true},
		{"orthogonal", v(1, 0, 0), v(0, 1, 0), 1, true},
		{"opposite", v(1, 0, 0), v(-1, 0, 0), 2, true},
		{"mismatched-len", v(1, 0), v(1, 0, 0), 0, false},
		{"empty", nil, nil, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, ok := cosineDistanceBlob(c.a, c.b)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if ok && math.Abs(d-c.want) > 1e-6 {
				t.Fatalf("distance = %v, want %v", d, c.want)
			}
		})
	}
	if !canaryMatches(v(1, 0, 0), v(1, 0, 0)) {
		t.Error("identical vectors should match")
	}
	if canaryMatches(v(1, 0, 0), v(0, 1, 0)) {
		t.Error("orthogonal vectors should not match")
	}
}

func f32le(f float32) []byte {
	bits := math.Float32bits(f)
	return []byte{byte(bits), byte(bits >> 8), byte(bits >> 16), byte(bits >> 24)}
}

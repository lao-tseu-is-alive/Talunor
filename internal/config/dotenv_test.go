package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/config"
)

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `# a comment
export TALUNOR_PROVIDER=openrouter
TALUNOR_MODEL = "anthropic/claude-sonnet-4"
EMPTY_LINE_ABOVE_IGNORED=yes

QUOTED_SINGLE='hello world'
NO_EQUALS_LINE
ALREADY_SET=from_file
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// A real env var must win over the file.
	t.Setenv("ALREADY_SET", "from_env")
	// Ensure the keys we assert on start unset (t.Setenv restores them after).
	for _, k := range []string{"TALUNOR_PROVIDER", "TALUNOR_MODEL", "QUOTED_SINGLE", "EMPTY_LINE_ABOVE_IGNORED"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}

	if err := config.LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv: %v", err)
	}

	checks := map[string]string{
		"TALUNOR_PROVIDER":         "openrouter",              // export stripped
		"TALUNOR_MODEL":            "anthropic/claude-sonnet-4", // quotes + spaces trimmed
		"QUOTED_SINGLE":            "hello world",             // single quotes stripped
		"EMPTY_LINE_ABOVE_IGNORED": "yes",
		"ALREADY_SET":              "from_env", // real env wins
	}
	for k, want := range checks {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q; want %q", k, got, want)
		}
	}
	if _, ok := os.LookupEnv("NO_EQUALS_LINE"); ok {
		t.Error("a line without '=' should be ignored")
	}
}

func TestLoadDotEnvMissingFileIsOK(t *testing.T) {
	if err := config.LoadDotEnv(filepath.Join(t.TempDir(), "nope.env")); err != nil {
		t.Errorf("missing .env should be nil, got %v", err)
	}
}

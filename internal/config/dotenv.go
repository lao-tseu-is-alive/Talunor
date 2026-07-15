// Package config holds small, dependency-free startup helpers shared by
// Talunor's commands — currently a minimal .env loader.
package config

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

// LoadDotEnv reads KEY=VALUE lines from path and sets them in the process
// environment. A missing file is not an error (returns nil), so calling it
// unconditionally at startup is safe.
//
// It is intentionally minimal — no external dependency:
//   - blank lines and lines starting with '#' are ignored;
//   - a leading "export " is stripped;
//   - surrounding matching quotes around the value are removed;
//   - **variables already set in the real environment win** — the .env only
//     fills in what is unset, so `VAR=x ./talunor` still overrides the file.
func LoadDotEnv(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, isSet := os.LookupEnv(key); isSet {
			continue // real environment takes precedence.
		}
		if err := os.Setenv(key, unquote(strings.TrimSpace(val))); err != nil {
			return err
		}
	}
	return sc.Err()
}

// unquote removes a single pair of matching surrounding quotes, if present.
func unquote(s string) string {
	if len(s) >= 2 {
		if q := s[0]; (q == '"' || q == '\'') && s[len(s)-1] == q {
			return s[1 : len(s)-1]
		}
	}
	return s
}

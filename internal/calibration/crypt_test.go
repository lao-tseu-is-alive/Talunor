package calibration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cryptSuiteYAML = "suite: enc\nscenarios:\n  - id: x\n    turns:\n      - user: hi\n        expect: { equals: hi }\n"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	enc, err := EncryptSuite([]byte(cryptSuiteYAML), "s3cr3t")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !IsEncrypted(enc) {
		t.Fatal("output should be recognised as encrypted")
	}
	if strings.Contains(string(enc), "hi") {
		t.Error("plaintext leaked into ciphertext envelope")
	}
	s, err := ParseMaybeEncrypted(enc, "s3cr3t")
	if err != nil {
		t.Fatalf("decrypt+parse: %v", err)
	}
	if len(s.Scenarios) != 1 || s.Scenarios[0].ID != "x" {
		t.Errorf("round-trip mismatch: %+v", s)
	}
}

func TestDecryptWrongKey(t *testing.T) {
	enc, _ := EncryptSuite([]byte(cryptSuiteYAML), "right")
	_, err := ParseMaybeEncrypted(enc, "wrong")
	if err == nil || !strings.Contains(err.Error(), "decrypt failed") {
		t.Fatalf("want decrypt-failed error, got %v", err)
	}
}

func TestEncryptedButNoKey(t *testing.T) {
	enc, _ := EncryptSuite([]byte(cryptSuiteYAML), "k")
	_, err := ParseMaybeEncrypted(enc, "")
	if err == nil || !strings.Contains(err.Error(), "no passphrase") {
		t.Fatalf("want no-passphrase error, got %v", err)
	}
}

func TestPlaintextPassesThrough(t *testing.T) {
	// Plaintext with a passphrase set: the key is simply ignored.
	if IsEncrypted([]byte(cryptSuiteYAML)) {
		t.Fatal("plaintext should not look encrypted")
	}
	s, err := ParseMaybeEncrypted([]byte(cryptSuiteYAML), "ignored")
	if err != nil {
		t.Fatalf("plaintext parse: %v", err)
	}
	if len(s.Scenarios) != 1 {
		t.Errorf("want 1 scenario, got %d", len(s.Scenarios))
	}
}

func TestLoadMaybeEncryptedFromFile(t *testing.T) {
	enc, _ := EncryptSuite([]byte(cryptSuiteYAML), "pw")
	path := filepath.Join(t.TempDir(), "suite.enc")
	if err := os.WriteFile(path, enc, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadMaybeEncrypted(path, "pw")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(s.Scenarios) != 1 {
		t.Errorf("want 1 scenario, got %d", len(s.Scenarios))
	}
}

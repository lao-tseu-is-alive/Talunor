package calibration

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"
)

// A calibration suite may be stored ENCRYPTED so it can be versioned in a shared
// repo without exposing the answers to a passive scraper (a training-data pipeline
// that ingests the repo would only see ciphertext). This is defense-in-depth, not
// a boundary:
//
//   - It does NOT protect against a HOSTED provider: to test it you hand it the
//     decrypted prompts at inference time. Fully private testing = a LOCAL model
//     (Ollama) + a private/encrypted suite.
//   - The key derivation is SHA-256 of a passphrase, not a slow KDF: it resists
//     scraping, not a determined attacker who has both the ciphertext and time.
//
// The scheme is standard AES-256-GCM (crypto/aes + crypto/cipher) — deliberately
// no home-grown cryptography.

const encMagic = "TALUNOR-CAL-ENC-1"

// keyFromPassphrase derives a 32-byte AES-256 key from any non-empty passphrase.
func keyFromPassphrase(passphrase string) [32]byte {
	return sha256.Sum256([]byte(passphrase))
}

// IsEncrypted reports whether data is a calibration encrypted envelope.
func IsEncrypted(data []byte) bool {
	return strings.HasPrefix(strings.TrimLeft(string(data), " \t\r\n"), encMagic)
}

// EncryptSuite encrypts plaintext with AES-256-GCM under passphrase and returns a
// magic-prefixed, base64 envelope — text, so it diffs and versions cleanly. The
// random nonce is prepended to the ciphertext before encoding.
func EncryptSuite(plaintext []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("calibration: empty encryption passphrase")
	}
	gcm, err := newGCM(passphrase)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, nil) // output = nonce || ciphertext+tag
	return []byte(encMagic + "\n" + base64.StdEncoding.EncodeToString(sealed) + "\n"), nil
}

// decryptSuite reverses EncryptSuite.
func decryptSuite(data []byte, passphrase string) ([]byte, error) {
	if passphrase == "" {
		return nil, fmt.Errorf("calibration: suite is encrypted but no passphrase (CALIBRATION_KEY) was given")
	}
	body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), encMagic))
	sealed, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return nil, fmt.Errorf("calibration: decode encrypted suite: %w", err)
	}
	gcm, err := newGCM(passphrase)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, fmt.Errorf("calibration: encrypted suite is truncated")
	}
	nonce, ct := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("calibration: decrypt failed (wrong CALIBRATION_KEY?): %w", err)
	}
	return plain, nil
}

func newGCM(passphrase string) (cipher.AEAD, error) {
	key := keyFromPassphrase(passphrase)
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// ParseMaybeEncrypted parses a suite from bytes that may be an encrypted envelope.
// Plaintext YAML passes straight through (passphrase ignored); an encrypted
// envelope is decrypted first (passphrase required). This is where the source-
// agnostic loader meets encryption without the core Parse having to know about it.
func ParseMaybeEncrypted(data []byte, passphrase string) (*Suite, error) {
	if IsEncrypted(data) {
		plain, err := decryptSuite(data, passphrase)
		if err != nil {
			return nil, err
		}
		return Parse(plain)
	}
	return Parse(data)
}

// LoadMaybeEncrypted reads a suite file and parses it, decrypting first if it is
// an encrypted envelope and passphrase is set.
func LoadMaybeEncrypted(path, passphrase string) (*Suite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("calibration: read suite %q: %w", path, err)
	}
	return ParseMaybeEncrypted(data, passphrase)
}

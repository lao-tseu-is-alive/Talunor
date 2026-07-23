// Package history is a persistent, deduplicated prompt history — the shell-like
// "recall a previous command with ↑/↓" affordance for Talunor's front-ends.
//
// Entries are kept unique and ordered oldest→newest: submitting a prompt that is
// already in the history moves it to the most-recent slot rather than adding a
// duplicate (like a shell with ignoredups + erasedups). The history persists to
// a file so recall works across sessions.
//
// Storage format is JSON-per-line (one JSON-encoded string per entry) so that
// multi-line prompts and special characters round-trip safely — a plain
// newline-delimited file could not represent a prompt that itself contains
// newlines.
//
// Navigation keeps a cursor and a stashed draft: pressing ↑ from the edit line
// stashes whatever the user was typing and walks back through older entries;
// pressing ↓ walks forward and restores the draft once past the newest entry.
package history

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// maxEntries caps the on-disk history so it cannot grow without bound; the
// oldest entries are dropped first.
const maxEntries = 1000

// History is a persistent, deduplicated prompt history with a navigation cursor.
// The zero value is not usable; build one with New. It is safe for concurrent
// use, though front-ends drive it from a single UI goroutine.
type History struct {
	mu      sync.Mutex
	path    string   // backing file; "" means in-memory only (no persistence).
	entries []string // unique, oldest→newest.

	// Navigation state. cursor indexes entries during a ↑/↓ walk; cursor ==
	// len(entries) means "on the edit line" (showing draft, not a history entry).
	cursor int
	draft  string
}

// New loads the history stored at path (missing or unreadable files yield an
// empty history — recall is a convenience, never a hard failure). Pass an empty
// path for an in-memory-only history that is never persisted.
func New(path string) *History {
	h := &History{path: path}
	h.load()
	h.cursor = len(h.entries)
	return h
}

// load reads entries from the backing file, ignoring blank/corrupt lines and
// de-duplicating (keeping the most recent occurrence's position).
func (h *History) load() {
	if h.path == "" {
		return
	}
	f, err := os.Open(h.path)
	if err != nil {
		return // no history yet, or unreadable: start empty.
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r\n")
		if line == "" {
			continue
		}
		var entry string
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // skip a corrupt line rather than aborting the load.
		}
		h.appendUnique(entry)
	}
	// A scan error (e.g. an over-long line) just truncates the load; the entries
	// read so far are valid and recall is best-effort, so it is not fatal.
	_ = sc.Err()
}

// appendUnique adds entry as the newest, first removing any earlier occurrence
// so the history holds only unique values. Blank entries are ignored. It does
// not touch navigation state or persist; callers handle those.
func (h *History) appendUnique(entry string) {
	if strings.TrimSpace(entry) == "" {
		return
	}
	for i, e := range h.entries {
		if e == entry {
			h.entries = append(h.entries[:i], h.entries[i+1:]...)
			break
		}
	}
	h.entries = append(h.entries, entry)
	if len(h.entries) > maxEntries {
		h.entries = h.entries[len(h.entries)-maxEntries:]
	}
}

// Add records a submitted prompt as the most-recent entry (moving it up if it
// was already present), resets navigation to the edit line, and persists. Blank
// prompts are ignored.
func (h *History) Add(entry string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if strings.TrimSpace(entry) == "" {
		return
	}
	h.appendUnique(entry)
	h.cursor = len(h.entries)
	h.draft = ""
	h.save()
}

// Prev returns the previous (older) entry for a ↑ keypress, given the text
// currently in the input. The first ↑ from the edit line stashes current as the
// draft; further ↑ walk backwards and stop at the oldest entry. ok is false when
// there is nothing to show (empty history).
func (h *History) Prev(current string) (entry string, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.entries) == 0 {
		return "", false
	}
	if h.cursor == len(h.entries) {
		h.draft = current // leaving the edit line: remember what was typed.
	}
	if h.cursor > 0 {
		h.cursor--
	}
	return h.entries[h.cursor], true
}

// Next returns the next (newer) entry for a ↓ keypress. Walking past the newest
// entry restores the stashed draft (the edit line). ok is false when already on
// the edit line (nothing newer to move to).
func (h *History) Next() (entry string, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cursor >= len(h.entries) {
		return "", false
	}
	h.cursor++
	if h.cursor == len(h.entries) {
		return h.draft, true // back to the edit line.
	}
	return h.entries[h.cursor], true
}

// Reset returns navigation to the edit line, discarding the stashed draft. Call
// it when the input is cleared or edited outside of ↑/↓ navigation.
func (h *History) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cursor = len(h.entries)
	h.draft = ""
}

// Len reports the number of stored entries.
func (h *History) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.entries)
}

// save writes the whole history to its backing file as JSON-per-line, via a
// temp file + rename so a crash mid-write cannot corrupt existing history. It is
// best-effort: a write error is swallowed (recall must never break input).
func (h *History) save() {
	if h.path == "" {
		return
	}
	if dir := filepath.Dir(h.path); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o700) // prompt history is personal; owner-only dir.
	}
	tmp, err := os.CreateTemp(filepath.Dir(h.path), ".history-*")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	w := bufio.NewWriter(tmp)
	enc := json.NewEncoder(w)
	for _, e := range h.entries {
		if err := enc.Encode(e); err != nil { // Encode writes a trailing newline.
			_ = tmp.Close()
			_ = os.Remove(tmpName)
			return
		}
	}
	if err := w.Flush(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return
	}
	_ = os.Rename(tmpName, h.path)
}

// DefaultPath returns the history file that lives next to the memory database at
// dbPath (e.g. …/talunor/history.jsonl), so history and memory share a home.
func DefaultPath(dbPath string) string {
	if dbPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(dbPath), "history.jsonl")
}

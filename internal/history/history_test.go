package history

import (
	"path/filepath"
	"testing"
)

// TestDedupMovesToNewest: re-adding an existing prompt keeps a single entry and
// promotes it to the most-recent slot (shell ignoredups + erasedups).
func TestDedupMovesToNewest(t *testing.T) {
	h := New("")
	h.Add("one")
	h.Add("two")
	h.Add("three")
	h.Add("one") // duplicate of the oldest.

	if got := h.Len(); got != 3 {
		t.Fatalf("Len = %d; want 3 (unique entries only)", got)
	}
	// Newest-first walk should be: one, three, two.
	want := []string{"one", "three", "two"}
	for i, w := range want {
		got, ok := h.Prev("")
		if !ok || got != w {
			t.Fatalf("Prev #%d = %q, ok=%v; want %q", i, got, ok, w)
		}
	}
}

// TestBlankIgnored: whitespace-only prompts are never stored.
func TestBlankIgnored(t *testing.T) {
	h := New("")
	h.Add("")
	h.Add("   ")
	h.Add("\n\t")
	if got := h.Len(); got != 0 {
		t.Fatalf("Len = %d; want 0 (blank prompts ignored)", got)
	}
}

// TestNavigationAndDraft: ↑ stashes the in-progress draft and walks back; ↓
// walks forward and restores the draft past the newest entry.
func TestNavigationAndDraft(t *testing.T) {
	h := New("")
	h.Add("first")
	h.Add("second")

	// ↑ from the edit line with a half-typed draft.
	if got, ok := h.Prev("draft"); !ok || got != "second" {
		t.Fatalf("Prev = %q, ok=%v; want second", got, ok)
	}
	if got, ok := h.Prev("draft"); !ok || got != "first" {
		t.Fatalf("Prev = %q, ok=%v; want first", got, ok)
	}
	// ↑ at the oldest entry stays put.
	if got, ok := h.Prev("draft"); !ok || got != "first" {
		t.Fatalf("Prev at oldest = %q, ok=%v; want first", got, ok)
	}
	// ↓ walks forward…
	if got, ok := h.Next(); !ok || got != "second" {
		t.Fatalf("Next = %q, ok=%v; want second", got, ok)
	}
	// …and past the newest restores the stashed draft.
	if got, ok := h.Next(); !ok || got != "draft" {
		t.Fatalf("Next to edit line = %q, ok=%v; want draft", got, ok)
	}
	// ↓ on the edit line reports nothing newer.
	if _, ok := h.Next(); ok {
		t.Fatalf("Next past edit line returned ok=true; want false")
	}
}

// TestPrevEmpty: navigating an empty history is a no-op.
func TestPrevEmpty(t *testing.T) {
	h := New("")
	if _, ok := h.Prev("x"); ok {
		t.Fatal("Prev on empty history returned ok=true; want false")
	}
}

// TestPersistenceRoundTrip: entries survive across New() calls, including a
// multi-line prompt (which the JSON-per-line format must round-trip).
func TestPersistenceRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")

	h1 := New(path)
	h1.Add("simple prompt")
	h1.Add("multi\nline\nprompt")
	h1.Add("simple prompt") // dedup: moves to newest.

	h2 := New(path)
	if got := h2.Len(); got != 2 {
		t.Fatalf("reloaded Len = %d; want 2", got)
	}
	// Newest-first: "simple prompt" then the multi-line one.
	if got, ok := h2.Prev(""); !ok || got != "simple prompt" {
		t.Fatalf("reloaded Prev = %q, ok=%v; want simple prompt", got, ok)
	}
	if got, ok := h2.Prev(""); !ok || got != "multi\nline\nprompt" {
		t.Fatalf("reloaded Prev = %q, ok=%v; want the multi-line prompt", got, ok)
	}
}

// TestReset returns navigation to the edit line.
func TestReset(t *testing.T) {
	h := New("")
	h.Add("a")
	h.Add("b")
	if _, ok := h.Prev(""); !ok {
		t.Fatal("Prev should succeed")
	}
	h.Reset()
	// After reset, Next reports nothing newer (we're on the edit line).
	if _, ok := h.Next(); ok {
		t.Fatal("Next after Reset returned ok=true; want false (on edit line)")
	}
}

// TestCap keeps the history bounded, dropping the oldest entries.
func TestCap(t *testing.T) {
	h := New("")
	for i := range maxEntries + 50 {
		h.Add(string(rune('a'+i%26)) + itoa(i))
	}
	if got := h.Len(); got != maxEntries {
		t.Fatalf("Len = %d; want capped at %d", got, maxEntries)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

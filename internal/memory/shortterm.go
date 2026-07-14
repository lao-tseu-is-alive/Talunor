package memory

import "sync"

// Turn is a single conversational message held in short-term memory.
type Turn struct {
	Role    string
	Content string
}

// ShortTerm is the immediate-context tier of Talunor's memory: a fixed-capacity
// ring buffer of the most recent conversation turns, kept verbatim. Unlike the
// long-term Store, it does no embedding and no retrieval — its whole job is to
// hand back the last N turns so recent context is never lost to semantic search.
//
// It is safe for concurrent use.
type ShortTerm struct {
	mu    sync.Mutex
	turns []Turn
	cap   int
}

// NewShortTerm creates a ring buffer holding at most capacity turns
// (clamped to a minimum of 1).
func NewShortTerm(capacity int) *ShortTerm {
	if capacity < 1 {
		capacity = 1
	}
	return &ShortTerm{cap: capacity, turns: make([]Turn, 0, capacity)}
}

// Add appends a turn, evicting the oldest if the buffer is full.
func (s *ShortTerm) Add(role, content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turns = append(s.turns, Turn{Role: role, Content: content})
	if len(s.turns) > s.cap {
		// Shift the retained tail to the front of the same backing array so the
		// offset never grows without bound.
		s.turns = append(s.turns[:0], s.turns[len(s.turns)-s.cap:]...)
	}
}

// Recent returns a copy of the buffered turns, oldest first.
func (s *ShortTerm) Recent() []Turn {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Turn, len(s.turns))
	copy(out, s.turns)
	return out
}

// Len returns the number of buffered turns.
func (s *ShortTerm) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.turns)
}

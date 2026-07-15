package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
)

// RecallMemory is a tool that searches Talunor's own long-term memory (semantic
// KNN over the store). It turns the agent's memory into an explicit capability:
// when the user refers to something from earlier, the model can look it up
// rather than guess. It is the same retrieval the agent does automatically each
// turn, exposed as an on-demand action.
type RecallMemory struct {
	store       *memory.Store
	k           int
	maxDistance float64
}

// NewRecallMemory builds the tool over a store (k=5, cosine threshold 0.75).
func NewRecallMemory(store *memory.Store) *RecallMemory {
	return &RecallMemory{store: store, k: 5, maxDistance: 0.75}
}

func (*RecallMemory) Name() string { return "recall_memory" }

func (*RecallMemory) Description() string {
	return "Search your own long-term memory for things the user told you earlier " +
		"(their name, preferences, facts, past conversation). Use this whenever the " +
		"user refers to something from before that isn't in the current conversation."
}

func (*RecallMemory) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "What to look up, in natural language (e.g. \"the user's favourite languages\")."
			}
		},
		"required": ["query"]
	}`)
}

func (r *RecallMemory) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if strings.TrimSpace(in.Query) == "" {
		return "", fmt.Errorf("query is required")
	}
	hits, err := r.store.Recall(ctx, in.Query, r.k, r.maxDistance)
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return "(no relevant memories found)", nil
	}
	var b strings.Builder
	for _, h := range hits {
		fmt.Fprintf(&b, "- %s\n", h.Content)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

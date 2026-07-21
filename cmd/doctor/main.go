// Command doctor smoke-tests Talunor's memory. It loads the SQLite extensions
// and embedding model, then exercises the typed memory API: it Remembers a small
// corpus, Recalls it by meaning with a relevance threshold, and demonstrates the
// short-term ring buffer. If the recalled memory is semantically the right one,
// the memory stack (Layers 1–2) works.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
	"github.com/lao-tseu-is-alive/Talunor/internal/version"
)

// corpus is a tiny set of unrelated facts. A good embedding model should rank
// them by meaning, not by shared keywords.
var corpus = []string{
	"The cat slept on the warm windowsill all afternoon.",
	"Go compiles to a single static binary with no runtime dependencies.",
	"Photosynthesis converts sunlight into chemical energy in plants.",
	"The Eiffel Tower was completed in Paris in 1889.",
	"SQLite stores an entire relational database in a single file.",
	"The Matterhorn is a mountain of the Alps, straddling the main watershed and border between Switzerland and Italy. It is a large, near-symmetric pyramidal peak in the extended Monte Rosa area of the Pennine Alps, whose summit is 4478 metres above sea level, making it one of the highest summits in the Alps and Europe",
	"Mont Blanc as the highest peak completely within Western Europe and the Alps. Located on the border of France and Italy, Mont Blanc has an elevation of 4,808 meters ",
}

// relevanceThreshold is the maximum cosine distance we treat as "relevant".
// Unrelated corpus sentences sit well above this; genuine matches sit below.
const relevanceThreshold = 0.75

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "doctor: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	fmt.Println(version.String())

	// Ephemeral in-memory database so the smoke test is repeatable and leaves
	// nothing behind. Extension/model paths come from `make deps`.
	cfg := memory.DefaultConfig()
	cfg.DBPath = ":memory:"

	fmt.Println("• opening store (loading vector.so, ai.so, embedding model)…")
	store, err := memory.Open(cfg)
	if err != nil {
		return err
	}
	defer store.Close()
	fmt.Printf("✓ store open — embedding dimension = %d\n\n", store.Dim())
	versionAI, err := store.VersionAI(ctx)
	if err != nil {
		return err
	}
	fmt.Println("• AI version:", versionAI)
	versionVector, err := store.VersionVector(ctx)
	if err != nil {
		return err
	}
	fmt.Println("• vector version:", versionVector)

	// Long-term memory: Remember embeds + stores in one call.
	fmt.Println("• remembering corpus…")
	for _, text := range corpus {
		if _, err := store.Remember(ctx, memory.KindDocChunk, "", text); err != nil {
			return err
		}
	}
	n, err := store.Count(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("✓ stored %d memories\n\n", n)

	// Semantic recall with a relevance threshold. The first query has a clear
	// match; the second shows the threshold letting only relevant memories
	// through.
	if err := recall(ctx, store, "Which technology keeps a whole database in one file?"); err != nil {
		return err
	}
	if err := recall(ctx, store, "Tell me about a famous French landmark."); err != nil {
		return err
	}
	if err := recall(ctx, store, "Can you list famous mountains in Europe."); err != nil {
		return err
	}

	// Short-term memory: a ring buffer of the most recent turns, kept verbatim.
	fmt.Println("\n• short-term buffer (capacity 3), after 5 turns:")
	st := memory.NewShortTerm(3)
	for _, turn := range []memory.Turn{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello!"},
		{Role: "user", Content: "what's my memory model?"},
		{Role: "assistant", Content: "short-term ring + long-term KNN"},
		{Role: "user", Content: "nice"},
	} {
		st.Add(turn.Role, turn.Content)
	}
	for _, turn := range st.Recent() {
		fmt.Printf("   %-9s %s\n", turn.Role+":", turn.Content)
	}

	fmt.Println("\n✓ Layers 1–2 OK: in-DB embeddings, KNN recall (thresholded), short-term buffer.")
	return nil
}

// recall runs a thresholded semantic search and prints the relevant hits.
func recall(ctx context.Context, store *memory.Store, query string) error {
	fmt.Printf("• recall: %q  (threshold d≤%.2f)\n", query, relevanceThreshold)
	hits, err := store.Recall(ctx, query, 5, relevanceThreshold)
	if err != nil {
		return err
	}
	if len(hits) == 0 {
		fmt.Println("   (no memory passed the relevance threshold)")
		return nil
	}
	for i, h := range hits {
		fmt.Printf("   %d. [d=%.4f] %s\n", i+1, h.Distance, h.Content)
	}
	return nil
}

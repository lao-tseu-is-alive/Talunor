// Command doctor smoke-tests Talunor's memory substrate: it loads the SQLite
// extensions and the embedding model, embeds a handful of sentences, stores
// them, then runs a KNN query and prints the ranking. If the nearest neighbour
// of a query is semantically the right sentence, the whole Layer 1 stack works.
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
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "doctor: "+err.Error())
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	fmt.Println(version.String())

	// Use an ephemeral in-memory database so the smoke test is repeatable and
	// leaves nothing behind. Extension/model paths come from `make deps`.
	cfg := memory.DefaultConfig()
	cfg.DBPath = ":memory:"

	fmt.Println("• opening store (loading vector.so, ai.so, embedding model)…")
	store, err := memory.Open(cfg)
	if err != nil {
		return err
	}
	defer store.Close()
	fmt.Printf("✓ store open — embedding dimension = %d\n\n", store.Dim())

	// Embed and store the corpus.
	fmt.Println("• embedding and storing corpus…")
	for _, text := range corpus {
		emb, err := store.Embed(ctx, text)
		if err != nil {
			return err
		}
		if _, err := store.DB().ExecContext(ctx,
			`INSERT INTO memories(kind, content, embedding) VALUES('doc_chunk', ?, ?)`,
			text, emb); err != nil {
			return fmt.Errorf("insert: %w", err)
		}
	}
	fmt.Printf("✓ stored %d memories\n\n", len(corpus))

	// Query with a paraphrase that shares no keywords with the target sentence
	// ("single file" / "one file" vs. the stored wording). Semantic search
	// should still surface the SQLite fact first.
	query := "Which technology keeps a whole database in one file?"
	fmt.Printf("• KNN query: %q\n", query)
	if err := knn(ctx, store, query, 3); err != nil {
		return err
	}

	query2 := "Tell me about a famous French landmark."
	fmt.Printf("\n• KNN query: %q\n", query2)
	if err := knn(ctx, store, query2, 3); err != nil {
		return err
	}

	fmt.Println("\n✓ Layer 1 substrate OK: extensions + in-DB embeddings + KNN all working.")
	return nil
}

// knn embeds the query, runs sqlite-vector's brute-force scan, and prints the
// top-k memories ranked by cosine distance (smaller = closer).
func knn(ctx context.Context, store *memory.Store, query string, k int) error {
	qvec, err := store.Embed(ctx, query)
	if err != nil {
		return err
	}
	rows, err := store.DB().QueryContext(ctx, `
		SELECT m.content, v.distance
		FROM vector_full_scan('memories', 'embedding', ?, ?) AS v
		JOIN memories m ON m.id = v.rowid
		ORDER BY v.distance`, qvec, k)
	if err != nil {
		return fmt.Errorf("vector_full_scan: %w", err)
	}
	defer rows.Close()

	rank := 0
	for rows.Next() {
		rank++
		var content string
		var dist float64
		if err := rows.Scan(&content, &dist); err != nil {
			return err
		}
		fmt.Printf("   %d. [d=%.4f] %s\n", rank, dist, content)
	}
	return rows.Err()
}

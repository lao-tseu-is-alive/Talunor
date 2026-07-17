# Lesson 03 — Semantic recall & embeddings

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🔍 Historical exploration** · Level 2 · **Advanced** · ~60 min

> **Advanced lesson.** It touches vectors and a little C-interop (cgo). If that's
> new, skim it now and come back later — you can happily continue to Lesson 04
> without fully mastering it.

## Why this lesson exists

In Lesson 01 you saw the magic: asking about *"a famous French landmark"* recalled
the *Eiffel Tower* memory, even though they share no words. That's **semantic
search** — matching by *meaning*. This lesson explains how it works, on the same
tag as Lesson 02, **`v0.2.0`**.

## Learning objectives

By the end you can:
- explain the difference between keyword search and semantic search;
- describe what an *embedding* is and what *cosine distance* measures;
- explain the **distance threshold** and the trade-off it controls.

## Prerequisites

- Lesson 02 done (same code, different lens).

## Check out the memory layer (same tag as Lesson 02)

```bash
git checkout v0.2.0     # detached HEAD — read only
```

Read, focusing on the *search* this time:

```text
internal/memory/store.go     # Embed and Dim — turning text into a vector
internal/memory/memory.go    # Recall, and the Hit type (note: Distance float64)
Makefile                     # `deps` fetches the embedding model (the .gguf file)
```

## The idea in three levels

**Intuition.** The computer turns each piece of text into a list of numbers (a
*vector*) that captures its *meaning*. Texts that mean similar things get vectors
that are *close together* — so "French landmark" lands near "Eiffel Tower", far
from "database password".

**Technique.** That list of numbers is an **embedding** (here, 384 numbers per
text, produced locally by the bundled model). "Closeness" is measured by **cosine
distance**: `0` = identical direction (same meaning), larger = more different.
`Recall` embeds your query, compares it to every stored vector, and returns the
nearest *k* — a **k-nearest-neighbours (KNN)** search.

**In Talunor.** Read the real signature (at `v0.2.0`):

```go
func (s *Store) Recall(ctx context.Context, query string, k int, maxDistance float64) ([]Hit, error)
// type Hit struct { …; Distance float64 }   // smaller Distance = more similar
```

`Embed(text)` makes the vector; `Recall` finds the closest `k` and returns them
with their `Distance`.

## The threshold — the knob that matters

`Recall` takes a `maxDistance`. In the code you'll see roughly:

```go
if maxDistance > 0 && h.Distance > maxDistance {
    // too far — drop it
}
```

That one line controls the whole quality of recall:

| `maxDistance` | Effect |
|---------------|--------|
| too small (strict) | nothing matches — the agent "forgets" relevant things |
| too large (loose) | everything matches — noise floods the prompt |
| just right (~0.75) | only genuinely relevant memories come back |

## Experiment

Run the smoke test and read the distances it prints:

```bash
make doctor
```

```text
• recall: "Which technology keeps a whole database in one file?"  (threshold d≤0.75)
   1. [d=0.2405] SQLite stores an entire relational database in a single file.
• recall: "Tell me about a famous French landmark."  (threshold d≤0.75)
   1. [d=0.6189] The Eiffel Tower was completed in Paris in 1889.
```

Notice the two distances: the SQLite match is *very* close (`0.24`), the Eiffel one
looser (`0.62`) but still under the `0.75` threshold — so both come back, and
unrelated memories (which would score higher) don't. **That gap is the threshold
doing its job.**

Return to the latest code when done:

```bash
git switch main
```

## Questions to answer

- Why does *"French landmark"* recall *"Eiffel Tower"* but not *"database
  password"*? (Answer in one sentence, using the word *meaning*.)
- What would happen to recall quality if you set `maxDistance` to `0.1`? To `2.0`?
- Why is a plain full scan of all vectors fine here, but a problem if the agent had
  a million memories? (This is a real, documented limitation — see the
  `CHANGELOG.md` on `main`.)

## Going further

- The embedding model runs **in-process via a C SQLite extension** — that's what
  `cgo_link.go` and cgo are about. You don't need to master it; just know *why* it
  exists: no external embedding service, everything works offline.
- On `main`, the same `Recall` gained a small over-fetch factor before filtering
  (`recallCandidateFactor`). See if you can find it — and reason about *why*
  fetching a few extra candidates before applying the role filter is safer.

## Common mistakes

- **Thinking embeddings "understand" text.** They don't reason — they place text in
  a space where *distance ≈ dissimilarity of meaning*. That's enough, and it's all
  it is.
- **Treating the threshold as one-size-fits-all.** It's a tuning knob; the right
  value depends on the model and the data.

## Completion checklist

- [ ] I can explain semantic search vs keyword search in one sentence.
- [ ] I can say what an embedding is and what cosine distance measures.
- [ ] I found the `maxDistance` check in `Recall` and can explain both extremes.
- [ ] I read the two distances in the `make doctor` output and understood why both
      matched.
- [ ] I returned to `main`.

**Next:** [Lesson 04 — LLM provider & streaming](../04-llm-provider-and-streaming/).

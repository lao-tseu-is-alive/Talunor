# 1. Memory backends — the `Embedder` / `VectorIndex` seams

- **Status:** Proposed / deferred (design recorded, not scheduled).
- **Date:** 2026-07-28
- **Deciders:** project owner + AI pair.
- **Supersedes / superseded by:** —

> This is the repo's first Architecture Decision Record. ADRs are contributor-facing
> internal reasoning (like `AGENTS.md`, `CHANGELOG.md`, `docs/atlas.md`) and are
> written in English; they are **not** part of the bilingual course. Keep each ADR
> short, honest about trade-offs, and append-only (add a new ADR to change a decision,
> don't rewrite a shipped one).

## The decision, up front

Two parts, because "should we support other vector stores?" is really two questions:

1. **For now: keep the single store as the only backend.** Talunor ships one memory
   backend — SQLite with the `sqlite-vector` (KNN) and `sqlite-ai` (in-DB embeddings)
   extensions, one file, fully offline. We do **not** build a backend abstraction
   speculatively. An interface with one implementation is indirection with no payoff;
   the current single-store design is genuinely *simpler* than any multi-backend one
   (see Consequences). YAGNI holds until a concrete second backend is actually wanted.
2. **If/when a second backend is wanted: this is the sanctioned seam.** Draw it at
   **`Embedder`** (text → vector) and **`VectorIndex`** (vector → neighbours), and keep
   the salience/decay/consolidation logic in a backend-agnostic core that never moves.
   This ADR records that shape so it is not re-litigated under time pressure later.

## Context

Two threads motivated the exploration:

- **Container CVE surface.** The published image scans with ~17 low/medium CVEs (0
  high/critical), all in the glibc/gcc runtime with *no fix available*. The heavy
  native dependency is the **embedder**: `objdump -p` shows `ai.so` needs `libstdc++`
  (a GGUF/llama.cpp-lineage C++ inference engine), while `vector.so` needs only `libc`.
  So the glibc weight lives in *embedding*, not in *vector search*.
- **Backend flexibility.** Would supporting chromem-go / pgvector / LanceDB / DuckDB VSS
  / Weaviate / Milvus be a good idea?

The key realisation: **Talunor's "memory" is not a vector store.** In `Recall`, the
vector engine's job is tiny — `vector_full_scan(...)` returns `(rowid, distance)`.
Everything distinctive happens in Go on top of that: the relational metadata (kind,
role, provenance, confidence, timestamps, migrations) and the **salience · decay ·
consolidation** ranking (`score = (1−distance)·confidence·effective_salience`, computed
lazily at read time — see [Lesson 18](../lessons/18-the-memory-of-the-gesture/) and
[`internal/memory/salience.go`](../../internal/memory/salience.go)). None of the external
databases do that; they replace ~10% of what memory does. So the seam must be drawn
*above* the vector layer, or the secret sauce fractures.

## The design (if built)

Three layers; the two swappable seams are `Embedder` and `VectorIndex`, and the
salience core sits above both.

### Seam 1 — `Embedder` (this seam owns the native-dependency weight)

```go
// text → vector. sqlite-ai impl links libstdc++ (glibc); an Ollama impl is a
// pure-Go HTTP call (the C++ weight moves to the Ollama process).
type Embedder interface {
    // purpose lets asymmetric retrieval models (nomic-embed-text, e5) prefix
    // "search_query"/"search_document"; symmetric models (all-MiniLM) ignore it.
    Embed(ctx context.Context, text string, purpose Purpose) (Vector, error)
    Dim() int   // vector dimension — schema + index depend on it
    ID() string // model identity; feeds the provenance canary (change ⇒ stale)
}
type Purpose int; const ( Document Purpose = iota; Query )
type Vector = []float32
```

### Seam 2 — `VectorIndex` (light on native deps)

```go
// Stores vectors keyed by the memory row id and does KNN. Knows NOTHING about
// provenance/confidence/salience — returns candidate ids + distances; the core ranks.
type VectorIndex interface {
    Upsert(ctx context.Context, id int64, vec Vector) error
    Delete(ctx context.Context, id int64) error
    KNN(ctx context.Context, query Vector, k int) ([]Neighbor, error)
    Dim() int
}
type Neighbor struct { ID int64; Distance float64 }
```

### The backend-agnostic core (stays put)

The `memories` table + migrations + provenance/confidence + **salience/decay/
consolidation** + recall ranking + reinforcement. It orchestrates the two seams:

| `Store` method | Embedder | VectorIndex | Core (metadata + the secret sauce) |
|---|---|---|---|
| `Remember` / `RememberFact` | `Embed(Document)` | `Upsert(id, vec)` | assign id + provenance/confidence, insert row |
| `Recall` | `Embed(Query)` | `KNN(qvec, k·factor)` | load metadata → distance gate → role filter → lazy decay → score → rank → top-k |
| `Reinforce` / `ReinforceFact` | — | — | UPDATE salience/confidence/access |
| `Forget` | — | `Delete(id)` | DELETE row |
| `ReEmbed` | `Embed(each)` | `Upsert(each)` | iterate rows, re-stamp provenance via `ID()` |
| provenance canary | `ID()` / `Dim()` | — | fingerprint the stack, flag stale |

The whole salience/decay/consolidation/ranking core sits *above* both seams and does
not change when a backend is swapped — the proof the abstraction is at the right
altitude. (A *third*, implicit seam is the metadata store itself — `mattn` cgo SQLite
vs `modernc.org/sqlite` pure-Go — but we keep that concrete in the core rather than
formalise a third interface, to avoid abstraction for its own sake.)

## Candidate backends, by seam

### Embedder candidates

| Candidate | Pure-Go binary? | Offline? | Notes |
|---|---|---|---|
| **sqlite-ai** (current) | ❌ cgo, libstdc++ | ✅ in-process | the "embeddings inside SQLite" showcase; owns the glibc weight |
| **Ollama + `nomic-embed-text`** | ✅ (HTTP client) | ❌ needs Ollama | better retrieval (768-dim, ~8k ctx, query/doc prefixes); moves C++ out of the Talunor binary/image → tiny, near-0-CVE image. Note: Ollama's *inference* is cgo/llama.cpp — you call the **service**, you do not import it as a library |
| pure-Go ONNX (e.g. MiniLM/nomic ONNX) | ⚠️ mature runtimes are cgo | ✅ in-process | the "offline *and* pure-Go" ideal, but immature today |

### VectorIndex candidates

| Candidate | Embedded? | Pure-Go? | Storage | Verdict |
|---|---|---|---|---|
| **sqlite-vector** (current) | ✅ | ❌ cgo (libc only, tiny) | one file | baseline |
| **chromem-go** | ✅ | ✅ | directory of gob files (`NewPersistentDB`, write-through) | the pure-Go alternative; persistence works but is a folder, not one file |
| **pgvector** | ❌ server | client pure, needs Postgres | server | conceptually cleanest (vectors + relational + SQL in one engine) → the *multi-user/production* graduation, not an embedded MVP |
| LanceDB | ✅ | ❌ (Rust core; Go binding is FFI/immature) | directory (Lance/Arrow) | great format, weak Go story → back to cgo; skip |
| DuckDB VSS | ✅ | ❌ (cgo, C++) | one file | single-file+SQL like SQLite but cgo/C++ (glibc, big), VSS persistence was experimental, OLAP-oriented → overkill |
| Weaviate / Milvus | ❌ server | n/a | server (HNSW, clustered) | solves a scale problem Talunor doesn't have; breaks offline/single-binary; *adds* attack surface — reject |

## Profiles the seams would unlock

- **Showcase (default, today):** mattn SQLite + `sqlite-vector` + `sqlite-ai` — embeddings
  inside SQLite, fully offline, one file, cgo/glibc.
- **Pure-Go static:** `modernc.org/sqlite` (pure-Go metadata) + chromem-go + Ollama/nomic
  → cgo-free binary, `FROM scratch`, ~0 OS CVEs. The honest way to answer the container-CVE
  question — by moving the C++ inference *out* to Ollama, not by fighting the base image.
  Cost: embeddings now need Ollama (no longer offline-standalone).
- **Scale-up:** pgvector — vectors + metadata + SQL in one server, multi-user; not embedded.

## Consequences

- **We lose single-file, single-row atomicity.** Today a memory's vector and metadata are
  one row in one SQLite file: delete the row → it is gone from KNN, atomically, with no
  separate index to maintain (`vector_full_scan` reads the live column). Splitting into
  `VectorIndex` + a metadata store means **two stores joined by `id`** that must be kept
  consistent (insert row, then upsert vector; on a failed upsert that id has no vector
  until a re-embed). This is the real price of the abstraction.
- **The embedder is the crux, not the vector store.** Swapping to chromem-go alone does
  *not* get Talunor off glibc — sqlite-ai still embeds. A truly static/no-glibc build needs
  a pure-Go *embedder*, and each option there has a real trade (Ollama = lose offline;
  pure-Go ONNX = immature). Design accordingly: the two seams have very different stakes.
- **Provenance already covers the migration.** Changing embedder changes `Dim()`/`ID()`,
  which trips the provenance canary and prompts `--reembed` — the machinery exists (Layer 11).
- **Pedagogy:** the split is a good future capstone lesson ("one memory contract, three
  substrates — where the salience logic *didn't* move, and where cgo actually lives"). But
  it deletes one existing teaching point ("embeddings run inside SQLite"), so keep the
  showcase profile as the default.

## Alternatives considered and rejected (for now)

- **Abstract at the `VectorIndex` level only** (not `Embedder`): rejected — it would leave
  the glibc weight (the embedder) in place and miss the actual container-CVE goal.
- **Abstract the whole `Store`** behind one interface: rejected — too coarse; the interface
  would be huge and every backend would re-implement the salience core identically.
- **Ship Weaviate/Milvus/DuckDB/LanceDB support:** rejected — none fit a single-user, offline,
  single-binary agent; the server ones break the model and add CVEs, the embedded native ones
  keep cgo without the offline/quality payoff.

## When to revisit

Build the seams only when a concrete driver appears — most likely: (a) a container image with
near-zero CVEs is wanted badly enough to accept "embeddings need Ollama" (→ Pure-Go static
profile), or (b) Talunor grows toward multi-user (→ pgvector). Until then, this ADR is the
record; the code stays single-store.

## References

- [`docs/architecture.md`](../architecture.md) — the mental model (the seams live inside the
  `memory` box).
- [`internal/memory/store.go`](../../internal/memory/store.go),
  [`memory.go`](../../internal/memory/memory.go),
  [`salience.go`](../../internal/memory/salience.go) — the current single store.
- Lessons [03 (semantic recall)](../lessons/03-semantic-recall/),
  [18 (salience/decay)](../lessons/18-the-memory-of-the-gesture/),
  [19 (single-connection-as-lock)](../lessons/19-off-the-critical-path/).

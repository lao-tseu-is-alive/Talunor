# Changelog

All notable changes to Talunor are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Talunor uses a `0.MINOR.PATCH` scheme where **each completed build layer bumps
`MINOR`**. Iteration 1 (a conversational agent with memory) completes at `0.5.0`.

This changelog doubles as a teaching log: each version records not just *what*
changed but the *lessons learned* while getting there.

## [Unreleased]

- Layer 4 — Agent loop: wire Perceive → Recall → Reason → Store into a single
  cognitive turn over the memory + provider.

## [0.3.0] - 2026-07-14 — Layer 3: LLM provider

The reasoning backend. A tiny streaming provider interface with an
OpenAI-compatible adapter, defaulting to a local Ollama server.

### Added

- `internal/llm` — the `Provider` interface (`Chat` streams a completion as a
  channel of `Chunk`s) plus:
  - `OpenAICompatible` — one adapter for every backend that speaks the OpenAI
    `/chat/completions` streaming API (Ollama now; OpenAI / OpenRouter later).
  - `NewOllama(model)` — a local Ollama provider (default model
    `qwen3:latest`, base URL `http://localhost:11434/v1`).
  - `Collect(...)` — drains a stream into the full answer string (for
    non-streaming callers and tests).
  - Types: `Message`, `Options` (model / temperature / max tokens), `Chunk`
    (carries `Content` **and** `Reasoning`).
- `cmd/chat` — one-shot smoke test: streams a prompt's reply to the terminal,
  rendering a thinking model's reasoning dimmed and its answer in full
  brightness. Prompt from args or stdin; `TALUNOR_MODEL` /
  `TALUNOR_OLLAMA_URL` env overrides.
- `internal/llm` tests: stream assembly, reasoning/answer separation, non-200
  setup error, in-stream error, connection refused — all against a mocked SSE
  server, so no live model is needed in CI.
- `make chat PROMPT="…"`.

### Design decisions

- **One adapter for three providers.** Ollama, OpenAI and OpenRouter all speak
  the OpenAI-compatible API, so `OpenAICompatible` covers them via base-URL +
  key. Only Anthropic (different Messages API) will need its own adapter.
- **Streaming as the primitive**, with `Collect` layered on top — not the other
  way around. The TUI (Layer 5) needs token-by-token output; a blocking call
  would have to be retrofitted, so streaming is the base and blocking is the
  convenience.
- **Setup errors vs. stream errors are distinct.** Connection refused / non-200
  come back as the `Chat` return error (fail fast, before any token); a failure
  mid-stream arrives as a terminal `Chunk.Err`. Callers can tell "never started"
  from "died partway".
- **No client-level HTTP timeout.** Long generations are normal; cancellation is
  the caller's `context`, not a fixed deadline.

### Lessons learned

1. **Thinking models split reasoning from the answer.** Ollama maps qwen3's
   chain-of-thought to a separate `reasoning` field in each SSE delta, leaving
   `content` empty until thinking finishes — so a small `max_tokens` can return
   an *empty* answer that spent its whole budget thinking. `Chunk` carries both
   fields, and `cmd/chat` renders reasoning dimmed so the distinction is visible.
2. **Test streaming without a model.** An `httptest` server replaying canned
   `data:` events exercises the whole SSE parser (assembly, `[DONE]`, error
   payloads, cancellation) deterministically and fast.
3. **The OpenAI-compatible surface is a real lever.** Pointing the same adapter
   at Ollama today and OpenAI/OpenRouter later costs only a base-URL and a key.

## [0.2.0] - 2026-07-14 — Layer 2: Memory API

A typed memory API over the Layer 1 substrate, plus the short-term tier. The
`doctor` now exercises the public API instead of raw SQL.

### Added

- `Store.Remember(ctx, kind, role, content)` — embeds content in-DB and inserts
  it in one call, returning the persisted row (id + timestamp via SQL
  `RETURNING`).
- `Store.Recall(ctx, query, k, maxDistance)` — the semantic-retrieval step: KNN
  over stored embeddings, nearest-first, with an optional cosine-distance
  threshold so only genuinely relevant memories are returned. This is what gets
  injected into the prompt before an LLM call.
- `Store.Count(ctx)` — number of stored memories.
- `ShortTerm` — the immediate-context tier: a fixed-capacity, concurrency-safe
  ring buffer of the most recent turns, kept verbatim (no embedding/retrieval).
- Typed model: `Kind` (`turn` / `doc_chunk`), `Memory`, `Hit` (memory +
  distance), `Turn`.
- `internal/memory` test suite: retrieval ranking, threshold filtering,
  `Remember` round-trip, and ring-buffer behaviour. Tests skip cleanly if
  `make deps` has not been run.

### Changed

- `cmd/doctor` now uses `Remember` / `Recall` and demonstrates the short-term
  buffer, instead of issuing raw SQL.

### Removed

- `Store.DB()` — the temporary Layer 1 escape hatch; the typed API replaces it.

### Design decisions

- **Two tiers, cleanly separated.** `ShortTerm` is pure in-memory recency;
  `Store` is embedded long-term recall. The agent loop (Layer 4) will write to
  both and read both, but neither knows about the other.
- **Threshold as a caller parameter** (`maxDistance`), not a hardcoded constant:
  retrieval relevance is a policy the caller owns. `0` means "no threshold".
  Empirically, related memories sit below ~0.7 cosine distance and unrelated ones
  above ~0.85, so ~0.75 is a sensible default (used by the doctor).

### Lessons learned

1. **A relevance threshold matters as much as top-k.** Plain KNN always returns
   `k` rows, including irrelevant ones when the store is sparse or the query is
   off-topic. Injecting those into a prompt is worse than injecting nothing.
   Filtering by cosine distance keeps recall precise (the doctor's first query
   now returns exactly one memory instead of three).
2. **`RETURNING` avoids a second round trip.** SQLite (bundled with
   `mattn/go-sqlite3`) supports `INSERT … RETURNING id, created_at`, so
   `Remember` gets the generated id and timestamp without a follow-up `SELECT`.
3. **`Recent()` must return a copy.** Handing out the internal slice would let
   callers mutate short-term memory by accident; a test pins this contract.

## [0.1.0] - 2026-07-14 — Layer 1: DB foundation

The persistence substrate for Talunor's memory, proven end to end
(load extensions → embed in-DB → KNN retrieval).

### Added

- `internal/memory` — a `Store` over SQLite (`mattn/go-sqlite3`, cgo) with two
  loadable C extensions from [sqliteai](https://github.com/sqliteai):
  - `sqlite-ai` (`ai.so`) runs a GGUF embedding model **in-process**, so
    embeddings are produced with plain SQL — no external embedding service.
  - `sqlite-vector` (`vector.so`) stores embeddings as `FLOAT32` BLOBs in an
    ordinary column and provides brute-force KNN via `vector_full_scan`.
- Embedding model: `all-MiniLM-L6-v2` (F16 GGUF), **384 dimensions**, cosine
  distance.
- `internal/version` — build identity (`Version`, `Commit`, `Date`), commit/date
  injected via `-ldflags` from the Makefile.
- `cmd/doctor` — a smoke test that embeds a small corpus, stores it, and runs
  KNN queries to confirm semantic retrieval works.
- `Makefile` — `make deps` fetches both extensions and the model into `ext/`
  (gitignored); `make doctor`, `make build`, `make clean`, `make distclean`.

### Design decisions

- **Single connection** (`db.SetMaxOpenConns(1)`): the loaded model, the
  embedding context, and `vector_init` are all *per-connection* state in these
  extensions. Pinning to one connection keeps that state valid and sidesteps a
  class of concurrency bugs — a fine trade-off for a single-user agent.
- **In-DB embeddings** over provider embeddings: fewer moving parts, offline,
  and a fixed 384-dim space independent of which chat LLM is used.
- Flat `memories(id, kind, role, content, embedding, created_at)` table for now;
  turns vs. document chunks are distinguished by `kind`, not separate tables.

### Lessons learned

1. **`sqliteai/sqlite-vector` is *not* the `vec0` virtual-table API.** That
   `vec0` syntax belongs to a different project (`asg017/sqlite-vec`). sqliteai's
   extension stores vectors as `FLOAT32` BLOBs in normal columns and searches
   with `vector_init(...)` + `vector_full_scan(table, col, query, k)`.
2. **`mattn/go-sqlite3`'s `LoadExtension(lib, "")` is broken for a default entry
   point.** It forwards `""` as a non-NULL empty C string, so SQLite calls
   `dlsym("")` and fails with an *empty* `undefined symbol:` message. Fix: pass
   the entry point explicitly — `sqlite3_vector_init`, `sqlite3_ai_init`.
3. **`vector.so` does not link libm.** It expects libm symbols (`fmaxf`, `exp`,
   …) to be resolvable in the global scope. Being merely a `NEEDED` dependency of
   the Go binary is not enough; the reliable fix is
   `dlopen("libm.so.6", RTLD_NOW | RTLD_GLOBAL)` at init (`cgo_link.go`).
4. **`sqlite-ai` v1.0.4 requires `embedding_type`** in
   `llm_context_create_embedding(...)`. The embedding flow is
   `llm_model_load(path,'gpu_layers=0')` → `llm_context_create_embedding('embedding_type=FLOAT32,normalize_embedding=1,pooling_type=mean')`
   → `llm_embed_generate(text,'json_output=0')`, which returns a `FLOAT32` BLOB
   directly usable for storage and as a query vector. `llm_model_n_embd()`
   reports the dimension.

### Requires

- `CGO_ENABLED=1` and a C toolchain (gcc).
- `make deps` before first build (downloads ~52 MB of extensions + model).

[Unreleased]: https://github.com/lao-tseu-is-alive/Talunor/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/lao-tseu-is-alive/Talunor/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/lao-tseu-is-alive/Talunor/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/lao-tseu-is-alive/Talunor/releases/tag/v0.1.0

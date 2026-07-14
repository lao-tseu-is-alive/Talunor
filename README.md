# Talunor

**Talunor** is a terminal-based, autonomous decision-making AI agent built in Go,
with a multi-tier memory backed by SQLite. It is developed **step by step as a
pedagogical project**: each layer is small, runnable, and documented, so the repo
reads as a guided tour of how to build a full cognitive-loop agent
(perception → reasoning → planning → action → learning) with guardrails.

> Current version: **v0.1.0** — Layer 1 of Iteration 1. See [CHANGELOG.md](CHANGELOG.md)
> for the version-by-version build log and lessons learned.

## Why it's interesting

- **Embeddings run *inside* SQLite.** A GGUF model (`all-MiniLM-L6-v2`, 384-dim)
  is executed in-process by the [`sqlite-ai`](https://github.com/sqliteai/sqlite-ai)
  extension — no external embedding service.
- **Vector search is just SQL.** The [`sqlite-vector`](https://github.com/sqliteai/sqlite-vector)
  extension stores embeddings as `FLOAT32` BLOBs and does KNN over them.
- **One file is the whole brain.** The agent's long-term memory is a single
  SQLite database file.

## Architecture (target)

```
Perception ─► Memory recall (KNN) ─► Reasoning (LLM) ─► Action ─► Learning
                    ▲                                              │
                    └───────────────── store ◄─────────────────────┘

internal/memory   SQLite + sqlite-vector + sqlite-ai (embeddings, KNN)   [Layer 1 ✓]
internal/llm      Provider interface + adapters (Ollama, OpenAI, …)      [Layer 3]
internal/agent    the cognitive loop                                     [Layer 4]
internal/tui      Bubble Tea + Glamour                                   [Layer 5]
internal/version  build identity                                         [✓]
cmd/doctor        memory substrate smoke test                            [✓]
```

## Status

### Iteration 1 — conversational agent + memory

| Layer | What | Status |
|-------|------|--------|
| 1 | **DB foundation** — load extensions, in-DB embeddings, KNN | ✅ done (v0.1.0) |
| 2 | **Memory API** — `Remember` / `Recall` (KNN + threshold), short-term ring buffer | ⏳ next |
| 3 | **LLM provider** — `Provider` interface + Ollama (OpenAI-compatible) adapter, streaming | ⬜ |
| 4 | **Agent loop** — Perceive → Recall → Reason → Store | ⬜ |
| 5 | **TUI** — Bubble Tea + Glamour | ⬜ |

### Later iterations

| Iter | Theme | Adds |
|------|-------|------|
| 2 | Tools & actions | tool registry, ReAct-style act/observe loop |
| 3 | Planning & guardrails | explicit planner, approval gates, policy checks |
| 4 | Learning | reflection, memory consolidation, salience/decay |

## Requirements

- Go 1.26+
- **`CGO_ENABLED=1`** and a C toolchain (gcc) — the SQLite extensions are C.
- Linux x86_64 (the fetched extension binaries; other platforms need the matching
  release assets — see the `Makefile`).
- ~52 MB of downloads on first setup (extensions + embedding model).

## Quickstart

```bash
make deps      # fetch sqlite-vector, sqlite-ai and the embedding model into ext/
make doctor    # smoke-test: embed a corpus, store it, run KNN queries
```

Expected `make doctor` output (abridged):

```
Talunor v0.1.0 (commit …, built …)
✓ store open — embedding dimension = 384
• KNN query: "Which technology keeps a whole database in one file?"
   1. [d=0.2405] SQLite stores an entire relational database in a single file.
   2. [d=0.8788] Photosynthesis converts sunlight into chemical energy in plants.
```

The nearest neighbour is chosen by *meaning*, with no shared keywords between the
query and the stored sentence.

## Lessons learned so far

Full details per version in [CHANGELOG.md](CHANGELOG.md). Highlights from Layer 1:

- `sqliteai/sqlite-vector` is **not** the `vec0` virtual-table API (that's the
  separate `asg017/sqlite-vec`); it uses BLOB columns + `vector_full_scan`.
- `mattn/go-sqlite3`'s `LoadExtension(lib, "")` needs an **explicit entry point**
  (`sqlite3_vector_init`, `sqlite3_ai_init`), or it fails with an empty
  `undefined symbol` error.
- `vector.so` relies on **libm being in the global symbol scope** — it's
  `dlopen`ed with `RTLD_GLOBAL` at startup.
- `sqlite-ai` needs `embedding_type=FLOAT32` when creating the embedding context.

## Layout

```
cmd/doctor/            memory substrate smoke test
internal/memory/       SQLite store: extensions, in-DB embeddings, KNN
internal/version/      build identity
ext/                   fetched .so extensions + GGUF model (gitignored)
Makefile               deps / doctor / build
CHANGELOG.md           version-by-version build log + lessons
```

## License

See [LICENSE](LICENSE).

# Talunor

**Talunor** is a terminal-based, autonomous decision-making AI agent built in Go,
with a multi-tier memory backed by SQLite. It is developed **step by step as a
pedagogical project**: each layer is small, runnable, and documented, so the repo
reads as a guided tour of how to build a full cognitive-loop agent
(perception → reasoning → planning → action → learning) with guardrails.

> Current version: **v0.4.0** — Layers 1–4 of Iteration 1. See [CHANGELOG.md](CHANGELOG.md)
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

internal/memory   SQLite store + short-term ring buffer (embeddings, KNN)  [Layers 1-2 ✓]
internal/llm      Provider interface + OpenAI-compatible adapter (Ollama)  [Layer 3 ✓]
internal/agent    the cognitive loop (perceive→recall→reason→store)        [Layer 4 ✓]
internal/render   shared streaming console renderer                        [✓]
internal/tui      Bubble Tea + Glamour                                     [Layer 5]
internal/version  build identity                                           [✓]
cmd/doctor        memory substrate smoke test                              [✓]
cmd/chat          LLM provider smoke test (streaming)                      [✓]
cmd/talunor       interactive agent REPL (persistent memory)               [✓]
```

## Status

### Iteration 1 — conversational agent + memory

| Layer | What | Status |
|-------|------|--------|
| 1 | **DB foundation** — load extensions, in-DB embeddings, KNN | ✅ done (v0.1.0) |
| 2 | **Memory API** — `Remember` / `Recall` (KNN + threshold), short-term ring buffer | ✅ done (v0.2.0) |
| 3 | **LLM provider** — `Provider` interface + Ollama (OpenAI-compatible) adapter, streaming | ✅ done (v0.3.0) |
| 4 | **Agent loop** — Perceive → Recall → Reason → Store | ✅ done (v0.4.0) |
| 5 | **TUI** — Bubble Tea + Glamour | ⏳ next |

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
- For chat: a local [Ollama](https://ollama.com) server with a model pulled
  (default `qwen3:latest`). Not needed for `make doctor` / `make test`.

## Quickstart

```bash
make deps      # fetch sqlite-vector, sqlite-ai and the embedding model into ext/
make doctor    # smoke-test: Remember a corpus, Recall it by meaning
make test      # run the test suite
```

Expected `make doctor` output (abridged):

```
Talunor v0.2.0 (commit …, built …)
✓ store open — embedding dimension = 384
• recall: "Which technology keeps a whole database in one file?"  (threshold d≤0.75)
   1. [d=0.2405] SQLite stores an entire relational database in a single file.
• recall: "Tell me about a famous French landmark."  (threshold d≤0.75)
   1. [d=0.6189] The Eiffel Tower was completed in Paris in 1889.
```

The recalled memory is chosen by *meaning* (no shared keywords), and the
relevance threshold drops everything unrelated — each query returns just its one
matching memory.

Talk to a local model (needs Ollama running):

```bash
make chat PROMPT="In one sentence, what is vector similarity search?"
# or:  go run ./cmd/chat "your prompt"   /   echo "your prompt" | go run ./cmd/chat
```

A thinking model's reasoning streams in dimmed, then its answer in full
brightness — a visible reminder that "reasoning" and "answer" are distinct.
Override the model with `TALUNOR_MODEL=qwen2.5-coder:14b`.

Run the interactive agent — it remembers across turns (and across sessions, via
a persistent `talunor.db`):

```bash
make run          # or: go run ./cmd/talunor
```

```
you> My name is Cedric and I love the Go programming language.
talunor> Ah, Cedric! Go is a fantastic choice …

you> What is my name and which language do I love?
talunor> Your name is Cedric, and you love the Go programming language.
```

The second answer comes from memory: the agent recalls the earlier turn (short-
term buffer + long-term KNN) and injects it into the prompt. Slash commands:
`/mem` (memory stats), `/exit`.

## Lessons learned so far

Full details per version in [CHANGELOG.md](CHANGELOG.md). Highlights:

**Layer 4 (agent loop)**

- **Loop order is a correctness issue**: recall must happen *before* the input is
  stored, or KNN returns the current message as its own top match.
- Streaming and "learning" cohabit via a tee goroutine — the user sees tokens
  live while the completed turn is captured once for storage.
- The assistant turn is stored only on clean completion; a cancelled/errored
  stream must not pollute memory.

**Layer 3 (LLM provider)**

- **Thinking models split reasoning from the answer**: Ollama returns qwen3's
  chain-of-thought in a separate `reasoning` field, so a small `max_tokens` can
  return an empty answer that spent its whole budget thinking.
- One OpenAI-compatible adapter serves Ollama, OpenAI and OpenRouter; only
  Anthropic needs its own.
- Streaming is the primitive; blocking (`Collect`) is layered on top.

**Layer 2 (memory API)**

- A **relevance threshold** matters as much as top-k: plain KNN always returns
  `k` rows, so an off-topic query still injects irrelevant memories into the
  prompt. Filtering by cosine distance keeps recall precise.
- `INSERT … RETURNING` fetches the new id + timestamp in one round trip.

**Layer 1 (DB foundation)**

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
cmd/chat/              LLM provider smoke test (streaming)
cmd/talunor/           interactive agent REPL (persistent memory)
internal/memory/       SQLite store: extensions, in-DB embeddings, KNN
internal/llm/          provider interface + OpenAI-compatible adapter
internal/agent/        the cognitive loop
internal/render/       shared streaming console renderer
internal/version/      build identity
ext/                   fetched .so extensions + GGUF model (gitignored)
Makefile               deps / doctor / chat / run / test / build
CHANGELOG.md           version-by-version build log + lessons
```

## License

See [LICENSE](LICENSE).

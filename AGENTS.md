# AGENTS.md — guide for AI coding agents working on Talunor

This file orients an AI (or human) contributor fast. Read it before making
changes. For the user-facing story see `README.md`; for the build-by-build
history and rationale see `CHANGELOG.md`.

## What Talunor is

A **pedagogical autonomous-agent MVP in Go**: a terminal assistant with a full
cognitive loop (perception → reasoning → planning → action → learning) and a
multi-tier memory. It is built **layer by layer, each layer a tagged release
with a documented lesson**, so the repo reads as a tutorial on how to build an
agent with guardrails. Optimise changes for clarity and teachability, not
cleverness.

Module: `github.com/lao-tseu-is-alive/Talunor` · Go 1.26 · **cgo required**.

## How it is built: the working agreement

- **One layer = one `MINOR` version.** Scheme `0.MINOR.PATCH`. Iteration 1
  (conversational agent + memory) spanned v0.1.0–v0.5.0; bugfixes/polish are
  `PATCH` bumps (v0.5.1, v0.5.2, …).
- **Every release, in lockstep:**
  1. Bump `Version` in `internal/version/version.go`.
  2. Add a `CHANGELOG.md` section **including a "Lessons learned" subsection** —
     this is the whole point; capture what was non-obvious.
  3. Sync `README.md` (status table, quickstart, env, layout).
  4. `gofmt`, `go vet ./...`, `go test ./...` all clean.
  5. Commit, then `git tag -a vX.Y.Z`, then push branch **and** tag to `origin`.
- **Linear history on `main`** — the user wants tags pushed directly to `main`,
  no PR branch. Commit messages: Conventional-Commits style, end with the
  `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` trailer.
- Work **step by step and checkpoint** with the user before starting the next
  layer.

## Architecture (package map)

```
cmd/doctor/     memory substrate smoke test (embed → store → KNN)
cmd/chat/       one-shot LLM streaming smoke test
cmd/talunor/    the app: TUI by default, --plain REPL, --list dump
internal/memory/   SQLite store: loadable extensions, in-DB embeddings, KNN,
                   Remember/Recall (thresholded), short-term ring buffer
internal/llm/      Provider interface + OpenAICompatible adapter (Ollama/OpenAI/…)
internal/agent/    the cognitive loop: Turn = perceive→recall→reason→store;
                   also slash-command helpers (Help/MemoryStats/ListMemories)
internal/render/   shared console stream renderer (reasoning dimmed, answer bright)
internal/tui/      Bubble Tea + Glamour front-end
internal/version/  build identity (Version const; Commit/Date via -ldflags)
ext/               fetched .so extensions + GGUF model (gitignored)
```

Data flow of one turn: input → `Store.Recall` (KNN, thresholded) + `ShortTerm`
recent turns → build prompt → `Provider.Chat` (stream) → render live →
`Store.Remember` both turns on clean completion.

## Build, test, run

```bash
make deps     # REQUIRED once: downloads ext/{vector,ai}.so + the GGUF model (~52MB)
make doctor   # smoke-test the memory substrate
make test     # go test ./...   (memory/agent/tui tests SKIP if deps missing)
make chat PROMPT="…"   # LLM streaming smoke (needs Ollama)
make run      # the agent TUI (needs Ollama)
make build    # -> bin/  (injects version via -ldflags)
```

- **`CGO_ENABLED=1` is mandatory** (the SQLite extensions are C). gcc required.
- Extensions/model are **not vendored**; `make deps` fetches them into `ext/`
  (Linux x86_64 assets are pinned in the `Makefile`).

## Environment variables

| Var | Purpose | Default |
|-----|---------|---------|
| `TALUNOR_MODEL` | Ollama chat model | `qwen3:latest` |
| `TALUNOR_OLLAMA_URL` | Ollama OpenAI-compatible base URL | `http://localhost:11434/v1` |
| `TALUNOR_DB` | database file | `$XDG_DATA_HOME/talunor/talunor.db` → `~/.local/share/talunor/talunor.db` |
| `TALUNOR_VECTOR_EXT` / `TALUNOR_AI_EXT` / `TALUNOR_EMBED_MODEL` | ext/model paths | under `ext/` |

Dev machine has Ollama running; `qwen3:latest` is a **thinking model** (see
gotchas). `qwen2.5-coder:14b` is a faster non-thinking alternative for smokes.

## Hard-won gotchas — do not rediscover these

### SQLite extensions (`sqliteai/sqlite-vector` + `sqlite-ai`, via `mattn/go-sqlite3`)
1. **`sqlite-vector` is NOT the `vec0` virtual-table API** (that's the separate
   `asg017/sqlite-vec`). It stores FLOAT32 BLOBs in ordinary columns:
   `vector_init(tbl,col,'dimension=384,type=FLOAT32,distance=cosine')` then KNN
   via `vector_full_scan(tbl,col,queryblob,k)` returning `(rowid, distance)`.
2. **Pass explicit extension entry points.** `mattn`'s `LoadExtension(lib, "")`
   forwards `""` as a non-NULL empty entry name → `dlsym("")` → empty
   `undefined symbol` error. Use `sqlite3_vector_init` / `sqlite3_ai_init`.
3. **`vector.so` needs libm in the global symbol scope.** `internal/memory/cgo_link.go`
   `dlopen`s `libm.so.6` with `RTLD_GLOBAL` at init. Do not remove it.
4. **`sqlite-ai` embedding flow:** `llm_model_load(path,'gpu_layers=0')` →
   `llm_context_create_embedding('embedding_type=FLOAT32,normalize_embedding=1,pooling_type=mean')`
   → `llm_embed_generate(text,'json_output=0')` returns a FLOAT32 BLOB directly
   storable and usable as a query vector. `embedding_type` is REQUIRED.
5. **One connection.** Model/embedding-context/`vector_init` are per-connection
   state, so `Store` pins `db.SetMaxOpenConns(1)`.

### LLM
6. **Thinking models split reasoning from answer.** Ollama returns qwen3's
   chain-of-thought in a separate `reasoning` field per SSE delta; `content`
   stays empty until it finishes. A small `max_tokens` yields an empty answer.
   `llm.Chunk` carries `Content` and `Reasoning` separately.
7. **Setup vs stream errors are distinct.** Connection refused / non-200 return
   from `Chat`; mid-stream failures arrive as a terminal `Chunk.Err`.

### TUI
8. **Never query the terminal from inside the render loop.** `glamour.WithAutoStyle`
   emits an OSC 11 background-color query; done inside the Bubble Tea loop its
   reply leaks onto the screen as escape-code garbage. Detect the background
   ONCE before `tea.NewProgram(...).Run()` (`lipgloss.HasDarkBackground()`) and
   build Glamour with `WithStandardStyle("dark"|"light")`.
9. **No mouse capture** (`tea.WithMouseCellMotion` is intentionally absent) so
   terminal text selection/copy still works; scrolling is keyboard-only.
10. **The stream→UI bridge:** `waitForChunk` reads one `llm.Chunk` per `tea.Cmd`,
    re-issued each `Update`. Render raw while streaming, Glamour once on
    completion. The model is a `*Model` so streaming state isn't copied.

## Testing conventions

- Tests needing the SQLite extensions/model resolve paths relative to the repo
  root and **`t.Skip` if `ext/` is absent** (so CI without `make deps` is green).
  Copy the `testStore`/`testConfig` helper pattern.
- LLM tests use an `httptest` SSE server — no live model.
- TUI tests are **headless**: feed synthetic `tea.Msg`s through `Update` and pump
  the returned `Cmd`s; assert on `View()`. A real terminal is not needed.
- Live TUI verification needs a PTY (`python3 pty.fork`); poll for the first
  frame (model load can take seconds) — see git history for the harness.

## Conventions

- Idiomatic Go; match surrounding style. Comments in English (or French,
  consistent per file). Follow DRY — check for an existing helper first
  (e.g. `internal/render`, `agent.FormatMemories`).
- Never write real secrets/credentials into files; use placeholders.
- Don't edit generated/fetched artifacts under `ext/`.

## Roadmap / status

- **Iteration 1 COMPLETE (v0.5.x):** conversational agent, multi-tier memory,
  streaming Ollama provider, agent loop, Bubble Tea TUI, config + commands.
- **Next — Iteration 2:** tools & actions (tool registry, ReAct act/observe
  loop), then guardrails/approval gates (the original goal, à la pi-go/Claude),
  then learning/reflection. Expect ~v0.6.0–v0.8.0, same checkpoint rhythm.

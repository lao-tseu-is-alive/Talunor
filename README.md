# Talunor

**Talunor** is a terminal-based, autonomous decision-making AI agent built in Go,
with a multi-tier memory backed by SQLite. It is developed **step by step as a
pedagogical project**: each layer is small, runnable, and documented, so the repo
reads as a guided tour of how to build a full cognitive-loop agent
(perception → reasoning → planning → action → learning) with guardrails.

> Current version: **v0.9.0** — Iterations 1 & 2 complete. The agent talks to
> local **Ollama** or hosted **OpenRouter** models (via `.env`) and *acts* — a
> ReAct tool loop (calculator, clock, memory search) with a human-in-the-loop
> **approval gate**, plus an opt-in, sandboxed **`bash`** tool that runs shell
> commands in a network-less throwaway container (nerdctl or a rootless
> user-namespace backend). See [CHANGELOG.md](CHANGELOG.md) for the
> version-by-version build log and lessons.

## Run without building

Each release (git tag `vX.Y.Z`) ships two prebuilt, self-contained artifacts so
you can try any iteration without a Go/C toolchain or `make deps`. Both bundle
the SQLite extensions **and** the embedding model, so memory works offline — only
the **chat** needs a local [Ollama](https://ollama.com) (default model
`qwen3:latest`). Linux **amd64** only (the sqliteai extensions are x86_64-only).

**Container image** (Docker or, with Rancher Desktop, `nerdctl` — same commands):

```bash
# The container reaches your host's Ollama via host.docker.internal. -v keeps
# long-term memory across runs. `docker run …` is identical. Port 11435 is the
# secure bridge below (use 11434 for the quick option).
nerdctl run --rm -it \
  --add-host=host.docker.internal:host-gateway \
  -e TALUNOR_OLLAMA_URL=http://host.docker.internal:11435/v1 \
  -v talunor-data:/data \
  ghcr.io/lao-tseu-is-alive/talunor:latest
# Add --plain for the REPL, --list 10 to inspect memory, or :v0.5.7 to pin a version.
```

- **Connecting to Ollama needs one-time host setup.** Ollama listens on
  `127.0.0.1` only, and under Rancher/Docker Desktop the container is in a VM —
  see **[Connecting the container to Ollama](docs/ollama-networking.md)**.
  Recommended: keep Ollama on localhost and bridge only the VM through a
  default-drop firewall (Option A = socat + systemd, Option B = pure nftables);
  the quick alternative exposes Ollama to your LAN. Native Docker Engine on Linux
  just needs `--network host`.
- **TTY:** the TUI needs `-it`. Without it, use `--plain` for the line REPL.
- Build the image locally: `make nerdctl-build && make nerdctl-run` (or the
  `docker-*` equivalents); override the endpoint with
  `make nerdctl-run OLLAMA_URL=http://host.docker.internal:11434/v1`.

**Standalone bundle** (a `.tar.gz` on each GitHub Release):

```bash
tar xzf talunor-v0.5.7-linux-amd64.tar.gz
cd talunor-v0.5.7-linux-amd64
./run.sh            # TUI  (./run.sh --plain for the REPL)
```

The bundle needs `libstdc++6` on the host (the `ai.so` embedding runtime links
it); the container image needs nothing. Verify downloads against `SHA256.txt`.

## Why it's interesting

- **Embeddings run *inside* SQLite.** A GGUF model (`all-MiniLM-L6-v2`, 384-dim)
  is executed in-process by the [`sqlite-ai`](https://github.com/sqliteai/sqlite-ai)
  extension — no external embedding service.
- **Vector search is just SQL.** The [`sqlite-vector`](https://github.com/sqliteai/sqlite-vector)
  extension stores embeddings as `FLOAT32` BLOBs and does KNN over them.
- **One file is the whole brain.** The agent's long-term memory is a single
  SQLite database file.
- **The agent writes its own memory.** After each turn a *reflection* step asks
  the model to distil durable facts from what you said ("User's favourite
  languages are Go and TypeScript") and stores them as **semantic** memory,
  separate from the verbatim **episodic** turns — so later questions recall a
  clean fact instead of a noisy sentence.

## Architecture (target)

```
Perception ─► Memory recall (KNN) ─► Reasoning (LLM) ─► Action ─► Learning
                    ▲                                              │
                    └───────────────── store ◄─────────────────────┘

internal/memory   SQLite store + short-term ring buffer (embeddings, KNN)  [Layers 1-2 ✓]
internal/llm      Provider interface + OpenAI-compatible adapter (Ollama)  [Layer 3 ✓]
internal/agent    the cognitive loop (perceive→recall→reason→store)        [Layer 4 ✓]
internal/render   shared streaming console renderer                        [✓]
internal/tui      Bubble Tea + Glamour front-end (default)                 [Layer 5 ✓]
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
| 5 | **TUI** — Bubble Tea + Glamour (default front-end) | ✅ done (v0.5.0) |

**Iteration 1 is complete** — Talunor is a working memory-augmented conversational
agent. Iteration 2 gives it the ability to *act*.

### Iteration 2 — tools & actions

| Layer | What | Status |
|-------|------|--------|
| 6 | **Providers & config** — OpenRouter provider, `llm.FromEnv()`, `.env` loader | ✅ done (v0.6.0) |
| 7 | **Tools & ReAct loop** — tool registry, native tool-calling, act→observe loop | ✅ done (v0.7.0) |
| 8 | **Approval gate** — human-in-the-loop y/n for side-effecting tools (guardrail) | ✅ done (v0.8.0) |
| 9 | **Sandboxed `bash`** — pluggable sandbox (namespaces/nerdctl), behind the gate | ✅ done (v0.9.0) |

### Later iterations

| Iter | Theme | Adds |
|------|-------|------|
| 3 | Planning & guardrails | explicit planner, policy checks (approval gate ✅ started) |
| 4 | Learning | memory consolidation, salience/decay, async reflection |

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

Run the interactive agent — a Bubble Tea TUI with Glamour-rendered markdown. It
remembers across turns (and across sessions, via a persistent `talunor.db`):

```bash
make run                 # TUI (default)
go run ./cmd/talunor --plain   # minimal line-based REPL instead
```

Try telling it something, then asking about it in a later turn — even after
restarting:

```
you> My name is Cedric and I love the Go programming language.
talunor> Ah, Cedric! Go is a fantastic choice …

you> What is my name and which language do I love?
talunor> Your name is Cedric, and you love the Go programming language.
```

The second answer comes from memory: the agent recalls the earlier turn (short-
term buffer + long-term KNN) and injects it into the prompt. In the TUI, a
thinking model's reasoning streams dimmed, then the answer renders as formatted
markdown; scroll with ↑/↓ or PgUp/PgDn, quit with Ctrl-C. The mouse is left free
so you can click-drag to select and copy text (e.g. to share a transcript); the
`--plain` REPL is also fully selectable and pipeable.

### Commands

Both the TUI and the `--plain` REPL understand:

| Command | Effect |
|---------|--------|
| `/help` | list commands |
| `/mem` | memory stats (count + database file) |
| `/list [n]` | list the most recent `n` memories (default 10) |
| `/forget <id>` | delete the memory with that `#id` (as shown by `/list`) |
| `/clear` | clear the on-screen transcript (TUI only; does not erase memory) |
| `/exit`, `/quit` | quit |

Inspect stored memory without starting a session:

```bash
go run ./cmd/talunor --list 20     # dump the 20 most recent memories and exit
```

### Choosing a model provider

Chat runs against **Ollama** (local, default) or **OpenRouter** (hosted frontier
models); embeddings always run locally from the bundled model. Select with
`TALUNOR_PROVIDER`:

```bash
# Local Ollama (default) — nothing to set.
TALUNOR_MODEL=qwen2.5-coder:14b talunor

# OpenRouter — a frontier model:
TALUNOR_PROVIDER=openrouter \
TALUNOR_MODEL=anthropic/claude-sonnet-4 \
OPENROUTER_API_KEY=sk-or-... \
  talunor
```

Configuration is easiest via a `.env` file: **`cp .env_sample .env`** and edit —
Talunor auto-loads it at startup (real environment variables still win). Every
supported variable is documented in [`.env_sample`](.env_sample). On a paid
provider, set `TALUNOR_REFLECT=0` to skip the per-turn reflection call.

### Tools (the ReAct loop)

Each turn, Talunor offers the model a set of tools and runs an act→observe loop:
the model asks to call a tool, Talunor runs it and feeds the result back, and
this repeats until the model answers. Built-in tools:

- **`calculator`** — safe arithmetic (parsed, never `eval`'d),
- **`current_time`** — current time in an optional timezone,
- **`recall_memory`** — searches Talunor's own long-term memory on demand.

Tool activity streams as dimmed notes (`🔧 calculator(…)` / `↳ 84`) before the
answer. Uses **native tool-calling**, so the chat model must support it (qwen3
and most OpenRouter frontier models do); set `TALUNOR_TOOLS=0` for a model that
doesn't.

### The sandboxed `bash` tool (opt-in)

Set `TALUNOR_BASH=1` to add a **`bash`** tool that runs shell commands in a
throwaway sandbox with **no network** and **no host filesystem** — only `/tmp` is
writable, and everything is discarded when the command ends. It is off by default
and, because it implements the v0.8.0 approval interface, **every call pauses for
your explicit y/N** before anything runs.

Two pluggable backends (pick with `TALUNOR_SANDBOX`, or let it auto-detect):

- **`nerdctl`** — delegates to a real OCI runtime (`nerdctl` or `docker`; Rancher
  Desktop works). This is the **strong** option: seccomp, cgroups, and dropped
  capabilities come for free. Runs `--network none --read-only` with `--pids-limit`,
  `--memory`, and a size-capped `--tmpfs /tmp`. Preferred when a runtime is present.
- **`namespaces`** — a from-scratch, **rootless** Linux sandbox built directly on
  user + mount + pid + net namespaces: it re-executes Talunor's own binary as a
  container init, `pivot_root`s into a cached busybox rootfs (read-only), mounts a
  fresh `/proc` and a size-capped `/tmp`, sets `no_new_privs`, drops all
  capabilities, and applies rlimits. An **empty net namespace** is why it has no
  network. This backend is **defense-in-depth and a teaching artifact, not a
  strong boundary** — there is *no seccomp filter*, so the whole syscall surface
  is reachable, and process-count limiting is best-effort (rootless cgroup
  delegation is usually unavailable; the memory cap + hard timeout contain a fork
  bomb). Use the `nerdctl` backend for genuinely untrusted code.

The `namespaces` backend is Linux-only and needs **unprivileged user namespaces**
enabled. On Ubuntu 24.04+ they are AppArmor-restricted by default; Talunor detects
this and tells you to lift the restriction (or just use the `nerdctl` backend).
The helper [`scripts/allow-unprivileged-userns.sh`](scripts/allow-unprivileged-userns.sh)
toggles it for you (`--restore` puts it back, `--status` shows the current state).
If the sandbox can't be set up, the tool is skipped with a warning — the app
still starts.

For the container image, [`scripts/run-container-with-ollama-bridge.sh`](scripts/run-container-with-ollama-bridge.sh)
starts the loopback→VM Ollama bridge and runs the container with the right
`nerdctl` flags in one step (see [docs/ollama-networking.md](docs/ollama-networking.md)).

### Where memory lives

Long-term memory is a single SQLite file. Its location is
`$TALUNOR_DB`, else `$XDG_DATA_HOME/talunor/talunor.db`, else
`~/.local/share/talunor/talunor.db` (created automatically) — so it persists
across sessions no matter where you launch from. The startup line prints the
active path.

### Environment

| Variable | Purpose | Default |
|----------|---------|---------|
| `TALUNOR_PROVIDER` | chat backend: `ollama` or `openrouter` | `ollama` |
| `TALUNOR_MODEL` | model for the selected provider | provider default |
| `TALUNOR_REFLECT` | set `0` to disable per-turn fact reflection | `1` |
| `TALUNOR_TOOLS` | set `0` to disable tools (model without tool support) | `1` |
| `TALUNOR_BASH` | set `1` to enable the sandboxed, approval-gated `bash` tool | `0` |
| `TALUNOR_SANDBOX` | bash backend: `nerdctl` or `namespaces` (unset = auto-detect) | auto |
| `TALUNOR_SANDBOX_IMAGE` | image for the `nerdctl` backend | `alpine:3.20` |
| `TALUNOR_SANDBOX_ROOTFS` / `TALUNOR_SANDBOX_BUSYBOX` | rootfs dir / busybox for the `namespaces` backend | built from a static busybox |
| `TALUNOR_OLLAMA_URL` | Ollama OpenAI-compatible base URL | `http://localhost:11434/v1` |
| `OPENROUTER_API_KEY` | required for `openrouter` | — |
| `TALUNOR_OPENROUTER_URL` | OpenRouter base URL | `https://openrouter.ai/api/v1` |
| `TALUNOR_DB` | database file | per-user data dir (above) |
| `TALUNOR_VECTOR_EXT` / `TALUNOR_AI_EXT` / `TALUNOR_EMBED_MODEL` | extension / model paths | under `ext/` |

See [`.env_sample`](.env_sample) for a copy-paste starting point.

## Lessons learned so far

Full details per version in [CHANGELOG.md](CHANGELOG.md). Highlights:

**Layer 5 (TUI)**

- A streaming channel maps cleanly onto Bubble Tea's `Cmd`/`Msg` model: one
  chunk per command, re-issued each update — no background goroutine mutating the
  model, no mutexes.
- Render raw text while streaming, run Glamour once on completion — smooth *and*
  correct (the reasoning/answer split from Layer 3, now visual).
- A TUI is testable without a terminal: feed synthetic `tea.Msg`s through
  `Update` and pump the returned `Cmd`s.

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
internal/tui/          Bubble Tea + Glamour front-end
internal/version/      build identity
ext/                   fetched .so extensions + GGUF model (gitignored)
Makefile               deps / doctor / chat / run / test / build / docker-*
Dockerfile             self-contained image (binary + extensions + model)
docs/ollama-networking.md  reaching a loopback Ollama from the container (secure)
.github/workflows/     CI (build+test), Release (bundle), Docker-publish, CVE scan
CHANGELOG.md           version-by-version build log + lessons
AGENTS.md              orientation guide for AI/human contributors
```

## License

See [LICENSE](LICENSE).

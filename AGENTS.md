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
  3. Sync `README.md` (status table, quickstart, env, layout) **and this file**
     (`AGENTS.md`: env table, package map, roadmap).
  4. **`make release-check`** must pass: gofmt + vet + tests, *plus* guards that no
     fetch target was silently dropped and the pinned checksums still match. For a
     networked, clean-room proof also run `make nerdctl-build`.
  5. Commit, then `git tag -a vX.Y.Z`, then push branch **and** tag to `origin`.
     The tag is the public release trigger, so run step 4 *before* tagging — green
     CI is not enough (CI does not exercise the release bundle step).
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
                   Remember/Recall (thresholded; recall excludes assistant
                   turns), Forget(id), short-term ring buffer. Kinds: turn
                   (episodic), fact (semantic), doc_chunk
internal/llm/      Provider interface + OpenAICompatible adapter (Ollama/OpenRouter),
                   FromEnv() provider selection, NewOpenRouter
internal/config/   minimal dependency-free .env loader (real env wins)
internal/agent/    the cognitive loop: Turn = perceive→recall→reason(act/observe
                   loop)→store→reflect. runLoop offers Config.Tools, executes
                   tool calls, feeds observations back (MaxToolIters cap), streams
                   the final answer (an unanswered tool-loop that hits MaxToolIters
                   ends with an explicit error, never silently). reflect.go =
                   FactExtractor (LLM distils facts into KindFact;
                   DisableReflection()). Optional Config.Debug (slog) traces
                   recall/tools/reflection. Slash-command helpers too.
internal/tools/    action layer: Tool interface + Registry; builtins Calculator
                   (AST-safe), Clock, RecallMemory (searches the store), Bash
                   (sandboxed shell; opt-in TALUNOR_BASH), WebFetch (SSRF-guarded
                   HTTP; opt-in TALUNOR_WEBFETCH). Approvable = coarse human-OK
                   interface; ApprovableFor = per-call gate from args (web_fetch's
                   allowlist bypass) — the agent prefers it when a tool implements it
internal/sandbox/  runs an untrusted script under limits; Sandbox iface + FromEnv.
                   Two backends: ociRuntime (nerdctl/docker — strong) and
                   namespaces (rootless userns re-exec — Linux-only, teaching, no
                   seccomp). Non-zero exit = output, not error. Linux files carry
                   //go:build linux; namespaces_other.go stubs elsewhere
internal/webfetch/ guarded HTTP fetcher for web_fetch: SSRF guard in the dialer
                   Control hook (blockedIP, DNS-rebinding-safe, re-checked per
                   redirect), timeout/MaxBytes/redirect limits, text-only bodies
internal/render/   shared console stream renderer (reasoning dimmed, answer bright)
internal/tui/      Bubble Tea + Glamour front-end (↑/↓ = prompt-history recall;
                   transcript scroll on PgUp/PgDn + Ctrl-U/D)
internal/history/  persistent, deduplicated prompt history (JSON-per-line next to
                   the DB; unique entries, temp-file+rename write, capped)
internal/version/  build identity (Version const; Commit/Date via -ldflags)
ext/               fetched .so extensions + GGUF model (gitignored)
```

Data flow of one turn: input → `Store.Recall` (KNN, thresholded) + `ShortTerm`
recent turns → build prompt → **act/observe loop**: `Provider.Chat` with tools;
while it returns tool calls, run them and append observations, then call again
(cap `MaxToolIters`); the final answer streams live, tool activity shows dimmed →
`Store.Remember` user + final answer → **reflect** (extractor distils durable
facts into `KindFact`, deduped). Learning runs before the stream closes. A tool
that implements `tools.Approvable` pauses the loop: the agent emits an
`llm.ApprovalRequest` on the stream and blocks on `Decision`; TUI/REPL prompt y/n
(deny → observation, fail closed).

## Build, test, run

```bash
make deps     # REQUIRED once: downloads ext/{vector,ai}.so + the GGUF model (~52MB)
make doctor   # smoke-test the memory substrate
make test     # go test ./...   (memory/agent/tui tests SKIP if deps missing)
make release-check  # pre-release gate: gofmt + vet + test + dep/checksum guards
make chat PROMPT="…"   # LLM streaming smoke (needs Ollama)
make run      # the agent TUI (needs Ollama)
make build    # -> bin/  (injects version via -ldflags)
make nerdctl-build && make nerdctl-run   # self-contained image (or docker-*)
```

- **`CGO_ENABLED=1` is mandatory** (the SQLite extensions are C). gcc required.
- Extensions/model are **not vendored**; `make deps` fetches them into `ext/`
  (Linux x86_64 assets are pinned in the `Makefile`).

## CI/CD & packaging (`.github/workflows/`, `Dockerfile`)

- **`ci.yml`** (push/PR to main): `make deps` + `go vet` + `go test` (cgo; caches
  `ext/`). **`cve-trivy-scan.yml`** (main + weekly): builds the image, Trivy scan,
  fails on fixable HIGH/CRITICAL.
- **Tag `vX.Y.Z`** fires two publishers: **`release.yml`** uploads a
  self-contained linux/amd64 `.tar.gz` (binary + extensions + model + `run.sh`) to
  the GitHub Release; **`docker-publish.yml`** builds, Trivy-scans/gates, and
  pushes `ghcr.io/lao-tseu-is-alive/talunor` (`{{version}}` + `sha` tags).
- **`Dockerfile`** is multi-stage (golang **bookworm** builder runs `make deps` +
  cgo build → **`gcr.io/distroless/cc-debian12`** runtime), baking the extensions
  + model in. Distroless/cc ships only glibc + libstdc++ + libgcc + ca-certs — the
  exact needs of the binary and `ai.so` — so the OS CVE surface is tiny (~17 total,
  0 HIGH/CRITICAL vs ~166/21 on debian-slim). Bookworm's glibc 2.36 satisfies the
  extensions (they need ≤ GLIBC_2.34 / GLIBCXX_3.4.29, checked via `objdump -T`).
  **amd64-only** (sqliteai ships no arm64 assets). Third-party action versions are
  pinned to commit SHAs (supply-chain), matching go-cloud-k8s-poc-2026.

## Environment variables

Selected via `llm.FromEnv()`; both commands load `.env` first (`internal/config`,
real env wins). See `.env_sample` for the full list.

| Var | Purpose | Default |
|-----|---------|---------|
| `TALUNOR_PROVIDER` | chat backend: `ollama` or `openrouter` | `ollama` |
| `TALUNOR_MODEL` | model for the selected provider | provider default |
| `TALUNOR_REFLECT` | `0` disables per-turn reflection (cost on paid APIs) | `1` |
| `TALUNOR_TOOLS` | `0` disables tools (model without tool-calling support) | `1` |
| `TALUNOR_DEBUG` | trace recall/tools/reflection: `1` → log file next to DB, `stderr`, or a path | off |
| `TALUNOR_BASH` | `1` enables the sandboxed, approval-gated `bash` tool | `0` |
| `TALUNOR_SANDBOX` | bash backend: `nerdctl`/`docker` or `namespaces` (unset = auto) | auto |
| `TALUNOR_SANDBOX_IMAGE` | image for the runtime backend | `alpine:3.20` |
| `TALUNOR_SANDBOX_ROOTFS` / `TALUNOR_SANDBOX_BUSYBOX` | rootfs dir / busybox for the namespaces backend | built from static busybox, cached |
| `TALUNOR_WEBFETCH` | `1` enables the SSRF-guarded, approval-gated `web_fetch` tool | `0` |
| `TALUNOR_WEBFETCH_ALLOW` | hosts skipping the fetch prompt (comma-sep; `.host`=sub-domains) | — |
| `TALUNOR_WEBFETCH_MAX_BYTES` / `TALUNOR_WEBFETCH_TIMEOUT` | fetch body cap / timeout | `524288` (512 KiB) / `10s` |
| `TALUNOR_OLLAMA_URL` | Ollama OpenAI-compatible base URL | `http://localhost:11434/v1` |
| `OPENROUTER_API_KEY` | required for `openrouter` | — |
| `TALUNOR_OPENROUTER_URL` | OpenRouter base URL | `https://openrouter.ai/api/v1` |
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

### Sandbox (`internal/sandbox`)
11. **The namespaces backend re-execs `/proc/self/exe`.** An `init()` in
    `namespaces_linux.go` hijacks the process when `TALUNOR_SANDBOX_CHILD=1` and
    becomes the container init *before* `main()` runs — the child shares Talunor's
    binary. This also means the backend works from a test binary (it imports the
    package, so the `init` is present).
12. **Rootless breaks the obvious limits.** RLIMIT_NPROC is per-host-uid (would
    throttle the user's own processes) and rootless cgroup delegation is usually
    absent, so there is **no reliable pids cap**; the memory rlimit + hard timeout
    (killing pid 1 of the pidns cascades) are what actually contain a fork bomb.
13. **Ubuntu 24.04+ AppArmor blocks unprivileged userns.**
    `kernel.apparmor_restrict_unprivileged_userns=1` makes `uid_map` writes fail
    with `EPERM`; `userNSAvailable()` detects it and points at the `sysctl` fix or
    the nerdctl backend. On such hosts the namespaces backend can't run — verify
    it after `sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0`.
14. **No seccomp in the namespaces backend** — it's defense-in-depth/teaching, not
    a boundary for hostile code. Say so; use the OCI runtime for real isolation.

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
  streaming Ollama provider, agent loop, Bubble Tea TUI, config + commands. v0.5.5
  adds reflection (semantic-memory facts) — an early taste of Iteration 4.
- **Iteration 2 (done):** v0.6.0 = providers & config (OpenRouter +
  `llm.FromEnv()` + `.env`); v0.7.0 = tools & actions (`internal/tools` registry,
  native tool-calling, `agent.runLoop` act/observe); v0.8.0 = approval gate
  (`tools.Approvable`, human y/n in TUI/REPL — Iteration 3 guardrail brought
  forward); v0.9.0 = sandboxed `bash` tool (`internal/sandbox`, nerdctl +
  rootless-namespaces backends, behind the gate, network-off) — **completes
  Iteration 2**. **v0.9.1 (patch)** = review quick-wins: bounded tool loop (no
  silent turns), persistent prompt history (`internal/history`, ↑/↓), `TALUNOR_DEBUG`
  trace, `make deps` checksums + `curl -f` hardening, non-root distroless image.
- **Layer 10 (done): v0.10.0** = `web_fetch`, the network opt-IN. `internal/webfetch`
  (SSRF guard in the dialer Control hook — DNS-rebinding-safe, per-redirect; timeout
  / MaxBytes 512 KiB / redirect caps; text-only) + `tools.WebFetch` behind
  `TALUNOR_WEBFETCH`. Introduces `tools.ApprovableFor` (per-call approval from args)
  for the `TALUNOR_WEBFETCH_ALLOW` allowlist — the allowlist skips the *prompt*, not
  the SSRF guard.
- **Next — Iteration 3**: an explicit planner before multi-step actions; policy
  checks for which tools/args are auto-allowed vs. need approval (generalising
  `ApprovableFor` into a policy the agent consults). Then Iteration 4 (learning/
  consolidation). Same per-layer checkpoint rhythm.

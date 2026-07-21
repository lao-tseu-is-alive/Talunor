# Talunor — Repository Atlas 🗺️

A guided map of the Talunor codebase: every tracked directory and file, each with
a one-line note on what it is and what it does.

- **Version:** `v0.11.0` (memory integrity: embedding provenance + re-embed; inline `/debug`)
- **Generated:** 2026-07-21
- **Scope:** *tracked files only.* Git-ignored paths are deliberately excluded —
  built binaries (`/bin`, `*.so`, `*.db`), fetched assets (`/ext`), local secrets
  (`.env`), personal notes (`todo.md`), and review output (`/reports`). Rebuild
  the ignored assets with `make deps`.

> Talunor is a **pedagogical autonomous-agent MVP in Go**: a terminal assistant
> with a full cognitive loop (perceive → recall → reason → act → learn) and
> multi-tier memory, built **layer by layer, each a tagged release with a
> documented lesson**. The tree below reads roughly in that layered order.

---

## Directory tree

```text
Talunor/
│
├── README.md                 # User-facing story: purpose, quickstart, tools, env, layout, lessons.
├── CHANGELOG.md              # Version-by-version build log; each release carries a "lessons learned".
├── AGENTS.md                 # Contributor guide (AI/human): architecture, conventions, release ritual, gotchas.
├── CLAUDE.md                 # Symlink → AGENTS.md, so Claude Code auto-loads the guide every session.
├── LICENSE                   # Project license.
├── go.mod / go.sum           # Go module definition and dependency checksums (Go 1.26, cgo).
│
├── Makefile                  # deps / doctor / build / test / run / docker-* + release-check gate.
│                             #   `deps` fetches + SHA256-verifies the SQLite extensions and GGUF model.
│                             #   `release-check` = gofmt + vet + test + dep/checksum guards (run before tag).
├── Dockerfile                # Multi-stage: bookworm builder (make deps + cgo) → distroless :nonroot runtime,
│                             #   extensions + model baked in. amd64-only.
├── .dockerignore             # Paths kept out of the Docker build context (e.g. ext/, so it fetches fresh).
├── .gitignore                # Ignored paths: build output, ext/ assets, *.db, .env, todo.md, /reports.
├── .env_sample               # Annotated template of every env var — copy to .env to configure.
│
├── images/                   # Static image assets referenced by the docs.
│   └── Talunor.jpg           #   Project logo shown at the top of the README.
│
├── .github/workflows/        # CI/CD pipelines (GitHub Actions).
│   ├── ci.yml                #   Push/PR to main: make deps + go vet + go test (cgo; caches ext/).
│   ├── cve-trivy-scan.yml    #   Main + weekly: builds the image, Trivy-scans, fails on fixable HIGH/CRITICAL.
│   ├── release.yml           #   On tag vX.Y.Z: build the self-contained linux/amd64 bundle → GitHub Release.
│   └── docker-publish.yml    #   On tag: build, Trivy-gate, push image to ghcr.io/lao-tseu-is-alive/talunor.
│
├── cmd/                      # Executable entry points (the binaries).
│   ├── talunor/main.go       #   THE APP. TUI by default, --plain REPL, --list dump, --reembed migration.
│   │                         #     Wires providers, tools (bash/web_fetch opt-in), prompt history, debug
│   │                         #     trace, and the startup embedding-provenance warning.
│   ├── chat/main.go          #   One-shot LLM streaming smoke test (verify a provider streams).
│   └── doctor/main.go        #   Memory-substrate smoke test: print ext versions → embed a corpus → store → KNN recall.
│
├── internal/                 # Private packages — one per teaching layer.
│   │
│   ├── memory/               # LAYER 1–2: SQLite store — loadable extensions, in-DB embeddings, KNN.
│   │   ├── store.go          #     Open the DB, load sqlite-vector + sqlite-ai, schema; one pinned conn
│   │   │                     #       (extension state is per-connection). DB path resolution.
│   │   ├── memory.go         #     Remember / Recall (KNN, thresholded, excludes assistant turns); Kinds
│   │   │                     #       (turn / fact / doc_chunk); Hit type; Forget; ext version accessors.
│   │   ├── provenance.go     #     LAYER 11: meta table fingerprints the embedding stack (canary vector);
│   │   │                     #       Open flags OK/Stale/Unknown; ReEmbed re-vectorises all rows.
│   │   ├── shortterm.go      #     Bounded ring buffer of the most recent turns (immediate context).
│   │   ├── cgo_link.go       #     cgo glue: dlopen libm with RTLD_GLOBAL — vector.so needs it in scope.
│   │   ├── provenance_test.go #    Tests (fresh=OK, canary mismatch=Stale→ReEmbed, legacy=Unknown, cosine).
│   │   └── memory_test.go    #     Tests (semantic recall, thresholding, assistant-turn exclusion).
│   │
│   ├── llm/                  # LAYER 3 / 6: LLM provider abstraction + OpenAI-compatible adapter.
│   │   ├── llm.go            #     Provider interface; Message / Chunk / Options / ApprovalRequest types.
│   │   ├── openai.go         #     OpenAICompatible streaming adapter (Ollama / OpenRouter, SSE parsing).
│   │   ├── config.go         #     Env-driven provider selection + default endpoints/models (FromEnv).
│   │   ├── openai_test.go    #     SSE streaming tests (over an httptest server, no live model).
│   │   └── config_test.go    #     Provider-selection tests.
│   │
│   ├── agent/                # LAYER 4: the cognitive loop (orchestrator).
│   │   ├── agent.go          #     Turn = perceive → recall → reason (act/observe loop) → store → reflect.
│   │   │                     #       Tool loop with MaxToolIters cap (errors, never silently); approval
│   │   │                     #       gate (Approvable / ApprovableFor); optional slog debug trace.
│   │   ├── reflect.go        #     FactExtractor: the LLM distils durable facts into semantic memory.
│   │   ├── debug.go          #     LAYER 11: /debug runtime toggle — streams recall rankings + reflection
│   │   │                     #       inline as dimmed Reasoning notes (TUI + --plain).
│   │   └── agent_test.go     #     Tests (recall+store, approval allow/deny, tool-loop cap, reflection, /debug).
│   │
│   ├── tui/                  # LAYER 5: Bubble Tea + Glamour terminal UI (default front-end).
│   │   ├── tui.go            #     Model/Update loop, stream→UI bridge, ↑/↓ history recall, approval prompt.
│   │   └── tui_test.go       #     Headless tests: feed synthetic tea.Msgs, assert on View().
│   │
│   ├── config/              # Minimal, dependency-free .env loader (real environment wins).
│   │   ├── dotenv.go        #     Parse a .env file into the environment.
│   │   └── dotenv_test.go   #     Parser tests (quotes, export, precedence).
│   │
│   ├── render/             # Shared console stream renderer (reasoning dimmed, answer bright) + approval.
│   │   └── render.go       #     Used by the --plain REPL to print a streaming reply with y/N prompts.
│   │
│   ├── tools/              # LAYER 7+: action layer — the capabilities the agent can call.
│   │   ├── tool.go         #     Tool + Registry; Approvable (coarse) and ApprovableFor (per-call) gates.
│   │   ├── builtin.go      #     Calculator (AST-safe, never eval'd) and Clock tools.
│   │   ├── memory.go       #     RecallMemory tool — lets the agent search its own long-term memory.
│   │   ├── bash.go         #     LAYER 9: Bash tool over the sandbox (opt-in, approval-gated, network-off).
│   │   ├── webfetch.go     #     LAYER 10: WebFetch tool (opt-in, SSRF-guarded, per-URL allowlist bypass).
│   │   ├── tools_test.go   #     Builtin + registry tests.
│   │   ├── bash_test.go    #     Bash-tool tests.
│   │   └── webfetch_test.go#     WebFetch allowlist-gating + Execute tests.
│   │
│   ├── sandbox/            # LAYER 9: run an untrusted shell script under isolation + resource limits.
│   │   ├── sandbox.go      #     Sandbox interface, Limits, DefaultLimits, FromEnv (backend selection).
│   │   ├── runtime.go      #     ociRuntime backend (nerdctl/docker) — the STRONG one (seccomp, cgroups).
│   │   ├── namespaces_linux.go # Rootless user-namespace re-exec backend — Linux-only, TEACHING, no seccomp.
│   │   ├── rootfs_linux.go #     Prepares/caches the busybox rootfs the namespaces backend pivot_roots into.
│   │   ├── namespaces_other.go # Non-Linux stubs (//go:build !linux) so the package still compiles.
│   │   ├── util.go         #     Shared sandbox helpers.
│   │   └── sandbox_test.go #     Backend behaviour tests (host-dependent; skip when unavailable).
│   │
│   ├── webfetch/           # LAYER 10 engine: the guarded HTTP fetcher behind the web_fetch tool.
│   │   ├── webfetch.go     #     Client/Fetch; SSRF guard = blockedIP (pure) enforced in the dialer's
│   │   │                   #       Control hook (DNS-rebinding-safe, re-checked per redirect); limits
│   │   │                   #       (timeout, 512 KiB cap, redirects); text-only bodies.
│   │   └── webfetch_test.go#     SSRF classifier table + redirect-to-internal-blocked + limits tests.
│   │
│   ├── history/            # Persistent, deduplicated prompt history (↑/↓ recall in the TUI).
│   │   ├── history.go      #     JSON-per-line store next to the DB; unique entries, temp-file+rename, capped.
│   │   └── history_test.go #     Dedup, navigation/draft, persistence round-trip tests.
│   │
│   └── version/            # Build identity.
│       └── version.go      #     Version const (0.MINOR.PATCH); Commit/Date injected via -ldflags.
│
├── docs/                  # Documentation.
│   ├── atlas.md           #   THIS FILE — the repository map.
│   ├── ollama-networking.md # Reaching a loopback Ollama from inside the container, securely.
│   └── lessons/           #   Hands-on course: a guided path through the tag-by-tag history.
│       │                  #     Each lesson is fully bilingual: README.md (EN, canonical) + README.fr.md (FR).
│       ├── README.md      #     Course index + prerequisites + the two-badge convention.
│       ├── 00-how-to-use-this-course/README.md  # Navigation: tags, detached HEAD, the reference docs.
│       ├── 01-first-contact/README.md           # First offline win (make doctor) + the v0.1.0 seed.
│       ├── 02-persistent-memory/README.md       # The SQLite store lifecycle at v0.2.0; short vs long term.
│       ├── 03-semantic-recall/README.md         # Embeddings, cosine distance, the recall threshold (v0.2.0).
│       ├── 04-llm-provider-and-streaming/README.md # The Provider interface + channel streaming (v0.3.0).
│       ├── 05-follow-the-agent-loop/README.md   # The minimal cognitive loop at v0.4.0, then its growth.
│       ├── 06-build-your-first-tool/README.md   # 🛠️ Add a unit_convert tool on main (extend, don't modify).
│       ├── 07-test-without-a-real-llm/README.md # 🛠️ Deterministic agent tests with a scripted provider.
│       ├── 08-observability-and-errors/README.md # 🛠️ Make a silent store error observable via the trace.
│       ├── 09-secure-web-fetching/README.md     # The SSRF guard at v0.10.0 (Control hook, blockedIP).
│       └── 10-understand-the-sandbox/README.md  # The two sandbox backends at v0.9.0 + honest boundaries.
│
└── scripts/               # Helper shell scripts.
    ├── initial_setup.sh   #   First-time dependency setup for the MVP.
    ├── allow-unprivileged-userns.sh # Toggle the Ubuntu AppArmor gate so the namespaces backend can run.
    └── run-container-with-ollama-bridge.sh # Start the loopback→VM Ollama bridge, then run the container.
```

---

## The layered reading order

If you are studying the repo as a tutorial, the packages map to build layers —
each one a tagged release (see `CHANGELOG.md`):

| Layer(s) | Package(s) | What it adds |
|----------|------------|--------------|
| 1–2 | `internal/memory` | SQLite + in-DB embeddings + KNN recall + short-term buffer |
| 3, 6 | `internal/llm` | streaming provider abstraction (Ollama, OpenRouter) |
| 4 | `internal/agent` | the cognitive loop (recall → reason → store → reflect) |
| 5 | `internal/tui` | the Bubble Tea terminal UI |
| 7 | `internal/tools` | tool registry + native tool-calling (ReAct loop) |
| 8 | approval gate | human-in-the-loop y/N (`Approvable`, in `agent` + `tools`) |
| 9 | `internal/sandbox` | run a real `bash` safely (kernel isolation) |
| 10 | `internal/webfetch` | reach the network safely (application-layer SSRF guard) |
| — | `internal/history`, `internal/version`, `internal/config`, `internal/render` | supporting infrastructure |

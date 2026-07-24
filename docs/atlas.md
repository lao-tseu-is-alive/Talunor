# Talunor — Repository Atlas 🗺️

A guided map of the Talunor codebase: every tracked directory and file, each with
a one-line note on what it is and what it does.

- **Version:** `v0.18.1` (course Lesson 19 — off the critical path)
- **Generated:** 2026-07-22
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
│   ├── doctor/main.go        #   Memory-substrate smoke test: print ext versions → embed a corpus → store → KNN recall.
│   └── calibrate/main.go     #   LAYER 14: model-calibration CLI — run a suite, save/diff a baseline
│                             #     (exit 1 on regression), `encrypt` a private suite (CALIBRATION_KEY).
│
├── internal/                 # Private packages — one per teaching layer.
│   │
│   ├── memory/               # LAYER 1–2: SQLite store — loadable extensions, in-DB embeddings, KNN.
│   │   ├── store.go          #     Open the DB, load sqlite-vector + sqlite-ai, schema; one pinned conn
│   │   │                     #       (extension state is per-connection). DB path resolution.
│   │   ├── memory.go         #     Remember / RememberFact / Recall (KNN, thresholded, excludes assistant
│   │   │                     #       turns; ranks by similarity·confidence·salience, soft-forgets faded);
│   │   │                     #       Kinds; Provenance + confidence (L16); salience/access (L17); Hit; Forget.
│   │   ├── provenance.go     #     LAYER 11: meta table fingerprints the embedding stack (canary vector);
│   │   │                     #       Open flags OK/Stale/Unknown; ReEmbed re-vectorises all rows.
│   │   ├── migrate.go        #     LAYER 15: ordered append-only migration runner; schema_version in meta;
│   │   │                     #       migration 1 = baseline; 2 = provenance/confidence; 3 = salience (L17).
│   │   ├── salience.go       #     LAYER 17: lazy decay (effective salience at read time), Reinforce /
│   │   │                     #       ReinforceFact (confidence only on independent evidence), forget floor.
│   │   ├── shortterm.go      #     Bounded ring buffer of the most recent turns (immediate context).
│   │   ├── cgo_link.go       #     cgo glue: dlopen libm with RTLD_GLOBAL — vector.so needs it in scope.
│   │   ├── provenance_test.go #    Tests (fresh=OK, canary mismatch=Stale→ReEmbed, legacy=Unknown, cosine).
│   │   ├── migrate_test.go   #     Tests (fresh stamps latest, idempotent reopen, legacy baseline no data loss).
│   │   ├── salience_internal_test.go # Pure-fn tests (decay, credibility, bounded confidence) — no DB/ext.
│   │   ├── salience_db_test.go #    Tests (reinforce bumps salience; confidence only on evidence; soft-forget).
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
│   │   ├── agent.go          #     Turn = perceive → recall → reason (reactLoop) → store → reflect.
│   │   │                     #       Async reflect worker (L18): enqueueReflect / Close(drain) / Quiesce.
│   │   │                     #       Tool loop with MaxToolIters cap (errors, never silently); each call
│   │   │                     #       consults Config.Policy (deny fails closed, risk≥medium prompts).
│   │   ├── planner.go        #     LAYER 13: Planner — LLM emits a validated plan.Plan (retry on bad JSON,
│   │   │                     #       never runs tools); opt-in Config.Planner / TALUNOR_PLANNER.
│   │   ├── execute.go        #     LAYER 13: runPlanned — plan → policy pre-screen → whole-plan approval
│   │   │                     #       → reactLoop capped to the plan's tools → learn; FormatPlan, /plan.
│   │   ├── reflect.go        #     FactExtractor: the LLM distils durable facts into semantic memory.
│   │   ├── debug.go          #     LAYER 11: /debug runtime toggle — streams recall rankings + reflection
│   │   │                     #       inline as dimmed Reasoning notes (TUI + --plain).
│   │   ├── agent_test.go     #     Tests (recall+store, approval allow/deny, tool-loop cap, policy deny/override).
│   │   ├── planner_test.go   #     Planner tests (happy path, retry-then-succeed, decodePlan tolerance).
│   │   └── execute_test.go   #     Planned-turn tests (whole-plan approval, deny/reject, high-risk, fallback).
│   │
│   ├── plan/                 # LAYER 12: the plan vocabulary shared by policy + (future) planner.
│   │   ├── plan.go           #     Plan / PlanStep (Type tool|think|final, Rationale required) + Validate;
│   │   │                     #       RiskLevel; NewToolCallPlan wraps one tool call as a one-step plan.
│   │   └── plan_test.go      #     Validation tests (required fields, unique ids, DependsOn resolvable).
│   │
│   ├── policy/               # LAYER 12: the action guardrail consulted before each tool call.
│   │   ├── policy.go         #     Policy interface + Decision{Allowed,Reason,Modified,RiskLevel};
│   │   │                     #       Denied() / NeedsApproval() mapping; AllowAllPolicy.
│   │   ├── toolgate.go       #     ToolGatePolicy (default): delegates to each tool's Approvable /
│   │   │                     #       ApprovableFor, reproducing pre-policy behaviour exactly.
│   │   ├── ruleengine.go     #     RuleEnginePolicy: data-driven YAML rules (allow/prompt/deny, TALUNOR_POLICY).
│   │   ├── policy_test.go    #     Decision-mapping + allow-all tests.
│   │   ├── toolgate_test.go  #     Tool-gate risk/approval tests with fake tools.
│   │   └── ruleengine_test.go #    YAML parse/evaluate, wildcard, deny, invalid-action, file-load tests.
│   │
│   ├── calibration/          # LAYER 14: deterministic reliability canary for an llm.Provider.
│   │   ├── scenario.go       #     Suite/Scenario/Turn types + Validate + source-agnostic Parse/Load (YAML).
│   │   ├── assert.go         #     Deterministic matchers (equals/contains/regex/number/json_valid/any_of); no LLM judge.
│   │   ├── runner.go         #     Run: replay each scenario N times clean-room → aggregate a Report.
│   │   ├── metrics.go        #     Stats (mean/stddev) — the continuous metric where a stddev is meaningful.
│   │   ├── report.go         #     Report / ScenarioResult / CategoryResult + human String().
│   │   ├── baseline.go       #     Baseline (pinned JSON) + Diff → drift/regression detection.
│   │   ├── crypt.go          #     Optional AES-256-GCM envelope (CALIBRATION_KEY); source-agnostic decrypt.
│   │   ├── assert_test.go    #     Matcher + validate tests.
│   │   ├── scenario_test.go  #     Parse/validate/load tests.
│   │   ├── runner_test.go    #     Run tests (all-pass, flaky 0.5, multi-turn, error, category agg) via a fake provider.
│   │   ├── baseline_test.go  #     Baseline round-trip + drift-detection tests.
│   │   └── crypt_test.go     #     Encrypt/decrypt round-trip, wrong-key, plaintext-passthrough tests.
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
│   ├── policy.sample.yaml #   Commented example TALUNOR_POLICY rule file (allow / prompt / deny per tool).
│   ├── calibration.seed.yaml #  LAYER 14: public example calibration suite (deterministic, threat-model header).
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
│       ├── 10-understand-the-sandbox/README.md  # The two sandbox backends at v0.9.0 + honest boundaries.
│       ├── 11-when-memory-forgets/README.md     # 🔍 Embedding provenance + /debug, from a real bug (v0.11.0).
│       ├── 12-the-open-bar/README.md            # 🔍 Why an agent needs a policy: the open-bar risk (v0.12.0).
│       ├── 13-plan-before-you-act/README.md     # 🔍 Emergent ReAct vs deliberate planning; capped execution (v0.13.0).
│       ├── 14-the-approval-that-didnt-bind/README.md  # 🔍 Post-mortem: plan-mode approval bound names not args (v0.13.1→fix).
│       ├── 15-dont-trust-the-review/README.md    # 🔍 Meta: falsify an AI code review's claims against ground truth.
│       ├── 16-measure-the-model/README.md        # 🔍 Build a deterministic reliability canary; drift over absolute (Layer 14).
│       ├── 17-learning-with-humility/README.md   # 🔍 Provenance + confidence; source-not-self-report; the calibration link (Layer 16).
│       ├── 18-the-memory-of-the-gesture/README.md # 🔍 Salience, lazy decay, consolidation & the independence rule (Layer 17).
│       └── 19-off-the-critical-path/README.md    # 🔍 Async reflection: single-conn-as-lock, the drain contract (Layer 18).
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
| 12 | `internal/policy` (+ `internal/plan`) | the action guardrail: allow / prompt / deny before each tool call |
| 13 | `agent/planner.go` + `agent/execute.go` | plan before acting: an approved plan, then ReAct execution capped to it |
| 14 | `internal/calibration` + `cmd/calibrate` | measure a model's reliability deterministically; detect silent drift |
| 15 | `internal/memory/migrate.go` | evolve the memory schema safely (ordered migrations) — the seam Iteration 4 builds on |
| 16 | `internal/memory` provenance/confidence + `agent` reflect | learn facts with a source + a calibration-scaled confidence; don't over-trust a model |
| 17 | `internal/memory/salience.go` + `agent` reflect/reinforce | give a memory a life: reinforce on recall, consolidate restatements, let the neglected fade |
| — | `internal/history`, `internal/version`, `internal/config`, `internal/render` | supporting infrastructure |

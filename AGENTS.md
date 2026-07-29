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
  3. Sync `README.md` (status table, quickstart, env, layout, **and the
     "Current version" banner** at the top) **and this file** (`AGENTS.md`: env
     table, package map, roadmap). If files were added or removed, regenerate
     **`docs/atlas.md`** (the `repo-atlas` skill).
  4. **`make release-check`** must pass: gofmt + vet + tests, *plus* guards that no
     fetch target was silently dropped, the pinned checksums still match,
     `docs/atlas.md` still references every tracked file (`atlas-check`), the
     README "Current version" banner matches `internal/version` (`readme-check`),
     and `docs/lessons/` references only valid tags/links/files (`lessons-check`).
     (These drift alarms can't judge whether prose is *accurate*, only that it's
     not stale/missing.) For a networked, clean-room proof also run `make nerdctl-build`.
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
cmd/calibrate/  standalone model-calibration CLI (Layer 14): run a suite, save/diff a
                baseline, `encrypt` a private suite; provider via llm.FromEnv()
internal/memory/   SQLite store: loadable extensions, in-DB embeddings, KNN,
                   Remember/Recall (thresholded; recall excludes assistant
                   turns), Forget(id), short-term ring buffer. Kinds: turn
                   (episodic), fact (semantic), doc_chunk. provenance.go: a `meta`
                   table fingerprints the embedding stack (canary vector) and
                   flags OK/Stale/Unknown on Open; ReEmbed re-vectorises all rows.
                   migrate.go (LAYER 15): ordered append-only migration runner;
                   schema_version int in `meta`; migration 1 = baseline (memories);
                   pre-versioning DBs are baselined automatically. SchemaVersion().
                   LAYER 16: each memory has provenance (user_stated/model_inferred/
                   tool_observed/unspecified) + confidence (system-assigned, never
                   model-self-reported). RememberFact(content,prov,conf); Remember
                   derives a turn's provenance from role; Recall/List expose both.
                   salience.go (LAYER 17): each memory has salience/last_accessed/
                   access_count (migration 3). Decay is LAZY — Recall computes
                   effective salience = salience·2^(−age/half-life) at read time (NO
                   writes: fits the single conn), ranks by similarity·confidence·
                   eff-salience, and soft-forgets below ForgetFloor (row survives).
                   Recall hides soft-forgotten rows from the prompt;
                   RecallForConsolidation (v0.18.2) shows them so reflection revives an
                   old fact instead of duplicating it.
                   Reinforce(ids) bumps salience (recall = it mattered);
                   ReinforceFact(id,gain) also raises confidence toward a <1 ceiling
                   with diminishing returns — but only on INDEPENDENT evidence
                   (EvidenceCredibility: user/tool=1, model_inferred=0, the echo
                   guard). Half-life/floor via TALUNOR_SALIENCE_HALFLIFE/_FORGET_FLOOR
                   evidence.go (LAYER 20): the evidence trail (migration 4). RecordEvidence/
                   EvidenceFor (which turns+sources support a fact, one row per store/reinforce)
                   + MemoryByID (for /why). Append-only; a fact with no rows = empty trail.
                   supersede.go (LAYER 21): the TRUST MODEL — Supersedes(newer,older) = the one
                   named function deciding who may retire whom (default: user/Verified-tool
                   authoritative, model_inferred retires nothing; swap it for another agent).
                   Store.Supersede soft-marks superseded_by (migration 5); Recall excludes them.
                   lexical.go + hybrid.go (LAYER 22): HYBRID RECALL. lexical.go owns the
                   FTS5 arm — an external-content index (content='memories') created
                   idempotently at Open (NOT a migration: it is derived data), kept in
                   sync by SQL triggers, queried with bm25(); matchExpression sanitises
                   user text into a quoted OR-expression (raw text would hit FTS5's own
                   query syntax). LexicalStatus = ok/unavailable/disabled: a build
                   without `-tags sqlite_fts5` has no fts5 module and degrades to
                   vector-only, reported by doctor + /mem. hybrid.go fuses the arms by
                   RECIPROCAL RANK FUSION (rrfK=60) because cosine distance and BM25
                   share no scale; with one arm the Layer-17 formula
                   (1-d)·confidence·eff-salience is kept verbatim, so a vector-only
                   build ranks exactly as before. Hit carries VectorRank/LexicalRank.
                   **Recall is hybrid; RecallForConsolidation stays VECTOR-ONLY** —
                   consolidation/supersession ask "is this the SAME fact?", a metric
                   question BM25 cannot answer (letting it in made reflection
                   consolidate onto merely word-similar facts). TALUNOR_RECALL=vector.
internal/llm/      Provider interface + OpenAICompatible adapter (Ollama/OpenRouter),
                   FromEnv() provider selection, NewOpenRouter
internal/config/   minimal dependency-free .env loader (real env wins)
internal/agent/    the cognitive loop: Turn = perceive→recall→reason(act/observe
                   loop)→store→reflect. reactLoop (shared core) offers Config.Tools,
                   executes tool calls, feeds observations back (MaxToolIters cap),
                   streams the final answer (an unanswered tool-loop that hits
                   MaxToolIters ends with an explicit error, never silently). Each
                   tool call is gated by Config.Policy (runTool wraps it as a
                   one-step plan.Plan and calls policy.Evaluate: deny fails closed,
                   medium+ risk prompts, Modified may rewrite the step). planner.go =
                   Planner (LLM emits a validated plan.Plan, retry on bad JSON, never
                   runs tools; opt-in Config.Planner/TALUNOR_PLANNER). execute.go =
                   runPlanned: plan→policy pre-screen→whole-plan approval
                   (Config.ApprovalMode plan|step|highrisk)→reactLoop capped to the
                   plan's tools→learn; /plan shows the last plan. reflect.go =
                   FactExtractor (LLM distils facts into KindFact;
                   DisableReflection()). LAYER 17: reflect CONSOLIDATES a restated
                   fact (knownFact → store.ReinforceFact) instead of skipping; Turn
                   reinforces recalled memories' salience (reinforceRecalled). LAYER
                   18: reflect is ASYNC — enqueueReflect → bounded reflectCh → a single
                   reflectWorker goroutine (started in New); Agent.Close() drains it,
                   Agent.Quiesce(ctx) waits (tests). One worker + single conn ⇒ no
                   extra locking. LAYER 20: reflect(job) learns from the turn's SOURCES
                   via learnFrom(text,prov,turnID) — user msg (user_stated), each tool
                   observation (model_inferred, or tool_observed iff tools.Verified;
                   trivial/empty skipped + size-capped), assistant answer (opt-in
                   Config.ReflectAssistant, off). Provenance is per-source/system-assigned
                   (sources extracted separately, never model-labelled). Records evidence
                   per store/reinforce; WhyMemory→/why. See ADR 0002. LAYER 21: arbiter.go —
                   FactArbiter classifies a new fact vs a near neighbour (restates/supersedes/
                   unrelated; default LLM, DisableArbiter() falls back to L20). learnOneFact
                   PROPOSES via the arbiter, then the memory.Supersedes trust model GATES: a
                   model inference is dropped, not allowed to overwrite the user; a Verified
                   tool can retire a stale belief. Candidate radius = wider SupersedeMaxDistance.
                   See ADR 0003. Optional Config.Debug (slog) traces
                   recall/tools/reflection. debug.go: the /debug runtime toggle
                   (screenDebug) streams recall rankings inline as dimmed Reasoning
                   notes (reflection notes now go to the log, being async). Slash-command helpers too.
internal/plan/     plan vocabulary shared by policy + (future) planner: Plan{Goal,
                   Steps, Confidence}, PlanStep{ID, Type tool|think|final, Tool,
                   Arguments, Rationale, DependsOn} with Validate(); RiskLevel;
                   NewToolCallPlan wraps one tool call as a one-step plan
internal/policy/   action guardrail: Policy.Evaluate(ctx,*Plan,PlanStep)→Decision
                   {Allowed, Reason, Modified, RiskLevel}. Decision.Denied() /
                   NeedsApproval() (RiskLevel≥medium) centralise the mapping.
                   AllowAllPolicy; ToolGatePolicy (default — consults each tool's
                   Approvable/ApprovableFor, preserves pre-policy behaviour);
                   RuleEnginePolicy (YAML rules, TALUNOR_POLICY)
internal/calibration/ LAYER 14: deterministic reliability canary for an llm.Provider.
                   Suite/Scenario/Turn/Assert from YAML (source-agnostic Parse; 1–5
                   clean-room turns). Matchers are DETERMINISTIC-only (no LLM judge):
                   equals/contains/regex/number/json_valid/any_of. Run → pass-rate
                   (≈0.5=flaky) + latency mean±stddev; Baseline+Diff = drift detection;
                   optional AES-256-GCM (CALIBRATION_KEY) for a private suite
internal/tools/    action layer: Tool interface + Registry; builtins Calculator
                   (AST-safe), Clock, RecallMemory (searches the store), Bash
                   (sandboxed shell; opt-in TALUNOR_BASH), WebFetch (SSRF-guarded
                   HTTP; opt-in TALUNOR_WEBFETCH). Approvable = coarse human-OK
                   interface; ApprovableFor = per-call gate from args (web_fetch's
                   allowlist bypass) — the default ToolGatePolicy consults these.
                   Verified (LAYER 20): optional — a tool declaring its output a
                   deterministic verified fact ⇒ learned as tool_observed (no builtin
                   implements it yet; the honest seam, ADR 0002)
internal/sandbox/  runs an untrusted script under limits; Sandbox iface + FromEnv.
                   Two backends: ociRuntime (nerdctl/docker — strong) and
                   namespaces (rootless userns re-exec — Linux-only, teaching, no
                   seccomp). Non-zero exit = output, not error. Linux files carry
                   //go:build linux; namespaces_other.go stubs elsewhere.
                   v0.20.2: verifyChildIdentity(pid,tokenFD,env) authenticates the
                   re-exec (pid 1 + per-run token on fd 3) before childMain mounts
                   anything — an ambient TALUNOR_SANDBOX_CHILD=1 can no longer
                   hijack a binary linking this package (gotcha 11)
internal/webfetch/ guarded HTTP fetcher for web_fetch: SSRF guard in the dialer
                   Control hook (blockedIP, DNS-rebinding-safe, re-checked per
                   redirect), timeout/MaxBytes/redirect limits, text-only bodies
internal/render/   shared console stream renderer (reasoning dimmed, answer bright)
internal/tui/      Bubble Tea + Glamour front-end (↑/↓ = prompt-history recall;
                   transcript scroll on PgUp/PgDn + Ctrl-U/D)
internal/history/  persistent, deduplicated prompt history (JSON-per-line next to
                   the DB; unique entries, temp-file+rename write, capped)
internal/version/  build identity (Version const; Commit/Date via -ldflags)
internal/testenv/  test-only capability contract (v0.20.4): Require(t,cap,err) skips
                   by default, FAILS when TALUNOR_REQUIRE names the capability
                   (ext|sandbox|docker|all). Imported only from _test.go files
ext/               fetched .so extensions + GGUF model (gitignored)
```

Data flow of one turn: input → `Store.Recall` (KNN, thresholded) + `ShortTerm`
recent turns → build prompt → **act/observe loop**: `Provider.Chat` with tools;
while it returns tool calls, run them and append observations, then call again
(cap `MaxToolIters`); the final answer streams live, tool activity shows dimmed →
`Store.Remember` user + final answer → **reflect** (extractor distils durable
facts into `KindFact`; a restatement consolidates onto the existing row). Since
LAYER 18 reflection is **async**: the turn `enqueueReflect`s the user message and the
stream closes immediately; a single background worker (`reflectWorker`, started in
`New`) distils and stores off the critical path. `Agent.Close()` drains the queue on
shutdown; `Agent.Quiesce(ctx)` waits for it (tests). Before
each tool runs, `runTool` wraps it as a one-step `plan.Plan` and asks
`Config.Policy`: a **deny** becomes an observation (fail closed); an allowed but
risky step (`RiskLevel ≥ medium`) pauses the loop — the agent emits an
`llm.ApprovalRequest` and blocks on `Decision`, TUI/REPL prompt y/n (deny →
observation, fail closed). The default `policy.ToolGatePolicy` derives that from
each tool's own `Approvable`/`ApprovableFor`, so behaviour matches pre-policy.

With `Config.Planner` set (opt-in), `Turn` instead runs `runPlanned`: the planner
emits a validated `plan.Plan` up front; the policy pre-screens it (a denied step
blocks the whole plan); the human approves the whole plan (per `ApprovalMode`); then
`reactLoop` executes it **capped to the plan's tools** (`execCtx.allowTools`), so the
model can only act within what was approved. The cap is by tool *name*, so
`execCtx.reapproveAtOrAbove` still re-prompts (with the **live** arguments) for steps
at/above a risk level — `RiskHigh` in `plan` mode (shell re-confirms), `RiskLow` in
`step`/`highrisk` (every risky call). A planning failure falls back to the plain
loop. Planning is off by default — the ReAct path above is unchanged.

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

- **`ci.yml`** (push/PR to main): `make deps` + **`make release-check`** (gofmt + vet
  + tests + the drift guards: atlas/readme/lessons + checksums) + **`go test -race`**
  (cgo; caches `ext/`; `fetch-depth: 0` so `lessons-check` sees the pinned tags). Since
  v0.13.3 CI enforces the same guards as a local pre-tag run — a PR that breaks gofmt
  or lets the docs drift now fails CI. **`cve-trivy-scan.yml`** (main + weekly): builds
  the image, Trivy scan, fails on fixable HIGH/CRITICAL.
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
| `TALUNOR_MODEL_CONFIDENCE` | `[0,1]` calibration scaling for learned-fact confidence (Layer 16); `0`→`1.0` | `1.0` |
| `TALUNOR_RECALL_MIN_CONFIDENCE` | drop recalled memories below this confidence (`0`=off) | `0` |
| `TALUNOR_SALIENCE_HALFLIFE` | Layer 17 decay half-life for un-recalled memories (Go duration) | `720h` (30d) |
| `TALUNOR_FORGET_FLOOR` | effective salience below which a memory is soft-forgotten from recall | `0.05` |
| `TALUNOR_SUPERSEDE_MAX_DISTANCE` | Layer 21 cosine radius the arbiter searches for a contradiction candidate (wider than dedup) | `0.35` |
| `TALUNOR_RECALL` | Layer 22 retrieval mode: `hybrid` (vector ∪ FTS5/BM25) or `vector` (lexical arm off) | `hybrid` |
| `TALUNOR_TOOLS` | `0` disables tools (model without tool-calling support) | `1` |
| `TALUNOR_POLICY` | path to a YAML rule file gating tool calls (allow/prompt/deny; `docs/policy.sample.yaml`); unset = default per-tool gate | — |
| `TALUNOR_PLANNER` | `1` plans before acting (inspectable, approved plan → ReAct execution capped to the plan's tools) | `0` |
| `TALUNOR_APPROVAL` | plan approval mode: `plan` / `step` / `highrisk` (ignored when planner off) | `plan` |
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
| `CALIBRATION_KEY` | passphrase to decrypt / `calibrate encrypt` a private calibration suite (Layer 14) | — |
| `TALUNOR_REQUIRE` | **tests only** (`internal/testenv`): capabilities this host must exercise — `ext`,`sandbox`,`docker`,`fts5`,`all`. A missing one FAILS instead of skipping. NOT read from `.env` (`go test` doesn't load it) — export it | — |

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

### SQLite driver build tags
6b. **FTS5 is not in the default driver build.** `mattn/go-sqlite3` compiles SQLite
    itself, and FTS5 only under **`-tags sqlite_fts5`** (the default build has FTS3/4,
    no `fts5` module, no `bm25()`). Hybrid recall's lexical arm needs it, so the tag is
    in the Makefile (`GOTAGS`), the Dockerfile, `release.yml` and CI. A build without it
    still runs — `Store.Lexical()` reports `unavailable` and recall is vector-only. When
    adding a `go` command anywhere, pass `$(GOFLAGS_TAGS)`: **the driver's build tags are
    part of the feature contract, as load-bearing as the schema.**

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
    package, so the `init` is present). **The env var is only the trigger** (v0.20.2):
    `childMain` calls `verifyChildIdentity` first — pid must be 1 of the new pidns
    AND a per-run 128-bit token must arrive both in `TALUNOR_SANDBOX_TOKEN` and on
    the pipe inherited as fd 3, else exit 127 before any mount. When touching that
    plumbing remember `exec.Cmd` dups `ExtraFiles` **at `Start`**: close the parent's
    read end after `Run` (defer), the write end before it (so the child sees EOF).
12. **A green `go test ./internal/sandbox/` may mean nothing.** Every real
    namespaces test skips when unprivileged userns are unavailable, and Ubuntu
    re-applies `kernel.apparmor_restrict_unprivileged_userns=1` across updates. Check
    the sysctl (gotcha 14) before believing the backend still works; the guard tests
    are deliberately written to need neither userns nor root.
13. **Rootless breaks the obvious limits.** RLIMIT_NPROC is per-host-uid (would
    throttle the user's own processes) and rootless cgroup delegation is usually
    absent, so there is **no reliable pids cap**; the memory rlimit + hard timeout
    (killing pid 1 of the pidns cascades) are what actually contain a fork bomb.
14. **Ubuntu 24.04+ AppArmor blocks unprivileged userns.**
    `kernel.apparmor_restrict_unprivileged_userns=1` makes `uid_map` writes fail
    with `EPERM`; `userNSAvailable()` detects it and points at the `sysctl` fix or
    the nerdctl backend. On such hosts the namespaces backend can't run — verify
    it after `sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0`.
15. **No seccomp in the namespaces backend** — it's defense-in-depth/teaching, not
    a boundary for hostile code. Say so; use the OCI runtime for real isolation.

## Testing conventions

- Tests needing the SQLite extensions/model resolve paths relative to the repo
  root and **skip if `ext/` is absent** (so CI without `make deps` is green).
  Copy the `testStore`/`testConfig` helper pattern.
- **Never `t.Skip` a capability directly — go through `internal/testenv`**
  (`testenv.Require(t, testenv.CapExt|CapSandbox|CapDocker, err)`). It skips by
  default, but *fails* on a host that exported `TALUNOR_REQUIRE` (…`=all` on the
  maintainer's machine). Rationale: `go test` prints the same `ok` whether a
  package ran everything or skipped it all, so a host that loses a capability
  silently shrinks the suite — which is how a broken sandbox backend passed a
  green run (v0.20.2 / Lesson 22). `make capabilities` prints host state +
  declaration; `release-check` shows it first.
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
- **v0.10.1 (patch)** = two fixes from a cross-model review: recalled memories now
  fenced + framed as untrusted DATA in `buildMessages` (persistent-prompt-injection
  mitigation); assistant text emitted before a tool-call is carried into the history.
- **v0.10.2 (docs)** = `docs/lessons/` — a hands-on course that turns the tag-by-tag
  history into a guided path for Go beginners (pilot: lessons 00, 01, 05). Historical
  lessons pin to immutable tags (drift-resistant); "read the code at the tag, the
  reference docs on `main`". Guarded by `make lessons-check`.
- **v0.10.3 (docs)** = course substrate lessons 02 (persistent memory, `v0.2.0`),
  03 (semantic recall/embeddings, `v0.2.0`), 04 (LLM provider/streaming, `v0.3.0`).
- **v0.10.4 (docs)** = course contribution/quality lessons 06 (build a tool), 07
  (deterministic tests), 08 (observability/errors) — the first 🛠️ lessons, on `main`.
- **v0.10.5 (docs)** = advanced security lessons 09 (SSRF, `v0.10.0`) + 10 (sandbox,
  `v0.9.0`, capstone). **Course complete: all 11 lessons (00–10).**
- **v0.10.6 (docs)** = French translation begins — bilingual `README.fr.md` next to
  each `README.md` (EN canonical). On-ramp done: index, 00, 01; more per batch.
  Cross-links stay directory-based during rollout; a top-of-page switcher flips language.
- **v0.10.7 (docs)** = French translation batch 2: substrate lessons 02–04. FR coverage
  now 00–04; 05–10 remain.
- **v0.10.8 (docs)** = French translation batch 3: loop + contribution lessons 05–08.
  FR coverage now 00–08; only advanced 09–10 remain.
- **v0.10.9 (docs)** = French translation complete (09–10). **The course is fully
  bilingual EN/FR — every lesson + index in both languages.** Keep new lessons bilingual.
- **v0.10.10 (patch)** = `doctor` DX: prints the loaded sqlite-ai / sqlite-vector
  extension versions (`Store.VersionAI` / `Store.VersionVector` → `ai_version()` /
  `vector_version()`), plus two mountain corpus facts + a third recall query. Cheap
  observability on the memory smoke test.
- **Layer 11 (done): v0.11.0** = memory integrity & in-session observability.
  **Embedding provenance** (`internal/memory/provenance.go`): a `meta` side-table stores
  a canary-vector fingerprint of the embedding stack; every `Open` re-embeds the canary
  and sets `ProvenanceOK` / `ProvenanceStale` / `ProvenanceUnknown`. `Store.ReEmbed`
  rewrites all vectors with the current model and re-stamps. `talunor --reembed` runs it;
  the app warns at startup (and in `/mem`) when provenance ≠ OK. **`/debug [on|off]`**
  (`internal/agent/debug.go`): runtime toggle streaming recall rankings + reflection
  results inline as dimmed `Reasoning` notes (TUI + `--plain`), complementing the
  file/stderr `TALUNOR_DEBUG` trace. Motivated by a real "agent forgot who I am" hunt —
  old memories embedded by a since-changed model build sat in a stale vector space.
- **v0.11.1 (docs)** = course **Lesson 11** — "When memory silently forgets: embedding
  provenance & observability" (`docs/lessons/11-when-memory-forgets/`, bilingual EN/FR).
  First lesson drawn from a real fixed bug (pinned to `v0.11.0`); course now 00–11.
- **Iteration 3 STARTED — Layer 12 (done): v0.12.0** = the **policy engine**. New
  `internal/plan` (Plan/PlanStep/RiskLevel + Validate; `NewToolCallPlan`) and
  `internal/policy` (Policy interface `Evaluate(ctx,*Plan,PlanStep)→Decision`;
  `AllowAllPolicy`, the default `ToolGatePolicy` delegating to each tool's
  `Approvable`/`ApprovableFor`, and `RuleEnginePolicy` reading YAML rules).
  `agent.runTool` wraps each call as a one-step plan and consults `Config.Policy`
  (deny fails closed; `RiskLevel ≥ medium` prompts; `Modified` may rewrite the
  step); `needsApproval` removed. `TALUNOR_POLICY` = YAML rule path
  (`docs/policy.sample.yaml`); unset ⇒ ToolGate, so v0.11.1 behaviour is preserved
  (the 3 old approval tests pass unchanged). `cmd/talunor` wiring extracted into
  `buildProvider`/`buildTools`/`buildPolicy`/`buildAgentConfig`. First dep outside
  the SQLite/TUI/LLM substrate: `gopkg.in/yaml.v3`.
- **v0.12.1 (docs)** = course **Lesson 12** — "The open bar: why an autonomous agent
  needs a policy" (`docs/lessons/12-the-open-bar/`, bilingual EN/FR). Pinned to
  `v0.12.0`; argues the threat (prompt-injected text → tool call) before reading the
  `Policy`/`Decision` code; course now 00–12. Keep new lessons bilingual.
- **Iteration 3 COMPLETE — Layer 13 (done): v0.13.0** = the **explicit planner**.
  `agent/planner.go` (`Planner` interface + default `llmPlanner`: LLM → JSON plan →
  validate + retry, never runs tools; `NewLLMPlanner`, opt-in via `TALUNOR_PLANNER`).
  `agent/execute.go` (`runPlanned`: plan → policy pre-screen → whole-plan approval →
  `reactLoop` **capped to the plan's tools** → learn; `FormatPlan`; `/plan` command).
  `runLoop` split into `runLoop` (plain entry) + `reactLoop` (shared core); `runTool`
  + core take `execCtx{allowTools, skipStepApproval}`; `toolSpecs(allow)` enforces the
  cap. `Config.Planner` + `Config.ApprovalMode` (`TALUNOR_APPROVAL` = plan|step|
  highrisk, default plan). A planning failure falls back to the plain ReAct loop.
  **Deferred (future layers/lessons):** `/edit-plan`, semantic deviation detection,
  automatic re-planning — the v0.13.0 cap is *structural* (only planned tools offered).
- **v0.13.1 (docs)** = course **Lesson 13** — "Plan before you act: from emergent
  ReAct to a plan you can read" (`docs/lessons/13-plan-before-you-act/`, bilingual
  EN/FR). Pinned to `v0.13.0`; contrasts emergent vs deliberate execution, reads the
  structured-output discipline in `planner.go` and the capped execution in
  `execute.go`; course now 00–13. Keep new lessons bilingual.
- **v0.13.2 (fix + docs)** = **plan-mode approval integrity** (P1 from a cross-model
  review): the whole-plan approval bound tool *names* but not the *arguments* the
  ReAct executor ran, so `plan` mode could execute a different command than the one
  approved. `execCtx.skipStepApproval` → `reapproveAtOrAbove plan.RiskLevel`;
  high-risk steps re-confirm live args in `plan` mode (regression tests added). Ships
  with course **Lesson 14** (post-mortem, bilingual; course now 00–14).
- **v0.13.3 (fixes)** = convergent cross-model-review batch: DB dir `0700` + file
  `0600` (personal-data privacy); `ReEmbed` made atomic (transaction, no mixed vector
  spaces on failure); silent assistant-store errors now traced (`store.assistant.error`
  + `/debug`); the planner now receives the recalled memories (`fencedMemories`, shared
  with `buildMessages`); `plan.Validate` now rejects `DependsOn` cycles (DFS) and the
  stale "deferred to executor" comment is corrected; **CI runs `make release-check` +
  `go test -race`** (`fetch-depth: 0`). Still open: the `lastPlan`/`screenDebug`
  cross-goroutine access (narrow, untested by the suite).
- **v0.13.4 (docs)** = course **Lesson 15** — "Don't trust the review: verifying what
  an AI claims about your code" (`docs/lessons/15-dont-trust-the-review/`, bilingual).
  The course's meta-lesson: a hands-on verification exercise (falsify five claims from a
  real, anonymised AI review against the repo's own gotchas). Model-agnostic; course now
  00–15.
- **Layer 14 (done): v0.14.0** = **model calibration**, a preliminary layer before
  Iteration 4 (motivated by the Lesson 15 review episode: measure a model before you
  let an agent *learn* from it). `internal/calibration` (deterministic-only harness:
  YAML suite, source-agnostic Parse, matchers with no LLM judge, Run→pass-rate +
  latency stddev, Baseline+Diff drift detection, optional AES-256-GCM/`CALIBRATION_KEY`)
  + `cmd/calibrate` (run / save-baseline / diff → exit 1 on regression; `encrypt`
  subcommand) + `docs/calibration.seed.yaml` (public example, threat-model header).
  Also: Lesson 15 gained a model-agnostic "naming the defects" aside (EN/FR).
  **Deferred:** wiring calibration into the policy (route a low-calibration model away
  from high-risk steps).
- **v0.14.1 (docs)** = course **Lesson 16** — "Measure the model: building a reliability
  canary" (`docs/lessons/16-measure-the-model/`, bilingual). Reads `internal/calibration`
  to teach the three design decisions of a trustworthy LLM eval (deterministic verifier,
  accuracy vs consistency, drift over absolute); closes the 11→15→16 trust-and-verify arc.
  Course now 00–16.
- **Iteration 4 STARTED — Layer 15 (done): v0.15.0** = **schema versioning &
  migrations** (`internal/memory/migrate.go`): an ordered append-only migration runner,
  `schema_version` int in the `meta` table, migration 1 = baseline (the memories table),
  pre-versioning DBs baselined automatically (no data loss). `Store.SchemaVersion()` +
  a `schema version:` line in doctor. **Zero behaviour change** — the seam every later
  learning layer adds its columns through. Add a migration by APPENDING to `migrations`
  (never reorder/renumber/edit a shipped one).
- **Layer 16 (done): v0.16.0** = fact **provenance + confidence** (migration 2 adds the
  columns). `memory.Provenance` + `BaseConfidence`; `RememberFact(content,prov,conf)`;
  `Remember` derives a turn's provenance from role; `Recall`/`List`/`Hit`/`Memory` carry
  both. **Calibration link:** `Config.ModelConfidence` (`TALUNOR_MODEL_CONFIDENCE`, from a
  `calibrate` run) scales a learned fact's confidence — decoupled, the agent consumes a
  number. `Config.RecallMinConfidence` (`TALUNOR_RECALL_MIN_CONFIDENCE`) filters recall.
  Confidence is system-assigned from the source, NEVER model-self-reported (sycophancy
  trap). `/list` shows a fact's provenance/confidence; `/debug` recall trace too.
- **v0.16.1 (docs)** = course **Lesson 17** — "Learning with humility: what a memory is
  worth" (`docs/lessons/17-learning-with-humility/`, bilingual). The first *learning*
  lesson: provenance + confidence, confidence-from-source-not-self-report, the calibration
  link; reads migration 2 (folds in the un-lessoned Layer 15). Course now 00–17.
- **Layer 17 (done): v0.17.0** = **salience / decay / consolidation** (the retention half
  of learning). Migration 3 adds `salience`/`last_accessed`/`access_count`. `salience.go`:
  decay is LAZY — `Recall` computes effective salience `= salience·2^(−age/half-life)` at
  read time (no writes → fits the pinned single conn), RANKS the relevant neighbourhood by
  `similarity·confidence·eff-salience`, and SOFT-FORGETS below `ForgetFloor` (row survives,
  a restatement revives it). Reinforcement is EXPLICIT: `Reinforce(ids)` (recall mattered →
  salience only; `agent.reinforceRecalled` after each turn's recall); `ReinforceFact(id,gain)`
  also raises confidence toward a <1 ceiling with diminishing returns. `reflect` now
  CONSOLIDATES a restated fact (`knownFact`→`ReinforceFact`) instead of skipping it. The
  honesty rule holds: salience rises on any repetition, confidence only on INDEPENDENT
  evidence (`EvidenceCredibility`: user/tool=1, model_inferred=0 — the echo-chamber guard);
  gain also folds in `ModelConfidence`. Knobs: `TALUNOR_SALIENCE_HALFLIFE` (30d),
  `TALUNOR_FORGET_FLOOR` (0.05). `/debug` + `/list` show salience/score; doctor → schema 3.
- **v0.17.1 (docs)** = course **Lesson 18** — "The memory of the gesture: salience, decay &
  consolidation" (`docs/lessons/18-the-memory-of-the-gesture/`, bilingual). Pinned to
  `v0.17.0`; reads `salience.go` + `Recall` + `agent.reflect` to teach salience as a third
  axis, LAZY decay as the design that respects `SetMaxOpenConns(1)`, soft forgetting, and
  consolidation + the independence rule (confidence only on independent evidence). Framed
  through the `/compact` parallel (working- vs long-term-memory consolidation). Course now 00–18.
- **Layer 18 (done): v0.18.0** = **async reflection** (learning off the turn's critical
  path). `agent.reflect` no longer runs inline; `reactLoop`/`runPlanned` call
  `enqueueReflect` → a bounded `reflectCh` (cap 8) → a single `reflectWorker` goroutine
  started in `New`, processing jobs in order. **Key insight:** one worker + the pinned
  single connection means `database/sql` serialises reflection's writes against a turn's
  reads for free — no extra locking (`go test -race` clean). `Agent.Close()` drains the
  queue on shutdown (deferred before `store.Close()`); `Agent.Quiesce(ctx)` waits for it
  (tests). `reflect` lost its stream param — its `/debug` notes now go to the log (async
  work can't narrate a closed turn); the recall trace stays inline. Closes Iteration 4's
  arc (schema → trust → retention → *when*).
- **v0.18.1 (docs)** = course **Lesson 19** — "Off the critical path: learning in the
  background" (`docs/lessons/19-off-the-critical-path/`, bilingual). Pinned to `v0.18.0`;
  reads `internal/agent` to teach async learning, the **single-connection-as-lock** insight
  (no extra mutex needed; the worker is for backpressure/ordering/drain, not safety), the
  shutdown-drain contract (`Close` + deferred-LIFO vs `store.Close`), and "async work can't
  narrate a closed turn" (why /debug reflection notes moved to the log). **Course now 00–19
  (twenty lessons); closes the Iteration-4 arc in the course.**
- **v0.18.2 (fixes)** = correctness & hardening patch from a five-model cross-review, each
  finding verified against the code. `llm.Options.Temperature` is now `*float64` (+`llm.Temp`)
  so an explicit `0` is actually sent — planner/reflection/calibration were silently getting
  the provider default via `omitempty`. `Store.RecallForConsolidation` lets reflection see
  soft-forgotten rows, so a restatement **revives** the old fact instead of duplicating it
  (matches the Layer-17 promise). `Agent.Close()` bounds its drain (`closeDrainTimeout`, then
  `bgCancel`) so an unresponsive provider can't wedge shutdown. TUI approval lets scroll keys
  through (only explicit keys decide). OCI sandbox now passes `--cap-drop=ALL
  --security-opt=no-new-privileges --user 65534:65534` (matches the doc's promised posture).
  SSRF `blockedIP` blocks `0.0.0.0/8` and decodes NAT64/6to4/Teredo IPv6→IPv4. Debug log is
  `0600`. `docs/policy.sample.yaml` `clock`→`current_time`. No schema/behaviour change beyond
  these; regression tests added throughout.
- **v0.18.3 (fix)** = concurrency patch closing the **last documented data race**: `lastPlan`
  → `atomic.Pointer[plan.Plan]`, `screenDebug` → `atomic.Bool` (written on the turn goroutine,
  read on the UI goroutine, or vice-versa; no lock, no API change). `TestConcurrentStateAccessIsRaceFree`
  drives both from two goroutines, clean under `-race`. **Lesson:** `-race` only finds what the
  tests exercise — a green race detector over a documented race just means the suite is sequential.
- **v0.18.4 (docs)** = the mental-model page + course competency matrix (the two most-requested
  pedagogy items from the v0.18.x reviews). **`docs/architecture.md`** (bilingual EN/FR): a
  one-turn-of-the-loop Mermaid diagram, the package DAG (from the real import graph), and the six
  load-bearing decisions, each linked to its lesson — positioned as the mental model (atlas = file
  map, lessons = the why). **Competency matrix** in `docs/lessons/README.md` (+ `.fr.md`), before
  the route table: 8 competencies × lessons × level × how-to-prove-it. Also folds in the untagged
  post-0.18.3 doc fixes: FR lesson links now point to `README.fr.md` (were resolving to EN),
  README "18-lesson"→"20-lesson", and **`lessons-check` extended to guard language-suffixed links**
  (`.md`/`.fr.md`/`.es.md` — the `.es.md` arm readies a future Spanish rollout). No code change.
- **Iteration 5 STARTED — Layer 20 (done): v0.19.0** = **learn from action + evidence trail**.
  `agent.reflect` no longer learns only from the user's message: `reflect(job)` loops over the
  turn's SOURCES via `learnFrom(text, prov, turnID)` — the user message (`user_stated`), each tool
  observation, and (opt-in `Config.ReflectAssistant`, off by default) the assistant answer. A fact
  distilled from a tool's text is **`model_inferred`** (an LLM interpreting text is inference);
  **`tool_observed`** is reserved for a `tools.Verified` tool (new optional capability) — none
  ship today, so it's a wired/tested seam. Provenance is assigned **per source by the system**
  (sources extracted separately — never one call asking the model to label itself; preserves the
  Layer-16 invariant). `internal/memory/evidence.go` (**migration 4**): an `evidence` table +
  `RecordEvidence`/`EvidenceFor`/`MemoryByID`, one row per store/reinforce; **`/why <id>`** shows
  it. Trivial/empty observations skipped + size-capped; rides the single `TALUNOR_REFLECT` toggle +
  the async worker. Append-only, no re-embed, zero change to existing facts. Decision recorded in
  **ADR 0002**. Deferred to later Iteration-5 layers: contradiction/supersession (21), hybrid recall (22).
- **v0.19.1 (docs)** = course **Lesson 20** — "Learn from action: most 'tool knowledge' is
  model-interpreted" (`docs/lessons/20-learn-from-action/`, bilingual). Pinned to `v0.19.0`; reads
  `internal/agent`/`memory`/`tools` to teach the honesty chain (confidence system-assigned → so is
  provenance → per-source extraction), the `model_inferred`-by-default rule + the `tools.Verified`
  seam, the evidence trail + `/why`. Competency matrix gains lesson 20; course now 00–20 (21 lessons).
- **Layer 21 (done): v0.20.0** = **contradiction & supersession** — a memory that corrects itself.
  `agent/arbiter.go` (`FactArbiter`: classify a new fact vs a near neighbour as restates/supersedes/
  unrelated; default LLM, `DisableArbiter()`→L20) + `memory/supersede.go` (the **trust model**
  `Supersedes(newer,older)` — the ONE named function for "who may retire whom"; default = user &
  Verified-tool authoritative, model_inferred retires nothing; + `Store.Supersede` soft-marking
  `superseded_by`, migration 5, excluded from recall). `learnOneFact` PROPOSES via the arbiter then
  GATES via the trust model: a model inference is dropped (never overwrites the user); a Verified
  tool can retire a stale belief. Two worked examples in **ADR 0003** (flat earth → belief, UNRELATED;
  attack signature → tool_observed can supersede). `TALUNOR_SUPERSEDE_MAX_DISTANCE` (0.35, wider than
  dedup). `/why` + `/list` show supersession. Append-only, no re-embed. Lesson 21 (v0.20.1) to write.
- **v0.20.1 (docs)** = course **Lesson 21** — "Whose word counts? A trust model is a decision, not a
  default" (`docs/lessons/21-whose-word-counts/`, bilingual). Pinned to `v0.20.0`; a META lesson (like
  L15): the flat-earth vs attack-signature opposing examples → a single global provenance rank breaks
  both ways → authority is per-domain → `memory.Supersedes` is the one place to decide it. Ships a
  reusable "before you build agent memory" checklist + a hands-on that flips `supersedeAuthority` to
  make `TestSupersedeGateProtectsUser` fail. Competency matrix gains lesson 21; course now 00–21 (22 lessons).
- **v0.20.2 (fix)** = **the sandbox re-exec is authenticated, not just triggered**. `init()` in
  `namespaces_linux.go` runs in every binary linking the package, so a stray
  `TALUNOR_SANDBOX_CHILD=1` in an environment turned `talunor` (or any test binary) into a
  container init that exited 127 before `main()` — a self-DoS footgun (it died at the first
  `mount`, EPERM: `pivot_root` on the host was never reachable unprivileged). New
  `verifyChildIdentity(pid, tokenFD, envValue)` gates `childMain` on pid 1 **and** a per-run
  128-bit token delivered twice (env + pipe on fd 3), fstat'd as a FIFO and read bounded.
  Tests need neither userns nor root: an impostor table + a subprocess run of the test binary
  (`-test.run=^$`, so a regression cannot fork-bomb). Closes the `todo.md` "future patch:
  namespaces re-exec guard" item.
- **v0.20.3 (docs)** = course **Lesson 22** — "The silent suite: a skipped test is not a passing
  test" (`docs/lessons/22-the-silent-suite/`, bilingual). Pinned to `v0.20.1` → `v0.20.2`; the
  third post-mortem (after L11, L14) and the first about a **fix**: severity-before-fixing, the
  three defects of the proposed patch (`ExtraFiles` dup'd at `Start`, `"" == ""` authentication,
  `ReadAll` on an un-owned fd) + `t.Skip` vs `//go:build`, then the real subject — four namespaces
  tests skipping for weeks behind a reverted sysctl, and the design answer (extract the privileged
  decision into a pure function so it stays testable). Competency matrix: evaluation/verification
  becomes 07 · 15 · 16 · 22; course now 00–22 (23 lessons). **NOTE:** lesson numbering is
  release-ordered, so course Lesson 22 is NOT Layer 22 (hybrid recall) — that layer's lesson will
  be 23.
- **v0.20.4 (fix/tooling)** = **the capability contract** — Lesson 22's principle put into
  the build. `internal/testenv`: `Require(t, cap, err)` replaces every direct capability
  `t.Skip` (ext / sandbox / docker); it still skips by default but **fails** when the host
  exported `TALUNOR_REQUIRE` (`=all` on a full dev machine) — so a silently reverted sysctl
  stops a release instead of shrinking the suite. `make capabilities` (now first in
  `release-check`) prints host state + declaration. **`TALUNOR_REQUIRE` is NOT read from
  `.env`** — `go test` doesn't load it; export it from the shell. Also fixed env-doc drift:
  `TALUNOR_SUPERSEDE_MAX_DISTANCE` was missing from the README table (all 31 user-facing vars
  now in README + AGENTS + `.env_sample`; the 7 internal sandbox handshake vars in none).
- **v0.20.5 (docs)** = **README restructured for a first-time reader** (no code). Order is now
  intro (+ the course callout, moved out of the version banner) → merged Philosophy/Why →
  Demo (+ a link to `images/demo_transcript.md`, previously referenced nowhere) → Requirements
  → Quickstart → **`## Using Talunor`** (new parent for the commands/providers/tools/`bash`/
  `web_fetch`/memory/env `###` sections, which the Quickstart move had orphaned) → What's new →
  Architecture → Status (**tables folded into `<details>`**). **Keep the banner to the current
  release** — it had accreted five versions of history; the archive is CHANGELOG.md.
- **Layer 22 (done): v0.21.0** = **hybrid recall (vector ∪ FTS5/BM25)** — Iteration 5 continues.
  `internal/memory/lexical.go` (FTS5 external-content index + triggers, created at Open, NOT a
  migration — it is derived data that a build without the tag cannot honour; `matchExpression`
  sanitises text into a quoted OR-expression; `LexicalStatus` ok/unavailable/disabled) +
  `hybrid.go` (**reciprocal rank fusion**, rrfK=60 — cosine distance and BM25 share no scale, so
  fuse the ORDERS; with a single arm the Layer-17 score is kept verbatim so vector-only builds
  rank exactly as before). `Hit.VectorRank/LexicalRank/BM25` + `HasVector()/FromLexical()`;
  `/debug` shows `v#1 d=0.23 l#2`, `/mem` + doctor show the mode. **Boundary that cost a
  regression: `RecallForConsolidation` stays VECTOR-ONLY** — retrieval is hybrid, IDENTITY is
  metric. Needs **`-tags sqlite_fts5`** (Makefile GOTAGS + Dockerfile + release.yml + CI, which
  now also sets `TALUNOR_REQUIRE=ext,fts5`). Knob `TALUNOR_RECALL=hybrid|vector`. Lesson 23 to write.
- **v0.21.1 (docs)** = course **Lesson 23** — "Two ways to find a memory: when meaning is the
  wrong index" (`docs/lessons/23-two-ways-to-find-a-memory/`, bilingual). Pinned to `v0.21.0`;
  opens with the reader reproducing the gap via `TALUNOR_RECALL=vector`, then FTS5/BM25, the
  stopword incident, RRF + its single-arm trap, retrieval-vs-identity, and build-tag capability.
  Competency matrix: persistence/retrieval becomes 02 · 03 · 23; course now 00–23 (24 lessons).
  **Iteration 5 is fully built AND fully lessoned.**
- **Next — open threads (documented, not started):** calibration→policy wiring;
  the executed plan as a learning source. Same per-layer checkpoint rhythm.

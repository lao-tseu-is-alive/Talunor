# Changelog

All notable changes to Talunor are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Talunor uses a `0.MINOR.PATCH` scheme where **each completed build layer bumps
`MINOR`**. Iteration 1 (a conversational agent with memory) completes at `0.5.0`.

This changelog doubles as a teaching log: each version records not just *what*
changed but the *lessons learned* while getting there.

## [Unreleased]

- **Layer 10, next** — a `web_fetch` tool that opts *into* a restricted network
  (allowlist + timeout), reusing the sandbox / approval machinery.
- **Iteration 3, later** — an explicit planner before multi-step actions; policy
  checks for which tools/args are auto-allowed vs. need approval.

## [0.9.0] - 2026-07-15 — Sandboxed `bash`: a tool that can run anything, safely

The agent gets its most powerful tool — a real shell — and the machinery to run
it without handing it the host. `bash` is **off by default** (`TALUNOR_BASH=1`),
**approval-gated** (every call pauses for a human y/N, reusing the v0.8.0 gate),
and runs inside a **network-less, throwaway sandbox**. This completes Iteration 2.

### Added

- **`internal/sandbox`** — a `Sandbox` interface (`Run(ctx, script, Limits)`)
  with two pluggable backends selected by `TALUNOR_SANDBOX` (auto-detected when
  unset). A non-zero exit is returned as *output*, not a Go error; only
  infrastructure failures error. Output is capped at 16 KiB.
  - **`nerdctl` backend (the strong one).** Shells out to `nerdctl`/`docker` with
    `--network none --read-only --pids-limit --memory --tmpfs /tmp:size=… --cpus=1`
    and a container-side `timeout`. Delegating to an OCI runtime buys seccomp,
    cgroups, and dropped capabilities for free.
  - **`namespaces` backend (the teaching one).** A from-scratch, **rootless**
    sandbox: re-execs Talunor's own binary as a container init in fresh
    user/mount/pid/uts/net/ipc namespaces, `pivot_root`s into a cached busybox
    rootfs (bind-mounted read-only), mounts a private `/proc`, a size-capped
    `/tmp`, and a minimal `/dev`, then sets `no_new_privs`, drops **all**
    capabilities, and applies rlimits (`AS`, `CPU`, `FSIZE`, `NOFILE`). An empty
    net namespace = no network. Linux-only; needs unprivileged user namespaces.
- **`tools.Bash`** — the tool: schema `{command}`, `RequiresApproval() → true`,
  runs the script through the sandbox and returns combined stdout+stderr. Wired
  in `cmd/talunor` behind `TALUNOR_BASH`; if the sandbox can't initialise the
  tool is skipped with a warning rather than crashing the app.
- **Env**: `TALUNOR_BASH`, `TALUNOR_SANDBOX`, `TALUNOR_SANDBOX_IMAGE`,
  `TALUNOR_SANDBOX_ROOTFS`, `TALUNOR_SANDBOX_BUSYBOX` — documented in
  `.env_sample` and the README env table.

### Lessons learned

1. **Isolation is a spectrum, and honesty about where you sit on it is the
   feature.** The `namespaces` backend *looks* like a container, but without a
   seccomp filter the entire syscall surface is reachable — it is defense in
   depth and a teaching artifact, not a boundary for hostile code. Building both
   backends makes the trade-off concrete: reach for the OCI runtime when it
   matters, keep the hand-rolled one to understand *what a runtime actually does*.
2. **Rootless changes which knobs work.** RLIMIT_NPROC is per-host-uid, so using
   it to cap processes would throttle the user's own shell; rootless cgroup
   delegation is usually absent too. The honest answer for a fork bomb is the
   memory cap plus the hard timeout (killing pid 1 of the pid namespace cascades
   to everything), and saying so rather than pretending pids are capped.
3. **The host fights you, and the error message is the UX.** Ubuntu 24.04+ gates
   unprivileged user namespaces behind AppArmor
   (`kernel.apparmor_restrict_unprivileged_userns=1`), so `uid_map` writes fail
   with a bare `EPERM`. Detecting that and printing the exact `sysctl` to fix it
   (or "use `nerdctl`") turns a baffling failure into a one-line decision.
4. **No network is a *default*, not a fetch you forgot to write.** An empty net
   namespace (or `--network none`) means the sandbox can't reach `localhost:11434`
   Ollama or anything else — the safe posture is the absence of a capability, and
   networking becomes a later, explicit opt-in (`web_fetch`).
5. **Build the brake before the engine.** The v0.8.0 approval gate existed first,
   so the first genuinely dangerous tool slotted behind it for free — the guard
   was never retrofitted onto a running risk.

## [0.8.0] - 2026-07-15 — Approval gate: human-in-the-loop for tools

An early piece of Iteration 3's guardrails, brought forward: a tool can now
require explicit human approval before each call. This is the safety
prerequisite for giving the agent side-effecting tools (next: a sandboxed
`bash`).

### Added

- **`tools.Approvable`** — an optional interface (`RequiresApproval() bool`) a
  tool implements to be gated. Tools that don't implement it (calculator, clock,
  memory search) keep running freely.
- **Approval in the ReAct loop.** When about to run a gated tool, `agent.runLoop`
  emits an `llm.ApprovalRequest` on the chunk stream and **blocks on
  `Decision`**; the front-end prompts the user and calls `Respond`. Threading it
  through the existing stream means both front-ends handle it uniformly:
  - **TUI** — a yellow y/n prompt pauses the stream; any key that isn't `y`
    denies; the stream resumes on the answer.
  - **REPL** — `render.StreamWithApproval` + an `ApproveFunc` that asks on stdin.
- **Fail closed.** A denial, an unanswered request on a cancelled turn, or a
  missing approver all deny; a denial is fed back to the model as an
  `error: the user denied…` observation so it can adapt rather than crash.

### Changed

- `render.Stream` now delegates to `StreamWithApproval(…, nil)` (deny-by-default),
  so tool-less callers (`cmd/chat`) are unaffected.

### Lessons learned

1. **Autonomy needs a brake before it needs more tools.** The ReAct loop happily
   auto-runs whatever the model asks; that's fine for a calculator and unsafe for
   anything with side effects. Building the approval gate *before* the first
   dangerous tool means the guardrail is never retrofitted onto a running risk.
2. **Reuse the transport you already have.** Emitting the approval request as a
   `Chunk` on the existing reply stream (with a reply channel inside it) let one
   mechanism serve both the TUI event loop and the blocking REPL — no separate
   callback plumbing, no new channel between agent and front-end.
3. **Fail closed, and turn refusal into information.** Denying by default on every
   ambiguous path (cancel, nil approver) is the safe bias; feeding the denial back
   as an observation keeps the agent useful (it can explain or try another way)
   instead of aborting the turn.

## [0.7.0] - 2026-07-15 — Tools & actions: the ReAct act/observe loop

## [0.7.0] - 2026-07-15 — Tools & actions: the ReAct act/observe loop

Talunor can now *do* things, not just talk. It runs a ReAct-style
act→observe→reason loop using **native tool-calling**: the model asks to call a
tool, the agent runs it and feeds the result back, and this repeats until the
model answers. Completes the core of Iteration 2.

### Added

- **`internal/tools`** — the action layer: a `Tool` interface (name,
  description, JSON-Schema args, `Execute`) and a concurrency-safe `Registry`
  that offers tool definitions to the LLM and routes calls, turning a missing
  tool or an execution error into an *observation* string so the loop recovers
  instead of crashing. Starter tools:
  - **`calculator`** — a dependency-free, safe evaluator: it parses the
    expression to a Go AST and walks only numbers, parentheses, unary ±, and
    `+ - * /`, rejecting anything else (no code is executed); whole results print
    as integers.
  - **`current_time`** — current time, optional IANA timezone.
  - **`recall_memory`** — searches Talunor's own long-term memory, turning
    retrieval into an on-demand action the model can invoke.
- **Native tool-calling in the adapter** (`internal/llm`) — requests carry the
  offered `tools`; the streaming parser accumulates fragmented `tool_calls`
  (id/name once, arguments concatenated) and emits them as one terminal chunk.
  `Message` gained `ToolCalls` / `ToolCallID`, `Chunk` gained `ToolCalls`,
  `Options` gained `Tools`; `ToolCall` marshals to OpenAI's function shape for
  the follow-up message.
- **The agent act/observe loop** (`agent.runLoop`) — offers the registry's tools
  each turn; while the model returns tool calls it executes them, appends the
  observations, and calls again (capped by `MaxToolIters`, default 6); the final
  answer streams live while tool activity is surfaced as dimmed notes
  (`🔧 tool(args)` / `↳ result`). Only the final answer is persisted; tool
  messages are ephemeral scratch. Enabled via `Config.Tools`; wired in
  `cmd/talunor` and toggled with `TALUNOR_TOOLS=0`.

### Changed

- The conversational turn is now a special case of the loop (zero tool calls →
  answer immediately), so `learnWhileStreaming` is replaced by `runLoop`.

### Lessons learned

1. **The act/observe loop is just "call, maybe run tools, repeat".** Wrapping the
   existing single-shot turn in a loop that stops when the model *doesn't* ask
   for a tool keeps plain chat unchanged and adds acting for free — the ReAct
   pattern is a control-flow shape, not a new subsystem.
2. **Streaming and tool-calling coexist cleanly because tool steps carry no
   answer text.** Content streams to the user live; tool-call fragments are
   accumulated silently and only acted on at end-of-step, so nothing half-formed
   is ever shown.
3. **Make tool failure an observation, not an exception.** Returning
   `error: …` as the tool result lets the model see and recover from a bad call
   (wrong args, unknown tool) instead of aborting the turn — robustness the agent
   gets for free.
4. **Evaluate untrusted input structurally, never by execution.** The calculator
   parses to an AST and walks only arithmetic nodes; there is no `eval`, so a
   crafted "expression" can compute but never *run* anything.

## [0.6.0] - 2026-07-15 — Iteration 2 begins: providers & config

## [0.6.0] - 2026-07-15 — Iteration 2 begins: providers & config

The first layer of Iteration 2. Talunor can now talk to **hosted frontier
models via OpenRouter**, not just local Ollama, and all configuration is
discoverable through a `.env` file. This unblocks running the upcoming
tool/ReAct loop on a strong tool-calling model.

### Added

- **OpenRouter provider.** `llm.NewOpenRouter(model, key)` reuses the existing
  OpenAI-compatible adapter (OpenRouter speaks the same API) with the right base
  URL, bearer auth, and OpenRouter's optional attribution headers. One adapter
  now serves Ollama **and** OpenRouter — only URL/key/headers differ.
- **Provider selection from the environment.** `llm.FromEnv()` builds the chat
  provider from `TALUNOR_PROVIDER` (`ollama` default, or `openrouter`), reading
  `TALUNOR_MODEL`, `TALUNOR_OLLAMA_URL`, `OPENROUTER_API_KEY`,
  `TALUNOR_OPENROUTER_URL`. Both `cmd/talunor` and `cmd/chat` use it (no more
  duplicated wiring), and a missing OpenRouter key fails fast with a clear error.
- **`.env` support.** A minimal, dependency-free loader (`internal/config`)
  auto-loads `.env` from the working directory at startup; **real environment
  variables always win** over the file. Ships with **`.env_sample`** documenting
  every supported variable.
- **`TALUNOR_REFLECT=0`** disables the reflection step — a second model call per
  turn that, on a paid provider, doubles cost.

### Changed

- `cmd/talunor` / `cmd/chat` now select the provider via `llm.FromEnv()` and load
  `.env` first; the inline Ollama-only setup and duplicated `envOr` helpers are
  gone.

### Lessons learned

1. **A good adapter boundary pays forward.** Because Layer 3 modelled the
   provider as "anything speaking the OpenAI streaming API", adding OpenRouter was
   a constructor and a header map — no new transport, no new parsing. The cost of
   the right abstraction is paid once.
2. **Configuration should be discoverable and layered.** `.env_sample` turns a
   pile of `TALUNOR_*` variables into self-documenting onboarding; letting the
   real environment override the file keeps it safe for secrets and CI.
3. **Make expensive behaviour a switch.** Reflection is great with a local model
   and costs nothing; on a metered API it silently doubles spend. Surfacing
   `TALUNOR_REFLECT` makes the trade-off the user's to make.

## [0.5.7] - 2026-07-15 — Harden the image: distroless base + dependency bumps

## [0.5.7] - 2026-07-15 — Harden the image: distroless base + dependency bumps

A security follow-up to 0.5.6, prompted by reviewing the image's CVE scan. No
application behaviour changed.

### Changed

- **Runtime base is now `gcr.io/distroless/cc-debian12`** (was
  `debian:trixie-slim`). Distroless/cc contains only glibc, libstdc++, libgcc and
  ca-certificates — exactly what the Go binary and `ai.so` need — with no shell,
  apt, perl or util-linux. A full Trivy scan drops from **166 CVEs (3 CRITICAL,
  18 HIGH)** to **17 (0 CRITICAL, 0 HIGH, 4 MEDIUM, 13 LOW)**; the fixable
  HIGH/CRITICAL gate stays at 0. The builder moves to `golang:1.26-bookworm` to
  match the runtime's glibc (2.36), which the extensions satisfy — they require at
  most `GLIBC_2.34` / `GLIBCXX_3.4.29` (measured with `objdump -T`), so the
  earlier trixie choice was over-cautious. Verified end to end that the distroless
  image still loads both extensions and the GGUF model (`… --list 1` opens the
  store cleanly).

### Fixed

- **Security:** bumped `golang.org/x/net` v0.55.0 → **v0.56.0** (`CVE-2026-46600`,
  DNS message parse panic) and `golang.org/x/text` v0.37.0 → **v0.39.0**
  (`CVE-2026-56852`, infinite loop on invalid input) — both flagged in the
  `gobinary` after 0.5.6 as the Trivy DB updated. The binary now scans clean.

### Lessons learned

1. **A CVE *count* is not a CVE *risk*.** Most of the 166 were `affected` /
   `fix_deferred` distro triage with no available patch — which is why the
   `ignore-unfixed` gate was already green. The real lever is **shrinking the base
   so those packages aren't present at all**: fewer packages ⇒ less surface *and*
   less noise, even before considering fixability.
2. **"Distroless" is a dependency contract, not magic.** It works only because the
   image's actual runtime needs are known and small — here, the `NEEDED` libraries
   of the binary and `ai.so`. Verify those (`ldd` / `objdump -T`) before choosing
   the smallest base that still satisfies them.
3. **Match the base's glibc to the *oldest* thing that must run on it.** The
   prebuilt native extensions set the floor; measuring their required symbol
   versions turned a guess ("use the newest base to be safe") into a decision
   ("bookworm is provably enough, and more portable").

## [0.5.6] - 2026-07-15 — CI/CD, container image & release bundles

Makes every tagged iteration installable **without a Go/C toolchain or
`make deps`**, so people can try Talunor by pulling an image or a bundle. No
application code changed — this is packaging and supply-chain plumbing.

### Added

- **`Dockerfile`** — a self-contained, multi-stage image. Both stages use Debian
  **trixie** (its newer glibc satisfies both the prebuilt sqliteai extensions and
  the cgo Go binary); the builder runs `make deps` + the cgo build, the
  trixie-slim runtime adds only `libstdc++6` (the single extra library `ai.so`
  needs) and bakes the extensions **and** the embedding model in. Embeddings run
  offline; only chat needs a reachable Ollama. **linux/amd64 only** — sqliteai
  publishes no arm64 extension assets. `.dockerignore` excludes `ext/` so the
  build fetches fresh assets rather than copying a local checkout.
- **GitHub Actions** (`.github/workflows/`):
  - `ci.yml` (push/PR to main) — `make deps` + `go vet` + `go test` under cgo,
    caching `ext/`.
  - `release.yml` (tag `vX.Y.Z`) — builds a linux/amd64 binary and a
    **self-contained bundle** tarball (binary + extensions + model + `run.sh`)
    with a `SHA256.txt`, attached to the GitHub Release.
  - `docker-publish.yml` (tag `vX.Y.Z`) — builds the image, Trivy-scans it,
    **gates on fixable HIGH/CRITICAL**, and pushes
    `ghcr.io/lao-tseu-is-alive/talunor` (`{{version}}` + `sha` tags).
  - `cve-trivy-scan.yml` (main + weekly cron) — builds the image and runs the
    same scan+gate, so CVEs that land against already-shipped images turn the
    build red.
  Third-party actions are pinned to commit SHAs (supply-chain), mirroring
  `go-cloud-k8s-poc-2026`.
- **Makefile** `docker-build`/`docker-run` + `nerdctl-build`/`nerdctl-run` for
  local image use (Rancher Desktop / containerd).
- **README** "Run without building" (image + bundle, Ollama networking, TTY,
  persistence); **AGENTS.md** CI/CD section.

### Fixed

- **Security:** the first CVE scan gated on **4 fixable HIGH** advisories in the
  transitive dependency `golang.org/x/net` v0.38.0 (x/net/html XSS
  CVE-2026-25681 / CVE-2026-27136, HTTP/2 DoS CVE-2026-33814, idna
  CVE-2026-39821); the Debian base scanned clean. Bumped `golang.org/x/net` to
  **v0.55.0** (pulling `x/sys`, `x/term`, `x/text` forward too).
- **Workflow:** the Trivy version pin used the wrong input name (`trivy-version`,
  silently ignored) and then the wrong form; the `setup-trivy` input is
  `version: 'v0.71.2'` (tag form, with the `v`).

### Lessons learned

1. **cgo changes the whole packaging story.** A static Go service ships as a lone
   binary; Talunor's binary dlopens two extensions and loads a model, so the
   honest artifact is a **self-contained image** that bundles all three — and the
   runtime base must carry `libstdc++6`. A "download the binary" release is only
   useful if it ships its runtime dependencies alongside.
2. **Match the runtime glibc to the prebuilt native assets.** The sqliteai `.so`s
   were linked against an older glibc; a newer base (trixie) runs them via
   backward compatibility, whereas an older base could be missing symbols.
3. **A CVE gate proves itself immediately or never.** It caught an out-of-date
   transitive dependency on the very first run — exactly the drift a scheduled
   re-scan is meant to surface on shipped images.
4. **Pin the *scanner* version too, with the exact input contract.** A pin that
   silently no-ops (wrong input name / missing `v` prefix) gives false assurance;
   verify the tool actually honoured it.

## [0.5.5] - 2026-07-15 — Semantic memory: reflection distils facts (Fix B)

A follow-up to 0.5.4. Fix A stopped the agent's own questions from polluting
recall; this adds the deeper fix — the agent now **writes its own memory**.

> An early taste of **Iteration 4 (learning/reflection)**, pulled forward as a
> memory-quality feature. `v0.6.0` remains reserved for Iteration 2 (tools).

### The problem it addresses

Even after 0.5.4, durable facts lived only inside verbatim conversation turns,
and a chatty turn is a *noisy carrier* for a fact. The message
*"hy my name is Carlos and i like to develop in Go and Typescript with Bun. and
you?"* sits at cosine distance **0.72** from a query like *"my favorite
languages"* — the signal ("Go and TypeScript") is diluted by greeting and
small-talk, leaving it near the noise floor (*"ok Talunor see you"* is 0.74).
Retrieval is a signal-to-noise problem; distilling the fact fixes the signal.

### Added

- **Semantic memory tier** — `memory.KindFact`: a durable, distilled statement
  ("User's favourite languages are Go and TypeScript."), distinct from episodic
  `KindTurn` rows (verbatim messages). Facts have no role and are eligible for
  recall like any other memory — but they win on merit because they embed close
  to how a future question is phrased.
- **Reflection step** (`internal/agent/reflect.go`) — after each turn, a
  `FactExtractor` distils durable facts from the user's message and stores the
  new ones as `KindFact`:
  - `llmExtractor` asks the agent's own provider (temperature 0, no token cap so
    a thinking model isn't starved) with a strict prompt: durable facts only,
    one third-person sentence per line, or `NONE`. `parseFacts` cleans the reply.
  - The interface is pluggable and best-effort: tests inject a fake extractor;
    `DisableReflection()` turns it off; any extraction/storage error is swallowed
    so it can never disturb the reply the user already received.
  - **Deduplication** (`Agent.factKnown`, `Config.DedupMaxDistance = 0.20`):
    restating a known fact does not accumulate near-duplicate rows — checked
    against existing *facts* only, so the first distillation of a turn is never
    blocked by the raw turn sitting nearby.
- Reflection runs in the **learn phase** (`learnWhileStreaming`), after every
  token has streamed to the caller but before the stream closes — off the
  user-visible critical path, yet deterministic (when the stream ends, learning
  is done), which keeps it testable.
- Tests: `TestParseFacts` (parser, no model); `TestReflectionStoresAndRecallsFact`
  (replays the reported session — a distilled fact is stored and recalled for a
  differently-worded re-ask); `TestReflectionDeduplicates`.

### Changed

- `agent.New` installs a default LLM-based extractor when `Config.Extractor` is
  nil; inject `DisableReflection()` to opt out. UI/loop tests that assert exact
  stored-turn counts now disable reflection (they exercise plumbing, not
  learning).

### Lessons learned

1. **The LLM is a memory *writer*, not only a reader.** The highest-leverage
   retrieval fix is often upstream of retrieval: change *what you store*. Asking
   the model to distil a message into clean facts (reflection) makes later recall
   easy, because the stored text now embeds close to how the question will be
   asked.
2. **Retrieval is signal-to-noise, not keyword matching.** A fact buried in
   greeting and small-talk embeds far from the query even when the words are
   present. Distillation raises the signal; that is what moved the fact from
   distance 0.72 (below the noise floor) to a confident recall.
3. **Semantic memory needs curation too** — dedup by similarity, or reflection
   rebuilds the very pollution 0.5.4 removed.
4. **Reflection costs a second model call per turn.** Here it blocks the
   turn-complete signal (a visible pause after the answer). Production systems do
   this asynchronously or in batches — the honest next lesson, and why Iteration 4
   (consolidation, salience/decay, async learning) exists.

## [0.5.4] - 2026-07-15 — Fix: recall loop (assistant turns pollute retrieval) + `/forget`

### Fixed

- **The agent could get stuck re-asking for something the user already told it.**
  Symptom: a user states a fact ("my name is Carlos, I like Go and Typescript"),
  and several turns later, when they ask to use it, the agent keeps asking for it
  instead of recalling it.

  Root cause was in retrieval, not storage — the fact *was* in the database.
  Every conversation turn (user **and** assistant) is stored and embedded, and
  the assistant's own clarifying questions (*"what is your favourite language?"*)
  are the **strongest** semantic match to the user re-asking that same question.
  So the top-`k` recall filled with the model's prior clarifications and evicted
  the one memory holding the answer — a self-reinforcing loop (the more it asks,
  the more its own asks dominate recall). Measured on the reported session, the
  user's fact ranked **6th** for a `k=5` retrieval — just outside the window.

  Fix (`Store.Recall`):
  - **Exclude assistant turns from semantic recall.** Only user turns and
    document chunks are retrieved; the assistant's replies no longer compete with
    the facts the user actually stated. (Assistant turns are still stored and
    still kept verbatim in short-term context — they're only removed from KNN.)
  - **Over-fetch KNN candidates** (`k × 6`) before role-filtering, so dropping
    assistant rows doesn't return fewer than `k` results.
  - Raised the default `RecallK` from **5 → 8** (`agent.DefaultConfig`) as
    defence-in-depth.

  Regression test `TestRecallExcludesAssistantTurns` replays the exact reported
  session and asserts the user's fact is recalled and no assistant turn leaks in;
  it fails against the pre-fix code.

### Added

- **`/forget <id>`** (TUI and REPL) — delete a single memory by the `#id` shown
  by `/list`, for pruning noise/mistakes by hand. `Store.Forget(ctx, id)` reports
  whether a row existed (so the UI can say *"no memory #N"*); `Agent.ForgetMemory`
  returns the display line; `agent.MemoryID` parses the argument (shared by both
  front-ends). A plain `DELETE` suffices — `vector_full_scan` reads the embedding
  column live, so there is no separate index to update.

### Lessons learned

1. **In RAG, what you *store* decides what you can *retrieve* — and storing the
   agent's own words can poison recall.** Verbatim assistant turns are
   near-duplicates of the questions users re-ask, so they dominate similarity
   search and bury the user's actual answers. Retrieval quality is a *curation*
   problem: choose what is eligible to be recalled, don't just embed everything.
2. **A stuck loop can be self-reinforcing through the memory itself.** Each time
   the model asked the same question, that question became a high-ranking
   "memory," making the next recall even more likely to surface a question
   instead of the answer. Feedback loops hide in data pipelines, not just code.
3. **Top-`k` alone is fragile when the store contains near-duplicates.** The fact
   was retrievable but ranked just outside `k`. Filtering *what competes* for the
   `k` slots fixes this far more reliably than simply enlarging `k`.
4. **Give the user a scalpel.** Automatic memory will always mis-store sometimes;
   a one-line `/forget <id>` lets the user curate rather than wipe everything.

## [0.5.3] - 2026-07-14 — TUI text selection + AGENTS.md

### Changed

- **TUI no longer captures the mouse.** `tea.WithMouseCellMotion` was grabbing
  mouse events, which disables the terminal's own click-drag text selection —
  making it impossible to select and copy a transcript. Dropped it so selection
  works again; keyboard scrolling covers navigation: ↑/↓ now scroll the
  transcript alongside PgUp/PgDn (single-line input doesn't need them). Status
  bar and `/help` hints updated.

### Added

- **`AGENTS.md`** — an orientation guide for AI/human contributors: the working
  agreement (one layer = one minor, release checklist), the package map, build
  commands, all the hard-won gotchas (SQLite extensions, thinking models, TUI
  terminal-query pitfall), and testing conventions.

### Lessons learned

1. **Mouse capture and text selection are mutually exclusive** in a terminal.
   A TUI that grabs the mouse for scrolling/clicks takes away the user's ability
   to select and copy — for a tool whose output people want to share, selection
   wins. Provide keyboard scrolling instead.

## [0.5.2] - 2026-07-14 — Fix: OSC 11 escape-sequence garbage in the TUI

### Fixed

- On TUI start, a stray sequence like `]11;rgb:3030/0a0a/2424` appeared next to
  the input. `glamour.WithAutoStyle` was querying the terminal background (OSC 11)
  from inside the Bubble Tea event loop (when the Glamour renderer is built on the
  first `WindowSizeMsg`); the terminal's reply raced Bubble Tea's input reader and
  was painted to the screen instead of consumed.

  Fix: detect the background **once, before** `tea.NewProgram(...).Run()` (via
  `lipgloss.HasDarkBackground()`, handled synchronously while the terminal is
  still in normal mode) and build Glamour with an explicit
  `WithStandardStyle("dark"|"light")` — no query inside the render loop.
  Verified with a PTY harness: zero OSC 11 queries emitted after the alternate
  screen is entered.

### Lessons learned

1. **Never query the terminal from inside the render loop.** Any code that emits
   a terminal query (background color, cursor position, device attributes) and
   reads the reply will fight the TUI framework's own input reader. Do such
   detection once, up front, before the program takes over the terminal.

## [0.5.1] - 2026-07-14 — Iteration 1 polish: help, memory inspection, config

UX and configuration fixes surfaced by using the agent: commands were not
discoverable, memory persistence was invisible and tied to the working
directory, and there was no way to see what was stored.

### Added

- **Slash commands in the TUI *and* the REPL**: `/help`, `/mem` (count + database
  path), `/list [n]` (recent memories), `/exit`; the TUI also has `/clear` (clears
  the on-screen transcript, not the stored memory). Commands run locally and
  never hit the LLM. The TUI shows `Type /help for commands` on start.
- `talunor --list N` — dump the most recent N stored memories and exit
  (non-interactive inspection; no model needed).
- `Store.List`, `Store.Path`; `Agent.Help` / `MemoryStats` / `ListMemories` and a
  shared `agent.FormatMemories`.
- Startup line now shows the database path so persistence is visible.

### Changed

- **Database path is configurable and stable.** `TALUNOR_DB` overrides it;
  otherwise it defaults to `$XDG_DATA_HOME/talunor/talunor.db` (or
  `~/.local/share/talunor/talunor.db`), created automatically. Memory now
  persists across sessions regardless of the working directory — previously it
  was a hardcoded `./talunor.db`, so it only persisted when launched from the
  same folder.
- Extension/model paths also honour env overrides (`TALUNOR_VECTOR_EXT`,
  `TALUNOR_AI_EXT`, `TALUNOR_EMBED_MODEL`) so the binary is not tied to the repo
  root.

### Lessons learned

1. **Discoverability is a feature.** A capable agent with hidden commands feels
   broken; `/help` and a visible startup hint cost little and change the
   experience. Centralising the command help (`agent.HelpText`) keeps the TUI and
   REPL in sync.
2. **"Persistent" must also mean "findable".** A CWD-relative database silently
   forks memory per launch directory. A stable XDG location plus showing the path
   makes persistence real and debuggable.
3. **Make stored state inspectable.** `--list` / `/list` read only text columns,
   so they work as a plain window into the database even though writes need the
   extensions loaded.

## [0.5.0] - 2026-07-14 — Layer 5: TUI (completes Iteration 1)

A Bubble Tea + Glamour terminal UI, now the default front-end. **Iteration 1 —
a conversational agent with multi-tier memory — is complete.**

### Added

- `internal/tui` — a Bubble Tea model over the agent loop:
  - Scrollable transcript (viewport) + text input; tokens stream in live.
  - **Glamour** renders the assistant's markdown (code blocks, lists, bold)
    once a reply completes; during streaming the raw text is shown (cheap, no
    flicker), and a thinking model's reasoning streams dimmed.
  - Status bar: provider · model · memory count · state · key hints.
  - Mouse-wheel / PgUp-PgDn scrolling; Ctrl-C or Esc to quit.
- `cmd/talunor` now launches the **TUI by default**; `--plain` selects the
  original line-based REPL.
- `Agent.MemoryCount` — powers the status bar.
- `internal/tui` headless tests: drive the `Update` loop (window size →
  keystrokes → pump the stream to completion) with a fake provider and a real
  store — no terminal needed — asserting the reply renders and both turns
  persist, and that Enter mid-stream is ignored.

### Design decisions

- **Channel → tea.Msg bridge.** `waitForChunk` reads exactly one `llm.Chunk`
  and returns it as a message; each `Update` re-issues it to pull the next.
  Tokens land in the UI event loop with no background goroutine mutating shared
  state — the Bubble Tea way.
- **Render raw while streaming, Glamour on completion.** Re-running a markdown
  renderer per token flickers and burns CPU; showing raw text live and
  formatting once at the end is smooth and correct.
- **Pointer model.** The model is used as a `*Model` so the streaming
  accumulators are never copied by the event loop (a value model would copy a
  `strings.Builder`, which panics; even with plain strings, a pointer keeps the
  bridge honest).
- **TUI default, REPL as `--plain`.** The rich UI is what you want day-to-day;
  the REPL remains for scripting, piping, and debugging.

### Lessons learned

1. **A streaming channel maps cleanly onto Bubble Tea's `Cmd`/`Msg` model** —
   one chunk per command, re-issued each update. No mutexes, no leaked
   goroutines writing to the model.
2. **Separate "live" and "final" rendering.** The cheap raw pass keeps the UI
   responsive; the expensive Glamour pass runs once. This is the same
   reasoning/answer split from Layer 3, now visual.
3. **A TUI is testable without a terminal.** Feeding synthetic `tea.Msg`s
   through `Update` and pumping the returned `Cmd`s exercises the whole
   interaction deterministically.

## [0.4.0] - 2026-07-14 — Layer 4: Agent loop

The three substrates connect into one cognitive turn. This is the first version
that **remembers across turns** and injects relevant long-term memories into its
reasoning.

### Added

- `internal/agent` — the cognitive loop:
  - `Agent.Turn(ctx, input)` runs perceive → recall → reason → store and returns
    the assistant's reply as a stream. It recalls **before** storing the input
    (so the current message is not retrieved as its own match), records the user
    turn immediately, and records the assistant turn only once the stream
    completes cleanly.
  - `Config` / `DefaultConfig` — system prompt, recall `k` + distance threshold,
    short-term capacity, provider options.
- `internal/render` — a shared console renderer (`Stream`) extracted so
  `cmd/chat` and `cmd/talunor` don't duplicate the reasoning-dimmed/answer-bright
  logic.
- `cmd/talunor` — the interactive agent REPL over a **persistent** database, so
  long-term memory accumulates across sessions. Slash commands `/exit`, `/quit`,
  `/mem`; Ctrl-C cancels cleanly.
- `internal/agent` tests: prompt-assembly order (no model/store needed) and a
  full-loop integration test (a seeded fact is recalled into the prompt and both
  turns are persisted) using a fake provider.
- `make run` starts the REPL.

### Changed

- `cmd/chat` now uses `internal/render` instead of its own inline renderer.

### Design decisions

- **Recall before store.** Storing the user input first would make KNN return
  that very message as the nearest match. Recalling against prior memory first
  keeps retrieval meaningful.
- **Store the assistant turn only on clean completion.** A cancelled or errored
  stream leaves a partial/empty answer that should not pollute memory; the user
  turn is stored regardless because it genuinely happened.
- **Two memory tiers, both injected.** Short-term turns give verbatim recent
  continuity; long-term recall (thresholded) surfaces older relevant facts. The
  agent orchestrates both; neither substrate knows about the other.
- **Tee-while-streaming.** `Turn` forwards chunks to the caller as they arrive
  while accumulating the answer for storage — the user sees tokens live and the
  memory write happens exactly once, at the end.

### Lessons learned

1. **Order in the loop is a correctness issue, not a detail.** Recall-then-store
   vs. store-then-recall changes what the model sees; the former is required.
2. **Streaming and "learning" must cohabit.** Returning the raw provider stream
   would make it impossible to capture the full answer for storage. Wrapping the
   stream in a tee goroutine keeps live output *and* records the completed turn.
3. **Extract the renderer once you have a second caller.** `cmd/chat` and
   `cmd/talunor` share identical terminal rendering — `internal/render` removes
   the duplication before it drifts.

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

[Unreleased]: https://github.com/lao-tseu-is-alive/Talunor/compare/v0.8.0...HEAD
[0.8.0]: https://github.com/lao-tseu-is-alive/Talunor/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/lao-tseu-is-alive/Talunor/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/lao-tseu-is-alive/Talunor/compare/v0.5.7...v0.6.0
[0.5.7]: https://github.com/lao-tseu-is-alive/Talunor/compare/v0.5.6...v0.5.7
[0.5.6]: https://github.com/lao-tseu-is-alive/Talunor/compare/v0.5.5...v0.5.6
[0.5.5]: https://github.com/lao-tseu-is-alive/Talunor/compare/v0.5.4...v0.5.5
[0.5.4]: https://github.com/lao-tseu-is-alive/Talunor/compare/v0.5.3...v0.5.4
[0.5.3]: https://github.com/lao-tseu-is-alive/Talunor/compare/v0.5.2...v0.5.3
[0.5.2]: https://github.com/lao-tseu-is-alive/Talunor/compare/v0.5.1...v0.5.2
[0.5.1]: https://github.com/lao-tseu-is-alive/Talunor/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/lao-tseu-is-alive/Talunor/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/lao-tseu-is-alive/Talunor/compare/v0.3.0...v0.4.0
[0.3.0]: https://github.com/lao-tseu-is-alive/Talunor/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/lao-tseu-is-alive/Talunor/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/lao-tseu-is-alive/Talunor/releases/tag/v0.1.0

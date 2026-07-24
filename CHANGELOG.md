# Changelog

All notable changes to Talunor are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Talunor uses a `0.MINOR.PATCH` scheme where **each completed build layer bumps
`MINOR`**. Iteration 1 (a conversational agent with memory) completes at `0.5.0`.

This changelog doubles as a teaching log: each version records not just *what*
changed but the *lessons learned* while getting there.

## [Unreleased]

- **Iteration 4, continued** — learning: fact provenance + confidence (Layer 16),
  salience / decay / consolidation (Layer 17), and async reflection (Layer 18), each
  built on the migration runner that Layer 15 just landed. Informed by calibration
  (don't consolidate facts from an unreliable model). The executed plan becomes an
  input to learning (deferred from Layer 13).

## [0.15.0] - 2026-07-24 — Iteration 4 begins: schema versioning & migrations (Layer 15)

Iteration 4 (learning) needs to *evolve* the memory schema — add per-fact provenance,
confidence, salience, decay. This layer lays the groundwork with zero behaviour change:
a tiny migration runner, so every later learning layer adds its columns as an ordered,
in-place migration instead of an ad-hoc `CREATE TABLE`. Done now, deliberately, while
the schema is still one flat table and the cost of getting migrations right is low.

### Added

- **`internal/memory/migrate.go`** — an append-only, ordered list of `migration`s and
  a `runMigrations` runner. The applied version is a single integer in the existing
  `meta` table (`schema_version`), read on every `Open`; migrations newer than the
  recorded version are applied, each in its own transaction **with** its version stamp
  (all-or-nothing, crash-safe). Migration 1 is the baseline (the `memories` table).
- **Automatic baselining.** A database that predates versioning (a `memories` table,
  no `schema_version`) starts at version 0; migration 1's `CREATE TABLE IF NOT EXISTS`
  is a harmless no-op on it, then the version is stamped — no data loss, no manual step.
- **`Store.SchemaVersion`** + a `schema version:` line in `make doctor`.

### Changed

- `bootstrap` now creates the `meta` table first (it holds the version), then runs the
  migrations (which create `memories`), then `vector_init` + the provenance check.
- The stale `memories.kind` comment (`'turn' | 'doc_chunk'`) now includes `'fact'`.

### Lessons learned

1. **Add the migration seam before you need it, not after.** The right time to
   introduce schema versioning is while the schema is trivial — one table, one baseline
   migration, no risk. Waiting until the first painful `ALTER` under real data is how
   projects end up with hand-run SQL and drift. The cheapest migration to write is the
   one that changes nothing.
2. **Baseline an existing schema, don't demand a clean slate.** Real users have
   databases already. Making migration 1 idempotent (`IF NOT EXISTS`) and treating an
   unstamped DB as version 0 means the machinery adopts a legacy database silently and
   safely — the test that matters is "reopen a pre-versioning DB and lose nothing".
3. **Append-only is the whole discipline.** A migration list is a shipped history:
   users have run those exact statements. The single rule — never reorder, renumber, or
   edit a released migration; fix mistakes with a *new* one — is what keeps every
   database in the fleet reaching the same state.

## [0.14.1] - 2026-07-23 — Course: Lesson 16 (measure the model), bilingual
- **Planner follow-ups (deferred from Layer 13):** `/edit-plan` (hand-edit a plan
  before it runs), semantic deviation detection, and automatic light re-planning
  when a step surprises — each a small layer / lesson of its own.

## [0.14.1] - 2026-07-23 — Course: Lesson 16 (measure the model), bilingual

A docs-only release. Layer 14 (`v0.14.0`) gets its lesson, closing the course's
trust-and-verify arc (11 → 15 → 16) and bridging to Iteration 4.

### Added

- **Lesson 16 — "Measure the model: building a reliability canary"**
  (`docs/lessons/16-measure-the-model/`, bilingual EN/FR). A ~75-min Level-3 🔍
  exploration + hands-on on `main`: it reads `internal/calibration` to teach the
  **three design decisions that make an LLM eval harness trustworthy** — the verifier
  must be deterministic (never an LLM: the recursive trap of Lesson 15); accuracy and
  consistency are different axes (a pass-rate near 0.5 is the danger; a standard
  deviation belongs on latency, not a binary outcome); and the value is *drift* from a
  pinned baseline, not the absolute score (the canary of Lesson 11) — plus the honest
  threat model of a public/hosted suite. The reader runs the seed suite, authors a
  scenario, and catches a regression against a baseline. Model-agnostic. Course now 00–16.
- Course index (EN + FR) updated to 00–16; Lesson 15's forward pointer now leads to Lesson 16.

### Lessons learned

1. **A "how it's built" lesson earns its place by teaching the *decisions*, not the
   API.** Lesson 16 could have been a `cmd/calibrate` tutorial; instead it isolates the
   three choices (deterministic verifier, the right statistic per axis, drift over
   absolute) that recur in every eval system — the transferable part.
2. **Distinct-but-adjacent lessons need an explicit hinge.** Lesson 15 (verify one
   claim by hand) and Lesson 16 (measure continuously, automatically) are close enough
   to blur; naming the difference in both — manual/one-off vs automated/continuous —
   is what keeps each sharp.

## [0.14.0] - 2026-07-23 — Layer 14: model calibration (a reliability canary)

A preliminary layer before Iteration 4 (learning), motivated by the review episode
that produced Lesson 15: an agent that will *learn* from a model must first *measure*
whether that model is reliable. `internal/calibration` runs a fixed suite of
scenarios whose correct answers are known and checked **deterministically**, so a
model's factual accuracy, format compliance, and consistency can be scored — and
silent quality drift (a provider update, a cheaper "flash" variant) caught before
users feel it. It is a truthfulness canary, in the spirit of Layer 11's embedding
canary.

### Added

- **`internal/calibration`** — a standalone, provider-agnostic harness (it runs
  against any `llm.Provider`, so one suite compares many models on one yardstick):
  - `Scenario`/`Turn`/`Assert` types loaded from YAML via a **source-agnostic**
    `Parse([]byte)` (the bytes may be plaintext or decrypted — the core does not care).
    Scenarios are 1–5 clean-room turns (no session memory).
  - **Deterministic matchers only** (`equals`, `contains(_all/any)`, `not_contains`,
    `regex`, `number` with tolerance, `json_valid`, `any_of`/`all_of`). Deliberately
    **no LLM judge**: a verifier that called a model would inherit the unreliability
    it measures.
  - `Run` replays each scenario N times and reports a **pass-rate** per
    scenario/category (a rate near 0.5 = flaky) plus latency **mean ± stddev** (the
    place a standard deviation is actually meaningful — a continuous metric).
  - **Baseline + drift**: `AsBaseline`/`Diff` detect a regression against a pinned
    reference — the automatic version of "this model got worse".
  - **Optional AES-256-GCM encryption** (`CALIBRATION_KEY`) for a private suite you
    want to version in a shared repo without exposing the answers to scrapers.
    Standard stdlib crypto, no home-grown scheme.
- **`cmd/calibrate`** — a standalone CLI: `calibrate --suite s.yaml [--baseline b.json |
  --save-baseline b.json] [--json]` (exit 1 on regression, CI-able) and
  `calibrate encrypt --in s.yaml --out s.enc`. Provider via `llm.FromEnv()`, like the agent.
- **`docs/calibration.seed.yaml`** — a public, deterministic 6-scenario example set
  (arithmetic, %, factual, JSON format, instruction-following, an *indicative*
  refuse-to-fabricate), with an explicit **threat-model header**: public suites
  measure *drift* well but *absolute* score weakly (memorisation); a hosted provider
  sees the prompts at inference, so encryption stops scrapers, not the provider.
- **`CALIBRATION_KEY`** env knob.

### Changed

- Course **Lesson 15** gains a short "naming the defects" aside (the five distinct
  failure modes — confabulation, provenance dishonesty, sycophancy, quality variance,
  error compounding), kept model-agnostic and mirrored EN/FR.

### Deferred (documented)

- Wiring calibration into the agent's policy (let a low-calibration model be routed
  away from high-risk steps, or require senior/deterministic validation upstream) —
  a natural next step once the harness has a track record. A named-predicate (in-code)
  verifier registry beyond the declarative YAML matchers, if ever needed.

### Lessons learned

1. **You can't govern what you don't measure — and you can't measure an LLM with an
   LLM.** The harness is only trustworthy because every verifier is deterministic.
   The moment a model judges the output, the measurement inherits the failure it was
   built to catch (Lesson 15, applied to our own tooling).
2. **The value of calibration is the *delta*, not the score.** A public suite can be
   memorised, so its absolute number is soft; but the same suite re-run over time
   turns "the model silently degraded" into a red CI. Build for drift detection (a
   pinned baseline), like the embedding canary of Layer 11.
3. **Accuracy and consistency are different axes.** A binary pass/fail's variance is
   fixed by its rate (a Bernoulli), so "flaky" is read off the rate's distance from
   0/1 — a standard deviation only earns its keep on a continuous metric (latency).
   Reporting the wrong statistic would hide the very flakiness you care about.
4. **Encrypt for the threat you actually have.** Encrypting a suite stops passive
   repo-scraping into training sets — a real vector — but does nothing about a hosted
   provider you hand the prompts to at inference. Naming what a control does *not*
   cover is as important as the control (same honesty as the sandbox's "no seccomp").

## [0.13.4] - 2026-07-23 — Course: Lesson 15 (don't trust the review), bilingual

A docs-only release, and the course's meta-lesson. During Talunor's own development
several LLMs reviewed the codebase; most were grounded, but one produced a fluent,
confident, and largely *fabricated* report — wrong driver, a search engine the project
doesn't have, the SSRF guard described backwards — and claimed to have "analysed the
exact source" while offering invented code as proof. A single falsifiable question
("paste the line from `go.mod`") collapsed it. That episode becomes a lesson.

### Added

- **Lesson 15 — "Don't trust the review: verifying what an AI claims about your code"**
  (`docs/lessons/15-dont-trust-the-review/`, bilingual EN/FR). A ~60-min Level-2 🔍
  verification *exercise* (not an essay): the reader is handed five claims from a real
  AI review — four false (including a self-contradicting "CGO-free" claim and an SSRF
  mechanism described inverted) and one true — and must falsify each with a concrete
  command, cross-checked against the repo's own `AGENTS.md` gotchas. It teaches the
  model-agnostic method — demand the verbatim citation; the repo's docs are ground
  truth; verify each claim independently; fluency and "I read your code" are not
  evidence — and the twist that even a model's articulate apology is a claim to verify.
  Deliberately names no vendor (it's about the method, and models date fast). Course
  now 00–15.

### Lessons learned

1. **The capstone of a trust-and-verify course is verifying the reviewer.** Every
   prior lesson taught distrust of a specific thing (a silent recall, an untrusted
   memory, an over-promising approval); generalising it to "an AI's output is a claim,
   never evidence" is the natural close — and the most transferable skill of all.
2. **A verification lesson must make the reader do the verifying.** The lesson isn't
   "LLMs hallucinate"; it's a set of `grep`/read commands the reader runs to watch four
   plausible claims fall. Doing the falsification is what installs the instinct.
3. **Teach the method, not the anecdote.** Naming the model would date the lesson and
   turn a durable skill into vendor gossip. The claims are anonymised; the ground truth
   (this repo) is permanent.

## [0.13.3] - 2026-07-23 — Convergent review batch: privacy, integrity, honesty, CI

Six small, low-risk fixes that a round of independent cross-model reviews of `v0.13.1`
converged on — each cheap, each closing a real (if bounded) gap. No behaviour change
for the happy path; several classes of silent or latent failure are now closed or
observable.

### Fixed / Changed

- **Local data privacy.** The memory database holds personal facts and verbatim
  turns, so `memory.Open` now creates its directory `0700` and `chmod`s the DB file
  `0600` (it was a `0755` dir + umask-dependent file); the prompt-history directory is
  `0700` too. Owner-only on shared machines. (`internal/memory/store.go`,
  `internal/history/history.go`; test `TestStoreFilePermissions`.)
- **`ReEmbed` is now atomic.** It computed and wrote each new embedding row-by-row
  with no transaction, so a mid-way failure left the store with a *mix* of old- and
  new-space vectors. It now computes every embedding first (untouched DB on failure),
  then applies all updates **and** the fingerprint stamp in one transaction — all or
  nothing. (`internal/memory/provenance.go`.)
- **Silent assistant-store errors are now observable.** The best-effort
  `Remember(assistant turn)` ignored its error (short-term had the reply, long-term
  might not). It stays non-fatal but is now traced and shown under `/debug`.
  (`internal/agent/agent.go`, `internal/agent/execute.go`.)
- **The planner sees recalled memory.** `runPlanned` passed the planner an empty
  memory context while the executor got the recalled memories. It now passes the same
  memories, framed as untrusted DATA (the framing is extracted into `fencedMemories`,
  shared with `buildMessages`). Plans can use what the agent knows. (`internal/agent`;
  test `TestPlannerReceivesRecalledMemory`.)
- **Plan `DependsOn` is validated as a DAG.** `plan.Validate` checked that
  dependencies resolve and aren't self-referential but not that the graph is acyclic
  (a stale comment said cycle detection was "deferred to the executor"). It now
  rejects cycles (three-colour DFS) and the comment states honestly that DependsOn is
  advisory today. (`internal/plan/plan.go`; test case `dependency cycle`.)
- **CI now runs the drift guards and the race detector.** `ci.yml` ran only
  `vet` + `test`; the release guards (gofmt, atlas-check, readme-check, lessons-check,
  checksums) were enforced only locally before a tag. CI now runs `make release-check`
  (with `fetch-depth: 0` so `lessons-check` sees the pinned tags) and `go test -race`.

### Lessons learned

1. **Independent review converges on the cheap wins.** These six came from separate
   model reviews that mostly *agreed*; the overlap was the signal that they were real,
   not stylistic. A single reviewer (even a thorough one) missed some that two others
   caught — breadth of perspective beats depth of one.
2. **"Personal" is broader than "secret".** No credentials live in the DB, but names,
   preferences, and prompt history do. File permissions are a one-line default that
   should match the sensitivity of the data, not the absence of obvious secrets.
3. **A data migration must be all-or-nothing.** Row-by-row rewrites of vectors are the
   textbook case for a transaction: a partial migration that *looks* done but mixes
   two vector spaces corrupts recall silently — the worst failure mode.
4. **A guard that isn't run by the machine is a guideline.** The drift alarms existed
   and were excellent; enforcing them only by human discipline before a tag meant a PR
   could still merge them broken. Move the guard to where it can't be skipped.

## [0.13.2] - 2026-07-23 — Fix: plan-mode approval integrity (P1) + Lesson 14 (post-mortem)

A security fix and its post-mortem lesson, shipped together. A cross-model review of
`v0.13.1` flagged a real gap in the planner's default approval mode: **a whole-plan
approval bound the tool *names* but not the *arguments* the ReAct executor actually
ran.** A plan that displayed `bash({"cmd":"ls"})` could execute
`bash({"cmd":"rm -rf /"})` — the human approved "the plan", but never saw the second
command. This is exactly the confused-deputy pattern Lesson 12 was built to prevent,
re-introduced by the guardrail meant to make things safer.

### Fixed

- **Plan-mode approval integrity (`internal/agent`).** The blunt boolean
  `execCtx.skipStepApproval` is replaced by a risk threshold `reapproveAtOrAbove`.
  In `runTool` a policy-flagged step now re-prompts — **with its live arguments** —
  when `RiskLevel >= reapproveAtOrAbove`. `runPlanned` sets the threshold per mode:
  `plan` → `RiskHigh` (low/medium ride on the plan approval; **high-risk steps like
  the shell re-confirm the arguments actually about to run**), `step` → `RiskLow`
  (every risky step re-confirms), `highrisk` → `RiskLow` (advisory plan, per-call
  policy as before). The planner-off ReAct loop is unchanged (zero value =
  `RiskLow` = prompt whenever the policy asks). This is the two-level approval
  Lesson 13 described — now actually enforced.
- Regression tests: `TestPlannedPlanModeReapprovesHighRiskLiveArgs` (the re-prompt
  shows `rm -rf`, not the plan's `ls`), `TestPlannedPlanModeDenyHighRiskStops`, and
  `TestPlannedPlanModeMediumRiskCoveredByPlan`.

### Added

- **Lesson 14 — "The approval that didn't bind: a plan-mode security post-mortem"**
  (`docs/lessons/14-the-approval-that-didnt-bind/`, bilingual EN/FR). The course's
  second post-mortem (after Lesson 11) and the first from a bug in a *guardrail*: it
  reads the gap at `v0.13.1`, the fix on `main`, and draws the review lesson — an
  author is the worst reviewer of their own guardrail; independent perspectives (and
  their *disagreements*) find the holes intent conceals. Course now 00–14.

### Lessons learned

1. **An approval protects only what it mechanically binds — not what it displays.**
   The plan UI showed arguments; the mechanism bound only tool names. Whenever a
   human decision is taken against a *representation* but produces an *effect* on
   something else, the distance between them is the vulnerability. Gate the effect.
2. **A boolean is a blunt instrument for a graded decision.** `skipStepApproval`
   (all-or-nothing) hid the gap; a *risk threshold* expressed the real intent —
   "the plan approval covers up to here, re-confirm above it" — in one comparison.
3. **"Documented limitation" is a smell, not a defence.** The structural cap was
   genuinely documented, which is exactly why the gap survived review: everyone read
   the note and no one re-derived the consequence. If a safety control needs a
   caveat that it's weaker than it looks, fix the control.
4. **You are the worst reviewer of your own guardrail** — you test it against your
   intent, not its behaviour. This gap was written by the author of Lesson 12 (which
   teaches not to do it) and caught only by independent cross-model review.

## [0.13.1] - 2026-07-22 — Course: Lesson 13 (plan before you act), bilingual

A docs-only release. The planner (`v0.13.0`) gets its lesson, closing the course's
coverage of Iteration 3: the course reaches its **fourteenth** entry (00–13).

### Added

- **Lesson 13 — "Plan before you act: from emergent ReAct to a plan you can read"**
  (`docs/lessons/13-plan-before-you-act/`), in 🇬🇧 English and 🇫🇷 French. An advanced
  (Level 3, ~90 min) 🔍 exploration pinned to `v0.13.0`: it contrasts emergent (ReAct)
  and deliberate (plan-first) execution, reads `planner.go` as a four-part discipline
  for getting reliable structured output from an LLM (strict contract → tolerant
  extraction → validation → self-correcting retry), traces `runPlanned` (plan → policy
  pre-screen → whole-plan approval → capped execution → learn), and has the reader run
  the planner live, view `/plan`, and watch a policy `deny` block a plan.
- Course index (EN + FR) updated to 00–13.

### Lessons learned

1. **A capstone lesson should name the trade-off, not just the feature.** The planner
   is easy to sell as "better"; the honest lesson makes the reader feel what deliberate
   planning *costs* (adaptivity) against what it *buys* (an inspectable, cappable,
   refusable artifact) — so they choose per situation rather than cargo-culting.
2. **Reliable structured output is the reusable takeaway.** The single most portable
   skill in the planner isn't agent-specific — it's the contract → extraction →
   validation → retry loop for coaxing JSON out of a text model. The lesson leads with
   it because a reader will need it far beyond Talunor.

## [0.13.0] - 2026-07-22 — Iteration 3 complete: the explicit planner (Layer 13)

Talunor now **plans before it acts**. Where the ReAct loop discovers the sequence
one tool call at a time, an optional planner first produces a whole, structured
[plan.Plan] the human can read and approve — then the loop executes it *capped to
the plan's tools*, so a plan-injected model can't wander off into actions nobody
approved. It is the capstone of Iteration 3: Layer 12 gave the agent a guardrail,
Layer 13 gives it forethought.

Planning is **opt-in** (`TALUNOR_PLANNER=1`); off, the plain ReAct loop is
unchanged — so a reader can toggle the two and feel the difference.

### Added

- **`internal/agent/planner.go`** — the `Planner` interface and the default
  `llmPlanner`: it asks the model for a JSON plan, extracts the object (tolerating
  prose / code fences), validates it (structure via `plan.Validate`, plus "every
  tool step names a known tool" and "ends in a final step"), and **retries once**
  with the exact error fed back. The planner **never executes tools** — it only
  plans. `NewLLMPlanner` builds it over the agent's own provider (temperature 0).
- **`internal/agent/execute.go`** — `runPlanned`, the planned turn: **plan → policy
  pre-screen → whole-plan approval → capped execution → learn**. A denied step
  blocks the whole plan before anything runs; the human approves the plan as a
  whole (the exact actions they see); execution reuses the ReAct core but offers
  **only the plan's tools** (the structural cap). `FormatPlan` renders a plan for
  approval, the trace, and `/plan`.
- **`Config.Planner`** and **`Config.ApprovalMode`** (`plan` — approve the whole
  plan once, then run its tools; `step` — approve the plan *and* confirm each risky
  step; `highrisk` — no whole-plan prompt, the plan is advisory and per-call policy
  prompts as before; default `plan`).
- **`TALUNOR_PLANNER`** (opt-in) and **`TALUNOR_APPROVAL`** env knobs; a **`/plan`**
  command (TUI + REPL) shows the most recent plan. A planning *failure* falls back
  to the plain ReAct loop, so the turn still answers.

### Changed

- `runLoop` is split into `runLoop` (the plain entry point) and **`reactLoop`** (the
  act/observe core), now shared by the plain and planned paths. `runTool` and the
  core take an `execCtx` carrying the tool cap and whether the whole-plan approval
  already stands in for per-step prompts. `toolSpecs(allow)` filters the offered
  tools — the cap's teeth.

### Deferred (documented, future layers)

- `/edit-plan` (editing a structured plan in a terminal is fiddly — approve/refuse
  first), **semantic** deviation detection, and automatic re-planning on a
  surprising result. The v0.13.0 cap is *structural* (only planned tools are
  offered); semantic "did it drift from the intent?" needs a second LLM judgement
  and is a layer of its own.

### Lessons learned

1. **Plan-level approval is only as safe as the cap that keeps execution inside the
   plan.** A blanket "yes" to a plan would be *weaker* than per-tool approval if the
   model could then call anything — so the load-bearing trick is that execution only
   *offers* the tools the approved plan named. The model can't call what it can't
   see. Enforce the boundary at the API surface, not by asking the model to behave.
2. **Reuse the loop you already trust.** The planner did not replace the ReAct loop;
   `reactLoop` was extracted and *reused* for execution. Planning only changes two
   things — which tools are offered and how approval is solicited — so the
   hard-won loop behaviour (streaming, the tool-iteration cap, fail-closed tools)
   carries over for free.
3. **A `json.Decoder` reads one value and ignores the rest.** Models wrap JSON in
   prose and ```fences```. Rather than a brittle regex, `decodePlan` finds the first
   `{` and lets a Decoder read exactly one object — which also handles braces inside
   strings correctly. Robust extraction, five lines.
4. **Design the failure as a downgrade, not a dead end.** A malformed plan after
   retries doesn't abort the turn; it falls back to the plain ReAct loop. The user
   still gets an answer; planning is an enhancement, not a single point of failure.
5. **Opt-in is a teaching tool, not a hedge.** `TALUNOR_PLANNER` isn't there because
   we're unsure of the feature — it's there so a learner can run the *same* prompt
   with and without a plan and watch emergent ReAct become deliberate planning.

## [0.12.1] - 2026-07-22 — Course: Lesson 12 (the open bar — why an agent needs a policy), bilingual

A docs-only release. The policy engine (`v0.12.0`) gets its lesson: the course
gains its **thirteenth** entry (00–12), the second drawn from a real layer's
*motivation* rather than its mechanics — it argues **why** the guardrail exists
before reading how.

### Added

- **Lesson 12 — "The open bar: why an autonomous agent needs a policy"**
  (`docs/lessons/12-the-open-bar/`), in 🇬🇧 English and 🇫🇷 French. An advanced
  (Level 3, ~75 min) 🔍 exploration pinned to `v0.12.0`: it frames the risk of a
  tool-using agent driven by untrusted text (prompt injection via a recalled memory
  or fetched page), shows why a boolean approval gate can't express *deny*, reads
  the `Policy` interface / `Decision` / the three implementations, and has the
  reader run the deterministic policy + agent tests and author a YAML rule file that
  allows, prompts, and denies.
- Course index (EN + FR) updated to 00–12.

### Lessons learned

1. **Some lessons teach a mechanism; the best security lessons teach a threat.**
   Lesson 12 leads with the *attack* (injected text → tool call) before a line of
   the policy code, because the guardrail only makes sense once you feel what it
   guards against. A feature explained without its adversary reads as ceremony.
2. **A "why" lesson still has to touch running code.** The argument lands because
   the reader can `deny` bash from a text file and watch the refusal happen — the
   principle and the `go test` / `TALUNOR_POLICY` run reinforce each other, the
   course's standing pattern.

## [0.12.0] - 2026-07-22 — Iteration 3 begins: the policy engine (Layer 12)

Iteration 3 turns the ad-hoc approval gate into a first-class **guardrail the agent
consults before it acts**. Where Layer 8 asked "does this tool require approval?"
through a boolean interface method, Layer 12 asks a `Policy`: *given this planned
step, is it allowed, does it need a human, or is it denied?* — three outcomes, one
seam, data-driven if you want it.

It also lays the **plan vocabulary** (`internal/plan`) that the explicit planner
(Layer 13) will produce: the policy's `Evaluate` already takes a `*plan.Plan`, so
the abstraction is plan-shaped from day one. Until the planner exists, the agent
wraps each individual tool call as a one-step plan — so the policy is real, wired,
and tested now, with **zero behaviour change** (the pre-policy approval tests pass
unchanged).

### Added

- **`internal/plan`** — the shared vocabulary of intent: `Plan{Goal, Steps,
  Confidence}` and `PlanStep{ID, Type(tool|think|final), Tool, Arguments,
  Rationale, DependsOn}`, each with a `Validate()` (rationale is required; a
  `tool` step needs a tool name; `DependsOn` must resolve to a real, non-self
  step). `RiskLevel` (low/medium/high) lives here too. `NewToolCallPlan` wraps a
  single tool call as a valid one-step plan — the bridge that lets the policy gate
  tool calls before the planner exists.
- **`internal/policy`** — the guardrail. A `Policy` interface —
  `Evaluate(ctx, *plan.Plan, plan.PlanStep) (Decision, error)` — plus three
  implementations:
  - `AllowAllPolicy` — permits everything at low risk (tests / permissive mode);
  - `ToolGatePolicy` — **the default**: it consults each tool's own
    `Approvable` / `ApprovableFor`, exactly reproducing pre-policy behaviour
    (bash always prompts; web_fetch prompts unless the host is allowlisted);
  - `RuleEnginePolicy` — data-driven, reads a YAML rule file (allow / prompt /
    deny per tool, first match wins, `*` wildcard, fail-safe `prompt` default).
  `Decision{Allowed, Reason, Modified, RiskLevel}` centralises the mapping onto
  the agent's behaviour: `Denied()` (fail closed) and `NeedsApproval()`
  (`Allowed && RiskLevel ≥ medium`). `Modified` lets a policy rewrite a step
  (e.g. force a dry-run) — wired end-to-end though the default policies leave it nil.
- **`TALUNOR_POLICY`** — path to a YAML rule file; unset ⇒ the default
  `ToolGatePolicy`. A malformed file fails closed at startup. Commented starting
  point in [`docs/policy.sample.yaml`](docs/policy.sample.yaml).
- Agent gains `Config.Policy`; `agent.runTool` now consults the policy (deny → the
  model observes the refusal and can recover; approval → the existing human y/n
  gate; otherwise it runs). New tests cover deny-fail-closed and a policy
  overriding a tool's own gate.

### Changed

- `cmd/talunor`: the crowded `run()` wiring is extracted into `buildProvider`,
  `buildTools`, `buildPolicy`, and `buildAgentConfig`; `run` now reads as
  orchestration. Flagged by the external model reviews (`reports/`) as the first
  thing to refactor before Iteration 3 grew the wiring further.
- `agent.needsApproval` is gone, replaced by the policy call.
- First third-party dependency outside the SQLite/TUI/LLM substrate:
  `gopkg.in/yaml.v3`, for policy rule files.

### Lessons learned

1. **Design the interface around where you're going, not where you are.** The
   policy only needs `(tool, args)` today, but its `Evaluate` takes a whole
   `*Plan`. That looks like over-reach until you notice it means Layer 13's planner
   drops in without touching the policy's signature. Wrapping today's single tool
   call as a one-step plan (`NewToolCallPlan`) made the forward-looking shape *free*
   and testable immediately — the abstraction earns its keep before the feature
   that motivates it ships.
2. **Preserve behaviour by delegating, not by re-encoding it.** The tempting move
   was to bake bash-prompts and the web_fetch allowlist into rule data. But the
   allowlist needs the per-argument host introspection the tool already does.
   Making the *default* policy (`ToolGatePolicy`) simply consult the existing
   `Approvable` / `ApprovableFor` interfaces preserved v0.11.1 behaviour exactly —
   the three old approval tests pass unchanged — and left the declarative rule
   engine free to stay coarse (per-tool) for now.
3. **Collapse three outcomes into a struct the caller can't misread.** Instead of
   an enum the agent must switch on, `Decision` exposes `Denied()` and
   `NeedsApproval()`, and the risk→approval threshold lives in exactly one place.
   A caller physically cannot forget the "deny" case or apply the threshold
   inconsistently.
4. **A new dependency in a zero-dep-ethos repo is a decision, not a reflex.** We
   chose YAML (`yaml.v3`) over stdlib JSON for policy rules *because the files are
   human-authored* — native comments explain *why* a rule exists, and the policy
   ecosystem (K8s / Docker / OPA) is YAML. Worth one dependency; recorded here so
   the next contributor knows it was weighed, not defaulted into.

## [0.11.1] - 2026-07-22 — Course: Lesson 11 (embedding provenance & observability), bilingual

A docs-only release. The `v0.11.0` bug hunt becomes a lesson: the course gains its
**twelfth** entry (00–11), and the first one drawn from a *real bug fixed in the
project's own history* rather than a planned layer.

### Added

- **Lesson 11 — "When memory silently forgets: embedding provenance & observability"**
  (`docs/lessons/11-when-memory-forgets/`), in 🇬🇧 English and 🇫🇷 French. An advanced
  (Level 3, ~75 min) 🔍 exploration pinned to `v0.11.0`: it retraces the recall failure
  caused by an embedding-model swap, reads the provenance guard (`provenance.go`) and the
  `/debug` toggle (`debug.go`), and has the reader run `TestProvenanceStaleThenReEmbed`,
  `talunor --reembed`, and a live `/debug` recall ranking.
- Course index (EN + FR) updated to 00–11; Lesson 10's capstone now points forward to
  Lesson 11 as a real-world encore.

### Lessons learned

1. **The best lessons are post-mortems.** A planned-curriculum lesson teaches a concept;
   a lesson reconstructed from a real bug teaches the *instinct* ("nothing errored, so
   distrust everything") that no feature list conveys. Mining the CHANGELOG's own
   "Lessons learned" for teaching material keeps the course honest and growing.
2. **`lessons-check` shapes how a historical lesson can cite code.** Its guard rejects a
   `git diff vA vB -- path` when `path` is absent at `vA`, so a lesson about a *new* file
   pins to the tag where it first exists (`v0.11.0`) and says "read it here" rather than
   diffing across its birth — the drift alarm doubles as an accuracy constraint.

## [0.11.0] - 2026-07-21 — Memory integrity (embedding provenance) & in-session observability (`/debug`)

Layer 11 hardens the memory substrate against a silent, nasty failure and makes the
agent's hidden decisions watchable from inside a session.

The trigger was a real bug hunt: an agent that "forgot" who the user was. It turned
out its memories had been embedded by a *different build* of the embedding model than
the one now on disk (the model is fetched from a mutable URL; the checksum pin only
arrived in v0.9.1). An embedding is only comparable with vectors from the **same**
embedding stack — swap the model and old vectors quietly land in a different space, so
KNN still runs but distances become meaningless and recall of older memories degrades
with no error. Nothing detected it.

### Added

- **Embedding provenance guard** (`internal/memory/provenance.go`). A `meta` side-table
  stores a fingerprint of the embedding stack — a fixed **canary** sentence embedded
  and kept as a vector, plus the model name and dimension. Every `Open` re-embeds the
  canary and compares: match → `ProvenanceOK`; mismatch → `ProvenanceStale`; a
  pre-provenance database with existing memories → `ProvenanceUnknown`. `Store.ReEmbed`
  recomputes every stored vector with the current model and re-stamps the fingerprint.
- **`talunor --reembed`** runs that migration with progress and exits; the app prints a
  one-line **startup warning** (and `/mem` shows the status) when provenance is not OK,
  pointing at the fix. `doctor` now reports the model + provenance too.
- **`/debug [on|off]`** — a runtime toggle that streams the loop's otherwise-invisible
  decisions (recall ranking with per-hit distances, reflection results) into the
  transcript as dimmed notes, in both the TUI and the `--plain` REPL. It rides the
  existing `Reasoning` channel (no renderer changes) and complements the file/stderr
  `TALUNOR_DEBUG` trace.

### Lessons learned

1. **An embedding is meaningless without its model's identity.** Vectors from two model
   builds share a dimension and a table but not a space; comparing across them yields
   plausible-looking distances that are quietly wrong. Storing a fingerprint *in the
   database* and checking it on open is the difference between a caught error and months
   of "recall feels off." A canary vector is a cheap, model-agnostic fingerprint: it
   detects *any* change to the stack (model file, config, extension), not just a renamed
   file.
2. **Fetch immutability is a data-integrity property, not just a supply-chain one.** The
   root cause was a model pulled from a mutable `resolve/main/` URL before checksums
   existed. The v0.9.1 checksum pin protects *forward*; it can't fix vectors already
   written by the old file. Pin fetched artifacts by digest from day one.
3. **The best debugging tool is the one already wired.** The agent had traced recall and
   reflection to a log file since v0.9.1, but nobody watching a live session could see
   it. Surfacing the *same* events inline behind `/debug` turned a multi-step forensic
   dig (copy the DB, write probes, re-embed by hand) into a one-command look. Build the
   toggle, not a new subsystem.
4. **A single pinned connection forbids nested queries.** `ReEmbed` must read all rows
   into memory and close the cursor *before* embedding, because `SetMaxOpenConns(1)`
   (needed for per-connection model state) means an open `rows` iterator would deadlock
   the `Embed` queries.

## [0.10.10] - 2026-07-21 — doctor surfaces the extension versions

A small developer-experience touch on the memory smoke test: `doctor` now prints
the loaded **sqlite-ai** and **sqlite-vector** extension versions right after the
store opens, so you can see *which* build of the C extensions you are running. The
corpus also gains two mountain facts (Matterhorn, Mont Blanc) and a matching recall
query, giving the semantic-search demo a denser, more convincing cluster to rank.

### Added

- `Store.VersionAI` / `Store.VersionVector` (`internal/memory/memory.go`): thin
  wrappers over the extensions' `ai_version()` / `vector_version()` SQL functions.
- `cmd/doctor` prints both versions, and a third recall query ("famous mountains in
  Europe") over two new corpus entries.
- Reference-doc links in the doc comments for `Embed` and `Recall`
  (sqlitecloud.io API references).

### Lessons learned

1. **"Nothing displays" is almost always a stale binary, not a bug.** New output
   that lives above the first screen of a long run is easy to miss in a scrollback
   terminal (VS Code opens at the bottom) — and a `./bin/doctor` compiled before the
   change shows nothing at all. `make doctor` uses `go run`, so it always reflects
   the source; reach for it before debugging phantom failures.
2. **Extension versions are cheap observability.** The C extensions are fetched and
   pinned by `make deps`, but nothing in the app told you *which* version was live.
   A one-line `SELECT ai_version()` closes that gap and makes bug reports precise.

## [0.10.9] - 2026-07-17 — Course fully bilingual: French translation complete (09–10)

The last two (advanced) translations land, and with them the course is **completely
bilingual** — all 11 lessons plus the index exist in both 🇬🇧 English and 🇫🇷 French.

### Added

- **French translations** (`README.fr.md`) for Lessons 09 (SSRF / secure web fetching)
  and 10 (the sandbox capstone, including the course-completion recap). The 🇬🇧↔🇫🇷
  switcher is now on every EN lesson.
- The FR index status flips from "in progress" to complete.

### Lessons learned

1. **A translation is a second review in disguise.** Doing the French pass end to end
   forced a fresh read of every lesson and caught real issues the English pass had left
   (e.g. Lesson 01's stale "Next" link, fixed in v0.10.6). Two languages, two chances to
   notice.
2. **Keep the translation drift-guarded like the code.** Because `lessons-check` scans
   the `.fr.md` files too — pinned tags, cross-links, and `git diff` paths — the French
   lessons are held to the same "references must resolve" bar as the English ones, for
   free.

## [0.10.8] - 2026-07-17 — Course: French translation, batch 3 (loop & contribution lessons 05–08)

The largest translation batch: the agent loop plus the three 🛠️ contribution lessons.
A French-speaking learner can now go index → Lesson 08 entirely in French — every
lesson up to (but not including) the two advanced security ones.

### Added

- **French translations** (`README.fr.md`) for Lessons 05 (agent loop), 06 (build a
  tool), 07 (deterministic tests), 08 (observability & errors). Go snippets, `git`
  commands, and identifiers stay verbatim; prose is translated. Each EN lesson gained
  the 🇬🇧↔🇫🇷 switcher.
- French course coverage is now **00–08** (10 of 12 files); only the advanced lessons
  09–10 remain.

## [0.10.7] - 2026-07-17 — Course: French translation, batch 2 (substrate lessons 02–04)

Continues the French translation with the substrate arc — memory, semantic recall,
the LLM provider — so a French-speaking beginner can now go from the index through
Lesson 04 entirely in French.

### Added

- **French translations** (`README.fr.md`) for Lessons 02 (persistent memory), 03
  (semantic recall & embeddings), and 04 (LLM provider & streaming). Code snippets,
  commands, and `make doctor` output are kept verbatim (language-neutral); prose is
  translated. Each EN lesson gained the 🇬🇧↔🇫🇷 switcher.
- French course coverage is now **00–04** (6 of 12 files, index included); 05–10
  remain.

## [0.10.6] - 2026-07-17 — Course: French translation begins (on-ramp: index, 00, 01)

The course is beginner-facing and the author's audience is French-speaking, so it
now ships **bilingual**. English stays canonical; each lesson gains a `README.fr.md`
alongside its `README.md`, added lesson by lesson.

### Added

- **French translations** of the course on-ramp: the index, **Lesson 00** (course
  navigation), and **Lesson 01** (first contact). Each file carries a language
  switcher at the top (🇬🇧 ↔ 🇫🇷); cross-links point to the sibling directory (so
  GitHub serves the English `README.md` by default and the switcher flips language),
  which keeps every link valid while the translation rolls out.

### Fixed

- **Lesson 01's "Next" link** pointed at Lesson 05 — a leftover from when the course
  was a 3-lesson pilot and 02–04 didn't exist yet. It now points to Lesson 02. (A
  small drift the translation pass surfaced — exactly the kind of stale cross-link a
  second read catches.)

### Lessons learned

1. **Translate the on-ramp first.** A beginner who can't read the *index* and the
   *first two lessons* in their language never reaches lesson three. Ship the entry
   path, then the rest — the same incremental discipline as the code.
2. **Keep links language-neutral during a rolling translation.** Cross-links point to
   the *directory*, not to `README.fr.md`, so nothing breaks while only some lessons
   are translated; the top-of-page switcher handles language. `lessons-check` already
   validates those links in the French files too.

## [0.10.5] - 2026-07-17 — Learning course complete: the advanced security lessons (09–10)

The final two lessons — both **advanced** — and with them the course is **complete:
all 11 lessons (00–10)**. They cover Talunor's two security surfaces, and the
capstone lands the idea the whole project has been building toward.

### Added

- **`docs/lessons/09-secure-web-fetching/`** *(🔍 `v0.10.0`, advanced)* — SSRF: why
  a URL allowlist isn't enough, why the guard checks the IP **at connect time**
  (DNS-rebinding-safe) and on every redirect, and how a security decision written
  as a *pure, table-tested function* (`blockedIP` / `guardDial`) is a joy to verify.
  Includes an optional 🛠️ hardening (add `0.0.0.0/8`).
- **`docs/lessons/10-understand-the-sandbox/`** *(🔍 `v0.9.0`, advanced, capstone)* —
  the two sandbox backends compared honestly, and the course's central idea: **a
  guardrail's worth is inseparable from an honest account of where it stops** (the
  `namespaces` backend says of itself "teaching artifact, not a strong boundary").
  Ends with a "you've finished the course" recap of the seven things the learner can
  now do.
- Course index flipped from *pilot* to *complete*; README banner now advertises the
  full 11-lesson course.

### Lessons learned

1. **End on the idea, not the hardest mechanism.** The capstone isn't "how Linux
   namespaces work" — it's *honesty about limits*. The most transferable thing a
   security-minded codebase teaches is to name where a guardrail stops, and Talunor
   models that in code you can read.
2. **A course is finished when it can replace a mentor.** The success test set at the
   start — run it, explain it, follow a turn, add a tool, test it, reason about its
   security, justify a trade-off — is now reachable end to end by reading `docs/lessons/`
   alone. That was the goal.

## [0.10.4] - 2026-07-17 — Learning course: the contribution & quality lessons (06–08)

The course's first **🛠️ current-contribution** lessons — where the learner stops
reading history and changes the *live* project on a branch off `main`.

### Added

- **`docs/lessons/06-build-your-first-tool/`** — implement the `tools.Tool`
  interface from scratch (a `unit_convert` tool), register it, and table-test it —
  learning that a new capability is an *extension*, never a change to the agent core.
- **`docs/lessons/07-test-without-a-real-llm/`** — deterministic agent testing with
  a `scriptedProvider`: drive a tool call → observation → final answer with no
  network, and test the very tool built in Lesson 06.
- **`docs/lessons/08-observability-and-errors/`** — a real live case: the
  best-effort `_, _ = a.store.Remember(...)` for the assistant turn. Turn the silent
  failure into an observable one via `a.trace` / `TALUNOR_DEBUG`, and learn
  "non-blocking ≠ invisible" (plus what must never be logged).

Course status: lessons 00–08 ready (9/11); 09–10 (advanced: SSRF, sandbox) planned.

### Lessons learned

1. **The `main`-based lessons are the drift-prone ones — reference by pattern, not
   line.** Unlike the historical lessons (pinned to immutable tags), 06–08 track the
   current code, so they point at things by *searchable pattern*
   (`grep "_, _ = a.store.Remember"`) and tell the reader that if the code has since
   moved, the principle still holds — studying the diff *is* the lesson.
2. **A great exercise threads through several lessons.** The `unit_convert` tool
   built in 06 becomes the thing tested in 07 — the learner exercises the tool
   interface *and* the testing pattern on the same concrete artefact.

## [0.10.3] - 2026-07-17 — Learning course: the substrate lessons (02–04)

Extends the course (started in v0.10.2) with the three lessons that come *before*
the agent loop, so a beginner meets each substrate on its own before seeing them
combine: **memory**, then **semantic recall**, then **the LLM**.

### Added

- **`docs/lessons/02-persistent-memory/`** *(🔍 `v0.2.0`, beginner)* — the SQLite
  store as an infrastructure boundary: `Open`/`Close` lifecycle, the schema, and
  the short-term ring buffer vs the long-term store (what survives a restart).
- **`docs/lessons/03-semantic-recall/`** *(🔍 `v0.2.0`, advanced)* — embeddings,
  cosine distance, KNN, and the `maxDistance` threshold, read straight from the
  `make doctor` output (why *"French landmark"* recalls *Eiffel Tower*).
- **`docs/lessons/04-llm-provider-and-streaming/`** *(🔍 `v0.3.0`)* — the small
  `llm.Provider` interface, streaming a reply over a channel, and a **compiling**
  fake provider (the trick behind deterministic agent tests). The signature is
  verified against `v0.3.0`, so — unlike a plausible-looking draft — it actually
  builds.
- The new `make lessons-check` guard (added just before these) validated every
  pinned tag and cross-link while they were written.

### Lessons learned

1. **Teach the substrate before the system.** Splitting "memory", "meaning", and
   "the model" into their own lessons — each on the tag where it first appears —
   lets a beginner build one idea at a time, so Lesson 05 (the loop) lands as
   *"oh, these three click together"* rather than a wall.
2. **A drift guard pays off immediately when *authoring*, not just at release.**
   `lessons-check` caught tag/link mistakes as the lessons were written — the same
   verify-against-reality discipline, now automated for the course.

## [0.10.2] - 2026-07-17 — A hands-on learning course (`docs/lessons/`)

The project's whole point is teaching, so it now has an actual **guided course**
that turns the tag-by-tag history into a path a Go **beginner** can walk: check out
an early tag to read a layer when it was small, understand one idea, then come back
to `main`. This release ships the **pilot** — three lessons validating the format
before scaling.

### Added

- **`docs/lessons/`** — a course index plus three pilot lessons:
  - **00 — How to use this course**: git navigation, "detached HEAD = read, don't
    commit", and the two lesson kinds (🔍 *historical exploration* vs 🛠️ *current
    contribution*).
  - **01 — First contact & first win**: an offline win first (`make doctor`, no
    Ollama), then the `v0.1.0` seed (memory only), then the interactive agent.
  - **05 — Follow the agent loop**: the *minimal* loop at `v0.4.0`, then its growth
    shown by a `git diff v0.4.0 v0.7.0` the learner runs themselves.
  - Every lesson has learning objectives, a files-at-this-tag map, an experiment,
    and a completion checklist. Historical lessons pin to **immutable tags**, so
    the "read this code" parts can't drift; only the reference docs (read on `main`)
    and the few `main`-based contribution lessons need upkeep.
- Referenced from the README (banner + Layout) and `docs/atlas.md`.

### Lessons learned

1. **A tag-per-layer history is a curriculum waiting to happen.** The discipline of
   "one layer = one immutable tag" (kept since `v0.1.0`) means a lesson can send a
   learner to *exactly* the code as it was, forever. That immutability is the
   drift-resistance the docs on `main` don't have — lean into it.
2. **Verify teaching material against the code, like everything else.** Drafting the
   lessons surfaced real errors in the outline: a command that used `cmd/talunor` at
   `v0.1.0` (it doesn't exist before `v0.4.0`), and an example `Provider` with a
   signature that wouldn't compile. For a beginner, code that doesn't run is worse
   than no code — every snippet and tag was checked against the actual repo.
3. **Docs grow with the code, and beginners must be told so.** `AGENTS.md` only
   exists from `v0.6.0`, `docs/atlas.md` only on the latest tags — so lessons read
   the *reference docs on `main`* and the *code at the tag*, and each historical
   lesson carries its own small map of that tag.

## [0.10.1] - 2026-07-16 — Patch: two fixes surfaced by a cross-model review

Five different LLMs were asked to review the repo; cross-checking their findings
against the actual code (and discarding the confident hallucinations) left two
real, verified defects — fixed here with tests. A nice lesson in itself: the most
fluent report missed the security issue a plainer, grounded one caught, and no
single model was complete — only verification against the code was.

### Security

- **Recalled memories are now framed as untrusted data (persistent prompt
  injection).** `agent.buildMessages` injected recalled memories into a **system**
  message — but their content originates from earlier user input and LLM-extracted
  facts, so a stored memory like *"ignore all previous instructions…"* was placed
  at system authority and could be obeyed on a later recall. The block is now
  fenced (`<recalled_memories>…</recalled_memories>`) and prefixed with an explicit
  instruction to treat everything inside as untrusted DATA, never as instructions.
  Textual mitigation (not a hard guarantee), covered by
  `TestRecalledMemoriesFramedAsUntrusted`.

### Fixed

- **Assistant text emitted before a tool call is no longer lost.** In `runLoop`,
  when the model produced text *and then* requested a tool in the same turn, the
  message fed back carried only the `ToolCalls` — the `Content` was dropped, so a
  "thinking out loud" model would see its own reasoning vanish from the history on
  the next call. The assistant tool-call message now carries `Content` too (and a
  chunk bearing both text and tool calls no longer drops its text). Covered by
  `TestAssistantContentBeforeToolCallPreserved`.

### Lessons learned

1. **Fluency is not completeness.** Across five model reviews, the best-written and
   most precise report missed the highest-impact finding (the memory→system
   injection) that a plainer, grounded review caught — and a model that *declared*
   it couldn't read the code still emitted a confident, fully-scored report built
   from hallucination. The only reliable filter was checking each claim against the
   real code (`grep` for the identifiers, read `buildMessages`, confirm the line).
2. **Retrieved context is an injection surface.** The moment memory content re-enters
   the prompt — especially at system authority — it must be treated as untrusted
   input, exactly like any other external data. RAG and agent memory inherit the
   whole prompt-injection threat model.

## [0.10.0] - 2026-07-16 — Layer 10: `web_fetch`, the network opt-IN

The agent gains the counterweight to the network-off bash sandbox: a tool that
reaches the internet **under a tight leash**. Where bash needs a *kernel* boundary
(it runs untrusted code), `web_fetch` needs an *application-layer* policy — the
fetched bytes never execute, they are handed to the model as text, so the real
risks are **SSRF** (tricking the agent into hitting an internal service) and
**resource abuse** (huge/slow responses). This layer defends against both. It is
**off by default** (`TALUNOR_WEBFETCH=1`) and **approval-gated**.

### Added

- **`internal/webfetch`** — a guarded HTTP fetcher.
  - **SSRF guard.** Rather than resolve → check → connect (which leaves a
    DNS-rebinding window), the guard runs inside the dialer's `Control` hook,
    which fires with the *actual resolved address* right before connect — so the
    IP vetted is the IP dialled, on the initial request **and every redirect**.
    `blockedIP` (a pure, table-tested function) refuses loopback, private (RFC1918
    + ULA), link-local (incl. the `169.254.169.254` cloud-metadata address), CGNAT
    (RFC6598), unspecified, and multicast — failing closed on anything it can't
    classify.
  - **Limits** (`DefaultLimits`): 10s timeout, **512 KiB** body cap (`io.LimitReader`
    + truncation flag), 5 redirects, http+https only (other schemes — `file`,
    `gopher`, `data`, … — rejected). Non-text content-types are reported by
    metadata only, so binaries never flood the model's context.
- **`tools.WebFetch`** — the tool: `{url}` schema, formats the fetch into an
  observation (final URL + status + content-type + capped body). Wired in
  `cmd/talunor` behind `TALUNOR_WEBFETCH`; the address guard applies unconditionally.
- **`tools.ApprovableFor`** — a finer-grained approval interface: a tool decides
  **per call, from its arguments**, whether a human prompt is needed. `web_fetch`
  uses it so hosts on `TALUNOR_WEBFETCH_ALLOW` skip the prompt (the SSRF guard
  still applies — the allowlist bypasses the *prompt*, never the *guard*). The
  agent consults `ApprovableFor` before the coarse `Approvable`; `bash` keeps the
  simple one. This is the first taste of the Iteration-3 arg-level policy.
- **Env**: `TALUNOR_WEBFETCH`, `TALUNOR_WEBFETCH_ALLOW` (comma-separated hosts;
  exact or leading-dot sub-domain match), `TALUNOR_WEBFETCH_MAX_BYTES`,
  `TALUNOR_WEBFETCH_TIMEOUT` — documented in `.env_sample`, README, AGENTS.md.

### Lessons learned

1. **Different threats, different boundaries.** bash and web_fetch look like
   siblings ("dangerous tools behind the gate") but need opposite defences: a
   kernel sandbox for *executing* untrusted code, an application-layer IP policy
   for *reaching* untrusted networks. Naming the threat first is what tells you
   which tool to reach for.
2. **Check the IP you dial, not the IP you resolved.** The naive SSRF guard
   resolves a host, checks the IP, then connects — and a hostile DNS can change
   the answer in between (rebinding). Enforcing inside the dialer's `Control` hook
   closes the gap, and it covers redirects for free (each hop dials afresh).
3. **The allowlist bypasses the prompt, not the guard.** Keeping those two
   concerns separate is the whole safety story: a "trusted" host that resolves to
   `169.254.169.254` is still refused. Conflating them would turn a convenience
   into a hole.
4. **`internal` packages make loopback tests awkward — lean into it.** `httptest`
   serves on 127.0.0.1, which the real guard blocks, so the guard is a pure
   function table-tested in isolation and the `Client` takes an injectable policy
   (permissive for happy-path tests; loopback-only-relaxed to prove a redirect to
   an internal address is still refused). The separation makes both halves clearer.

## [0.9.1] - 2026-07-16 — Patch: bounded tool loop, prompt history, observability & hardening

A hardening + quality-of-life patch on top of Iteration 2, working through the
"quick wins" of a technical review of the repo.

### Fixed

- **Tool loop no longer ends a turn silently.** When the model kept requesting
  tools past `MaxToolIters`, the loop exited with no final answer, stored nothing,
  and showed the user nothing. It now stops as soon as the tool budget is spent
  (without wasting a final, unread round of tool calls) and emits an explicit
  terminal error so the failure is visible. Covered by `TestToolLoopExhaustion`
  (`internal/agent`).
- **Honest `agent.New` contract for `RecallMaxDistance`.** `New`'s doc claimed
  *all* zero-valued config fields fall back to `DefaultConfig`, but this one is a
  deliberate exception: `0` is a meaningful value (keep all `k` matches, no
  thresholding), so it is intentionally *not* defaulted. Clarified both the field
  doc and `New`'s doc rather than silently changing recall behaviour for anyone
  relying on the documented `0`. (`cmd/talunor` sets `0.75` via `DefaultConfig`.)

### Security

- **`make deps` now verifies checksums.** The SQLite extensions and embedding
  model are downloaded over the network and the `.so` files run as **native code
  inside the process with no sandbox**. Each artefact's SHA256 is now pinned and
  checked after download (via a small `verify_sha256` make macro); a mismatch
  deletes the file and fails the build. This turns "whatever the URL serves today"
  into "exactly the bytes we reviewed". Regenerate the pins when bumping a
  `*_VERSION` (command in the `Makefile`).
  - Adding the checks immediately caught a real one: a flaky HuggingFace 504 made
    `curl -sL` save a tiny HTML error page *as* the model, which then failed the
    hash. Downloads now use `curl -f … --retry` so an HTTP error fails loudly and
    is retried, instead of silently poisoning `ext/`. (The `.so` release assets are
    immutable by tag; the model tracks a mutable `main` ref — noted in the Makefile.)
- **The container no longer runs as root.** The runtime image moves to the
  distroless `:nonroot` tag (uid 65532); `/data` is seeded with that ownership so
  a named volume stays writable without privilege. A bug in a loaded extension or
  a tampered model can no longer act as root on a bind-mounted host path. A host
  bind-mount must itself be writable by uid 65532 (named volumes just work).

### Documentation

- **Version examples no longer pin a stale release.** The container tag, the
  standalone-bundle commands, and the `make doctor` sample output used a hard
  `v0.5.7` / `v0.2.0`, which read as "the current version" to newcomers. They now
  use a `vX.Y.Z` placeholder with a pointer to the Releases page. (The iteration
  table keeps its real per-layer completion versions — those are history, not a
  "run this" example.)
- **Documented the GitHub Actions pinning policy.** A new "Supply chain & CI"
  README section explains the deliberate split: third-party actions are pinned by
  commit SHA (a mutable `@v4` tag can be repointed at malicious code), while
  first-party `actions/*` are intentionally left on moving tags — a conscious
  exception, not an oversight. Closes the review's "inconsistent pinning" item by
  making the reasoning explicit rather than churning the workflows.

### Added

- **`TALUNOR_DEBUG` — a debug/trace mode for the loop's invisible decisions.**
  With it set, the agent emits a structured (`log/slog`) trace of *recall* (each
  hit's id + cosine distance + kind), *tool* calls and results, and *reflection*
  (facts extracted / stored / skipped, and previously-silent extraction errors).
  It logs to a `talunor-debug.log` next to the DB by default (so the TUI's screen
  stays clean — `tail -f` it), or to `stderr`, or to a path. Off by default; the
  seam is a nil-able `agent.Config.Debug` so instrumentation call sites stay cheap.
- **`internal/history`** — a persistent, deduplicated prompt history. The TUI
  recalls earlier prompts with **↑/↓** like a shell; entries are kept **unique**
  (re-submitting a prompt promotes it to newest rather than duplicating) and
  persist across sessions in `history.jsonl`, stored next to the memory database.
  - Storage is **JSON-per-line** so multi-line prompts and special characters
    round-trip safely; writes go through a temp-file + rename so a crash can't
    corrupt existing history; the file is capped (oldest entries dropped first).
  - **↑/↓ now navigate history** in the TUI; transcript scrolling moves to
    **PgUp/PgDn** and **Ctrl-U/Ctrl-D** (the status bar hint was updated). Typing
    (not navigating) drops the history position, so the next ↑ starts fresh from
    the just-typed line, and the in-progress draft is restored when you ↓ past the
    newest entry. The plain REPL records prompts to the same file but cannot do
    ↑/↓ line editing (scanner-based input), so recall there is write-only.

### Lessons learned

1. **"Bounded autonomy" needs a visible edge.** A cap that silently swallows the
   turn teaches nothing and looks like a hang; the fix is not just the limit but
   surfacing *why* the turn stopped. Don't run work whose result no one will read
   — trip the cap *before* the final tool round, not after.
2. **Pick a storage format for the ugly input, not the pretty one.** A newline
   history file breaks the moment a prompt contains a newline; encoding each entry
   as JSON makes multi-line and special-character prompts a non-issue.
3. **Repurposing a key means re-teaching it.** Moving ↑/↓ from scroll to history
   is free to implement but silent to the user — the status-bar hint is part of
   the change, not an afterthought.
4. **Pin what you execute, not what you download.** The tarballs are deleted after
   extraction; the thing that actually runs in the process is the extracted `.so`.
   So the checksum guards the `.so`/`.gguf` directly — hashing the tarball would
   verify a file we throw away and never load.
5. **A zero value is an API decision, not a default.** Four numeric config fields
   treat `0` as "unset, use the default"; one treats `0` as a real setting ("no
   threshold"). The bug was never the behaviour — it was a doc comment that
   flattened the distinction. Fixing the words beats forcing a fifth field into a
   consistency it doesn't want.
6. **Documented bugs are part of the curriculum.** This release keeps the silent
   tool-loop bug *visible* — a failing-then-passing test and a written account of
   how it was caught — because in a teaching repo, *how a defect was found and
   fixed* is as instructive as the code that ships.
7. **`curl` without `-f` is a foot-gun.** By default `curl` exits 0 and writes the
   server's error page to your output file, so a transient 504 becomes a corrupt
   "artefact". The checksum caught it here — but the real fix is `curl -f` so the
   download fails where the failure happens, not three steps later.
8. **Observability is a teaching surface, not just an ops one.** A nil-able
   `slog` seam turns the loop's silent choices (why *this* memory recalled, why
   reflection stored nothing) into a readable trace — cheap to add, and it makes
   the "invisible" middle of the agent legible to a learner without a heavy stack.
9. **Test the cold path, not the warm one.** The checksum edit accidentally
   dropped the `EMBED_MODEL` Make variable, so `make deps` silently stopped
   fetching the model — invisible locally because the file already existed and the
   fast-path reported "nothing to do". Only the release runner's clean checkout
   caught it. When a change touches a build's *fetch* step, exercise it from
   empty. (Follow-up: the release workflow now caches `ext/`, so third-party
   assets are fetched once and reused, not re-downloaded on every tag.)

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

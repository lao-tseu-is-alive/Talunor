# Talunor architecture — the mental model

**Language:** 🇬🇧 English · [🇫🇷 Français](architecture.fr.md)

This page is the **map you read before the territory**: one turn of the cognitive
loop, how the packages fit together, and the handful of decisions that give the
system its shape. It is deliberately short.

- For a **file-by-file** index, see [`docs/atlas.md`](atlas.md).
- For the **why, taught step by step**, see the [course](lessons/) (26 lessons).
- For **contributor conventions** (release ritual, gotchas), see [`AGENTS.md`](../AGENTS.md).

Talunor is a terminal AI agent with a full cognitive loop
(perceive → recall → reason → act → learn) over a multi-tier memory. The one idea
worth carrying through everything below: **a reliable agent is not a smarter LLM;
it is a system where actions, memory, trust, and learning each cross an explicit,
verifiable boundary.**

---

## 1. One turn of the loop

A single `Agent.Turn` runs the loop synchronously and streams the reply to the
user; **learning is handed off to the background and the turn ends**. Tool calls
pass through a policy gate (and, when risky, a human y/n) before they run.

```mermaid
flowchart TD
    U(["User message"]) --> R["Recall — KNN over embeddings,<br/>distance gate, rank by similarity·confidence·salience"]
    R --> RF["Reinforce recalled memories<br/>(salience ↑, decay clock reset)"]
    RF --> BM["Build prompt: system +<br/>fenced memories (untrusted DATA) +<br/>short-term turns + input"]
    BM --> SU["Store user turn"]
    SU --> CHAT

    subgraph react["Reason + Act — ReAct loop, capped at MaxToolIters"]
      direction TB
      CHAT["provider.Chat(with tools)"] --> DEC{"tool call<br/>requested?"}
      DEC -->|no| ANS["Final answer<br/>streams live to the user"]
      DEC -->|yes| POL{"policy.Evaluate"}
      POL -->|"deny → observation"| CHAT
      POL -->|"risk ≥ medium"| APP{"Human y/n"}
      POL -->|allow| EXE["tools.Execute"]
      APP -->|yes| EXE
      APP -->|"no → observation"| CHAT
      EXE --> OBS["Observation fed back"] --> CHAT
    end

    ANS --> SA["Store assistant turn"]
    SA --> ENQ["enqueueReflect(input)<br/>turn ends here — stream closes"]
    ENQ -. "background, off the critical path" .-> W

    subgraph bg["Reflection — single background worker"]
      W["reflectWorker: distil durable facts"] --> KF{"already known?<br/>RecallForConsolidation"}
      KF -->|yes| RC["ReinforceFact — consolidate:<br/>salience always, confidence only<br/>on independent evidence"]
      KF -->|no| RM["RememberFact —<br/>provenance + system confidence"]
    end

    R -->|reads| DB[("SQLite — one pinned connection<br/>sqlite-vector + sqlite-ai (cgo)")]
    SU --> DB
    SA --> DB
    RC --> DB
    RM --> DB
```

Reading it in words:

1. **Perceive & recall.** The user's message is embedded and matched against
   long-term memory (brute-force KNN), gated by cosine distance, then ranked by
   `similarity · confidence · effective-salience`. Recalled memories are
   *reinforced* (being useful is a signal they matter).
2. **Reason.** The prompt is assembled: the system prompt, the recalled memories
   **fenced as untrusted DATA** (a prompt-injection mitigation), the recent
   short-term turns, and the new input.
3. **Act.** The model may ask for tools. Each call is wrapped as a one-step plan
   and sent to `policy.Evaluate`: a **deny** becomes an observation (fail closed),
   a **risky** call pauses for human approval, an **allowed** call runs. The
   observation is fed back and the model is called again, up to `MaxToolIters`.
4. **Store.** The user turn is recorded immediately; the assistant turn once the
   reply has streamed.
5. **Learn — later.** The turn does **not** wait for learning. It enqueues the
   user message and returns; a single background worker distils durable facts and
   either stores a new one or *consolidates* a restatement onto the existing row.

> **Planner variant** (`TALUNOR_PLANNER=1`): before acting, the agent produces an
> explicit, inspectable plan, the human approves the whole plan, and then this same
> ReAct loop runs **capped to the plan's tools** — the model cannot call a tool the
> approved plan didn't name.

---

## 2. How the packages fit together

The internal packages form a **DAG — no import cycles**. The cognitive core
(`agent`) depends on the substrate; the substrate never depends back. The seams
between layers are **interfaces**, which is what makes each layer testable in
isolation with a fake.

```mermaid
flowchart TD
    subgraph front["Front-ends / presentation"]
      TUI["tui"]
      RENDER["render"]
      HIST["history"]
    end
    subgraph core["Cognitive core"]
      AGENT["agent"]
      POLICY["policy"]
      TOOLS["tools"]
      PLAN["plan"]
    end
    subgraph infra["Substrate (leaves)"]
      LLM["llm"]
      MEM["memory"]
      SANDBOX["sandbox"]
      WEBFETCH["webfetch"]
    end
    CAL["calibration<br/>(off the chat path)"]

    TUI --> AGENT
    TUI --> LLM
    RENDER --> LLM
    AGENT --> LLM
    AGENT --> MEM
    AGENT --> POLICY
    AGENT --> TOOLS
    AGENT --> PLAN
    POLICY --> PLAN
    POLICY --> TOOLS
    TOOLS --> MEM
    TOOLS --> SANDBOX
    TOOLS --> WEBFETCH
    CAL --> LLM
    MEM --> EXT[("sqlite-vector + sqlite-ai<br/>+ GGUF model (cgo)")]
```

Arrows read **"imports / depends on."** Things worth noticing:

- **`plan` sits at the bottom.** It is a pure vocabulary (`Plan`, `PlanStep`,
  `RiskLevel`) shared by `policy` and `agent`, deliberately dependency-free so
  there is no `policy ↔ agent` cycle.
- **The seams are interfaces**, each with a fake in tests: `llm.Provider`,
  `policy.Policy`, `tools.Tool`, `sandbox.Sandbox`, `agent.Planner`,
  `agent.FactExtractor`, `agent.FactArbiter`.
- **`tools` is the only thing that touches `sandbox` and `webfetch`.** The agent
  never reaches the network or the shell directly — it goes through a tool, which
  goes through a guarded boundary.
- **`calibration` is off the chat critical path.** It measures a model's
  reliability; it does not run during a turn. The link back is one number
  (`ModelConfidence`), not a code dependency.
- **`memory` is where cgo lives.** The two SQLite extensions and the GGUF
  embedding model are loaded here; that C state is per-connection (see §3.1).

---

## 3. The load-bearing decisions

Eight choices explain most of the code. Each links to the lesson that teaches it.

### 3.1 One pinned SQLite connection *is* the concurrency model
The `sqlite-ai` / `sqlite-vector` extensions keep the loaded model, embedding
context, and vector index in **per-connection** C state, so `memory.Store` pins
the pool to a single connection (`SetMaxOpenConns(1)`). This is not a limitation
worked around — it is *used*: `database/sql` serialises every access, so lazy
decay stays a pure read and the async reflection worker needs **no extra lock
around the store**. Note the scope: this buys serialisation of *database* access
only. In-process state still needs the usual care, and the code says so —
`atomic.Bool`/`atomic.Pointer` for state shared between the UI and turn
goroutines, `sync.RWMutex` for the shutdown handoff. "No mutexes anywhere" would
be a misreading.
→ Lessons [02](lessons/02-persistent-memory/) and [19](lessons/19-off-the-critical-path/).

### 3.2 Trust comes from the source, never the model's self-report
A memory's `confidence` is assigned by the **system** from its provenance
(`user_stated` and a verified `tool_observed` are both trusted; `model_inferred`
is not), then scaled by the model's measured calibration — the model is never
asked how sure it is (a sycophancy trap). Reinforcement raises confidence **only
on independent evidence** (a user restating counts; the model echoing itself does
not).
→ Lessons [16](lessons/16-measure-the-model/) and [17](lessons/17-learning-with-humility/).

### 3.3 Authority is a function of (who spoke, what it is about) — not a ranking
Confidence says how much a fact is believed. **Authority** — who may *retire*
someone else's fact — is a separate question, and it is deliberately **not** a
linear rank. A global ordering like "user > tool > model" is the design this
project tried and rejected: it breaks in both directions (see ADR 0003's worked
examples). `memory.Supersedes` therefore reads an **`Attribution` = (provenance,
subject)** and asks two questions in order:

1. **Different subjects never supersede.** A claim about the *user* and a claim
   about the *world* cannot contradict each other; they coexist. This is
   arithmetic, not a judgement call — it holds even when both LLM steps misbehave.
2. Within one subject, authority decides: `user_stated` about the **user** = 2,
   `tool_observed` = 2 (*equal*, not below), `unspecified` = 1,
   `model_inferred` = 0 — and `user_stated` about the **world** = 0. You are the
   authority on yourself, not on the world; saying it does not make it so.

A refused correction is not discarded: it is recorded as **counter-evidence**,
which makes the incumbent report `Contested` — a status **derived** from the
evidence trail, never stored beside it, so it cannot drift from its own
justification.
→ Lessons [21](lessons/21-whose-word-counts/), [24](lessons/24-the-adr-that-didnt-bind/)
and [25](lessons/25-the-scar-that-never-bled/); ADRs
[0003](decisions/0003-trust-model-for-supersession.md),
[0004](decisions/0004-subject-as-data.md),
[0005](decisions/0005-contested-claims.md).

### 3.4 Every fact carries an auditable trail, with two sides
`RecordEvidence` appends one row per store and per reinforcement — which turn,
which source — so "the agent believes X (90%)" becomes "…because you said so in
turns #3 and #9" (`/why <id>`). Since Layer 24 the trail also holds what
*contradicted* a fact and lost. Facts distilled from a tool's text are
`model_inferred` by default: an LLM reading a tool's output is still inference.
`tool_observed` is reserved for a tool that declares itself `tools.Verified` —
**no builtin does today**; it is a wired and tested seam, not a claim.
→ Lesson [20](lessons/20-learn-from-action/); ADR [0002](decisions/0002-provenance-from-source.md).

### 3.5 Retention is computed at read time, not maintained by writes
Salience decays **lazily**: `Recall` computes effective salience
`= salience · 2^(−age/half-life)` at the moment of the query and soft-forgets
below a floor (the row survives). No background job, no write on the read path —
which is exactly what the single-connection design (§3.1) needs.
→ Lesson [18](lessons/18-the-memory-of-the-gesture/).

### 3.6 Every action crosses an explicit, fail-closed gate
Before any tool runs, `policy.Evaluate` decides allow / prompt / deny. A policy
error or a denial **fails closed** (the model observes a refusal, it does not act).
A whole-plan approval binds the tool *names*, but a high-risk step still
re-confirms its **live arguments** — approving a plan is not a blank cheque.
→ Lessons [12](lessons/12-the-open-bar/) and [14](lessons/14-the-approval-that-didnt-bind/).

### 3.7 Danger is opt-in — and each guard says which kind of guard it is
The powerful tools are **off by default** (`TALUNOR_BASH`, `TALUNOR_WEBFETCH`).
When on, they sit behind guards of **three different strengths**, and the
difference is load-bearing — stating it is the point of this section, not a
disclaimer at the end of it:

| Guard | Strength | What that means |
|---|---|---|
| `bash` via the **OCI runtime** (nerdctl/docker) | **Boundary** | A real container: `--cap-drop=ALL`, `--security-opt=no-new-privileges`, non-root, no network. Use this for code you do not trust. |
| `bash` via the **namespaces backend** (rootless Linux) | **Defense-in-depth — NOT a boundary** | Rootless user namespaces, read-only rootfs, empty netns, a memory rlimit and a hard timeout. But there is **no seccomp**, and rootless gives **no reliable pids cap**. It is teaching material and a speed bump. **Do not run hostile code behind it.** |
| `web_fetch` SSRF guard | **Boundary** | The check lives in the dialer's `Control` hook, so it vets the **resolved IP** immediately before connect and re-checks on every redirect — which is what defeats DNS rebinding. |
| Fenced recalled memory | **Mitigation** | Untrusted text is delimited and labelled as DATA in the prompt. It reduces prompt injection; it cannot *prevent* it, because the model still reads it. |

The honest summary: two boundaries, one mitigation, and one guard whose value
depends entirely on which backend is selected. `TALUNOR_SANDBOX` chooses; auto-detect
falls back to `namespaces` when no container daemon answers — so *check which one you
got* before trusting it (`make capabilities`).
→ Lessons [09](lessons/09-secure-web-fetching/) and [10](lessons/10-understand-the-sandbox/).

### 3.8 Learning runs off the critical path
Reflection is a second LLM call; making it synchronous would hold the reply open.
Instead the turn hands the message to a bounded queue and ends; one background
worker learns behind it. One worker + the single pinned connection means the
worker's writes are serialised against a turn's reads for free.
→ Lesson [19](lessons/19-off-the-critical-path/).

---

*Next: pick a thread from the [course](lessons/), or read the substrate first in
[`internal/memory/store.go`](../internal/memory/store.go) and the loop in
[`internal/agent/turn.go`](../internal/agent/turn.go).*

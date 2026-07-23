# Lesson 13 — Plan before you act: from emergent ReAct to a plan you can read

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🔍 Historical exploration** (reading the `v0.13.0` code, with 🛠️ runs on `main`) ·
Level 3 (advanced) · ~90 min

## Why this lesson exists

Everything the agent has done so far, it did *emergently*. In the ReAct loop
(Lesson 05) you only learn what Talunor will do by watching it do it: it calls a
tool, sees the result, decides the next call, and so on. That is powerful and
adaptive — but it means the plan lives only in the model's head, one step ahead of
you, and the first time you see step three is when it happens.

Lesson 12 gave the agent a **guardrail** (the policy). This lesson gives it
**forethought**: an optional planner that writes the whole plan down *first* — a
structured list of steps you can read, approve, or refuse before a single tool
runs. It is the capstone of Iteration 3, and it turns "trust me, I'll figure it
out" into "here is exactly what I intend to do."

The interesting engineering is in two places: getting a *reliable structured plan*
out of a fundamentally unreliable text generator, and *executing that plan safely*
without throwing away the adaptivity that made ReAct good.

## Learning objectives

By the end you can:
- contrast **emergent** (ReAct) and **deliberate** (plan-first) execution, and say
  what each trades away;
- explain how Talunor coaxes a **valid JSON plan** out of an LLM — a strict
  contract, tolerant extraction, validation, and a retry that feeds the error back;
- trace the planned turn — **plan → policy pre-screen → whole-plan approval → capped
  execution → learn** — and explain the *structural cap* that keeps it safe;
- choose an approval mode (`plan` / `step` / `highrisk`) and predict its prompts;
- say why a planning failure is a *downgrade*, not a dead end.

## Prerequisites

- **Lesson 05 (the agent loop)** — you must know the ReAct loop this builds on.
- **Lesson 12 (the policy)** — the planner leans on the policy to screen a plan.
- **Lesson 07 (test without a real LLM)** — the experiments include deterministic
  tests that plan and execute with no model.

## Part 1 — two ways to reach a goal

Ask Talunor "what is 12 × 8, use the calculator". Two architectures answer it very
differently.

**Emergent (ReAct, the default).** The model is handed the tools and starts
talking: it decides *now* to call `calculator`, sees `96`, then decides *now* to
answer. The sequence is discovered as it goes. If a tool result surprises it, it
adapts on the spot. But you cannot inspect the sequence in advance — there isn't
one yet.

**Deliberate (plan-first, `TALUNOR_PLANNER=1`).** The model is first asked for a
*plan*: a JSON object listing the steps. Only once that plan exists — and you have
seen it — does anything execute. You trade some adaptivity (the plan is decided up
front) for something valuable: the actions become **inspectable and approvable as a
whole**, before any of them happen.

Neither is "better". ReAct is nimble; planning is legible and controllable. Talunor
ships both and lets you switch with one environment variable — precisely so you can
feel the difference. The rest of this lesson is about how the deliberate path works.

> **The core idea.** Emergent execution reveals its plan by acting. Deliberate
> execution states its plan, then acts. The second is worth building when *seeing
> the plan first* — to approve it, cap it, or refuse it — matters more than
> adapting mid-flight.

## Part 2 — getting a plan out of an LLM (read `planner.go` at `v0.13.0`)

This is the current layer. If `main` has moved on, read it as it landed:

```bash
git checkout v0.13.0        # detached HEAD — read only (see Lesson 00)
```

Open:

```text
internal/agent/planner.go
```

An LLM emits *text*, not data structures. Getting a dependable `plan.Plan` out of
one is a four-part discipline you'll reuse in any structured-output task:

1. **A strict contract.** `planSystemPrompt` tells the model to reply with *only* a
   JSON object of a precise shape, lists the available tools, and states the rules
   (every step needs a rationale; a tool step names a listed tool; end with a final
   step). A narrow, machine-checkable contract is what makes the reply safe to act
   on — the same instinct as the fact extractor's rigid prompt in Lesson 05.
2. **Tolerant extraction.** Models wrap JSON in prose and ` ```json ` fences.
   `decodePlan` doesn't fight that with a fragile regex: it finds the first `{` and
   hands the rest to a `json.Decoder`, which reads exactly **one** JSON value and
   ignores the trailing text — and correctly handles braces inside strings. Robust,
   in five lines.
3. **Validation beyond parsing.** Valid JSON is not a valid plan. `decodePlan` runs
   `plan.Validate()` (structure, unique ids, resolvable `depends_on` — Lesson 12's
   `plan` package), then adds the two checks only the agent can make: every tool
   step names a **known** tool, and the plan **ends in a final step**.
4. **A retry that teaches.** If any check fails, the planner re-asks — but it feeds
   the exact error back and echoes the bad reply, so the model *corrects* rather
   than *repeats*. One retry (`maxPlanAttempts = 2`) is enough for a capable model;
   more just burns tokens on one that can't comply. And crucially, the planner
   **never executes a tool** — it only produces the plan.

That is the whole reliability story: a tight prompt, forgiving extraction, real
validation, and a self-correcting retry.

## Part 3 — executing a plan safely (read `execute.go`)

Open:

```text
internal/agent/execute.go
```

Find `runPlanned`. It is the planned turn, in four phases that mirror the cognition
model — **plan → gate → execute → learn**:

1. **Plan.** Ask the planner. If it fails, don't abort the turn: fall back to the
   plain ReAct loop so the user still gets an answer. *Planning is an enhancement,
   not a single point of failure.*
2. **Policy pre-screen.** Evaluate every tool step against the policy (Lesson 12).
   A single **denied** step blocks the *whole* plan before anything runs — fail
   closed, with an explanation.
3. **Whole-plan approval.** The human sees the entire plan — the exact tools and
   arguments — and approves it once. This is the key UX shift from Lesson 12's
   per-call gate: you consent to the *plan*, not to each step in isolation.
4. **Capped execution.** Here is the load-bearing trick. Execution *reuses the ReAct
   loop* (`reactLoop`, extracted so the plain and planned paths share one trusted
   core) — but it offers the model **only the tools the plan named**
   (`toolSpecs(exec.allowTools)`). The model literally cannot call a tool the
   approved plan didn't include, because it never sees it.

> **Why the cap is not optional.** A blanket "yes" to a plan would be *weaker* than
> per-tool approval if the model could then call anything. The safety of whole-plan
> approval rests entirely on execution staying inside the plan. Talunor enforces
> that at the API surface — the offered tool list — not by asking the model to
> behave. Enforce boundaries where they can't be argued with.

You can see the loop split directly:

```bash
git diff v0.12.0 v0.13.0 -- internal/agent/agent.go
```

`runLoop` became a thin entry point over a shared `reactLoop`, which now takes an
`execCtx` carrying the tool cap and whether the whole-plan approval already stands
in for per-step prompts.

**Approval modes** (`TALUNOR_APPROVAL`, default `plan`) tune the human-in-the-loop:

| mode | whole-plan prompt | tool cap | per-step risky prompt |
|------|-------------------|----------|-----------------------|
| `plan` | yes, once | yes | no (the plan approval is the consent) |
| `step` | yes, once | yes | yes (belt and braces) |
| `highrisk` | no | no | yes (the plan is advisory; behaves like Lesson 12) |

The policy's **deny** is enforced in every mode. When you're done reading, return:

```bash
git switch main
```

## Part 4 — watch it plan

First, the deterministic path — no model needed (Lesson 07):

```bash
go test ./internal/agent/ -run 'Planner|Planned|DecodePlan' -v
```

Read those tests next to the code: a valid plan, a retry-then-succeed, `decodePlan`
tolerating prose and fences, and the planned turn approving / denying / rejecting a
plan and falling back on failure.

Now live (needs Ollama). Use `highrisk` first so a low-risk calculator plan runs
without any prompt:

```bash
TALUNOR_PLANNER=1 TALUNOR_APPROVAL=highrisk go run ./cmd/talunor --plain
```

```text
you> what is 12 * 8? use the calculator.
📋 Plan:
goal: what is 12 * 8? use the calculator.  (confidence 1.00)
  1. [tool] calculator({"expression": "12 * 8"}) — compute the product
  2. [final] — report the result
🔧 calculator({"expression":"12 * 8"})
   ↳ 96
The result of 12 multiplied by 8 is 96.
you> /plan
```

`/plan` re-prints the last plan. Now feel the **whole-plan approval**: restart with
the default mode and give it a task that uses a tool:

```bash
TALUNOR_PLANNER=1 go run ./cmd/talunor --plain   # TALUNOR_APPROVAL defaults to plan
```

This time, before anything runs, you're asked to approve the whole plan — answer
`n` and watch it refuse to proceed. Finally, see the **policy** block a plan before
execution. Write a rule file that denies the calculator (Lesson 12):

```bash
printf 'rules:\n  - tool: calculator\n    action: deny\n    reason: no math today\n' > deny.yaml
TALUNOR_PLANNER=1 TALUNOR_POLICY=./deny.yaml go run ./cmd/talunor --plain
```

Ask for a calculation: the plan is produced, the pre-screen sees the denied step,
and the turn ends with an explanation — the tool never ran, and you were never even
asked to approve. Deny beats plan.

## The principles

```text
Emergent execution reveals its plan by acting; deliberate execution states it first.
```

1. **Plan-level approval is only as safe as the cap that keeps execution in-plan.**
   Enforce the boundary at the API surface (the offered tools), not by trusting the
   model to stay on script.
2. **Reliable structured output is a discipline, not a prompt.** Strict contract →
   tolerant extraction → real validation → a retry that feeds the error back.
3. **Reuse the loop you trust.** The planner didn't replace ReAct; execution reuses
   the same core, changing only which tools are offered and how approval is asked.
4. **Design failure as a downgrade.** A bad plan falls back to plain ReAct; the user
   still gets an answer.
5. **Deliberate and emergent are both valid — ship the switch.** `TALUNOR_PLANNER`
   lets you run the same prompt both ways and choose per situation.

## Completion checklist

- [ ] I can contrast emergent (ReAct) and deliberate (plan-first) execution and name
      the trade-off.
- [ ] I read `planner.go` and can list the four parts of getting a valid plan from
      an LLM.
- [ ] I can explain why `decodePlan` uses a `json.Decoder` instead of a regex.
- [ ] I read `runPlanned` and can state its four phases.
- [ ] I can explain the structural cap and why whole-plan approval depends on it.
- [ ] I ran the planner tests, and ran the agent live with a plan (and saw `/plan`).
- [ ] I saw a policy `deny` block a plan before execution.
- [ ] I returned to `main`.

---

## 🎓 About this lesson

This closes **Iteration 3**: the agent now has both a guardrail (the policy) and
forethought (the planner). Notice what the plan gave you that ReAct could not — a
single artifact to *inspect, approve, cap, and refuse*. That is the recurring shape
of safe autonomy: make the intent explicit, then constrain the execution to it.

The honest limits are worth remembering — and one of them turned out to be a real
security gap: at `v0.13.0` the whole-plan approval bound the tool *names* but not the
*arguments* the executor ran, so **Lesson 14 is a post-mortem of exactly that**, and
its fix. Talunor's cap is **structural** (only planned tools are offered), not
**semantic** (it doesn't judge whether an in-plan call drifted from the intent), and
it does not re-plan when a step surprises it.
Those — plus letting you hand-edit a plan before it runs — are deferred to later
increments, and each is a fine lesson waiting to be written. Next comes Iteration 4:
learning — consolidating memory, and learning from the plans the agent has run.

Back to the [course index](../).

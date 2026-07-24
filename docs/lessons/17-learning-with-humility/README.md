# Lesson 17 — Learning with humility: what a memory is worth

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🔍 Exploration + hands-on** (reading `internal/memory` and `internal/agent` on `main`) ·
Level 3 (advanced) · ~75 min

## Why this lesson exists

Since Lesson 05 Talunor has *remembered*: it distils durable facts from what you say
and recalls them later. But a memory is only useful if you know *how much to trust
it*. A fact the user stated plainly ("my name is Carlos") deserves more weight than
one a model *inferred*, and either deserves less weight when the model doing the
distilling is itself unreliable.

Iteration 4 is about learning, and its first move (Layer 16) is the humble one:
before making the agent learn *more*, make it learn *honestly*. Every memory now
records **where it came from** and **how much to trust it** — and, crucially, that
trust is assigned by the *system* from the source, never self-reported by the model.
This lesson is about that mechanism, why each choice is the way it is, and how it
closes the loop with the calibration you built in Lesson 16.

## Learning objectives

By the end you can:
- explain why a stored memory needs **provenance** and **confidence**, not just text;
- state the load-bearing rule — **confidence comes from the source, never the
  model's self-report** — and why self-reported confidence is a trap;
- describe the **calibration link**: how a learned fact's confidence is scaled by the
  model's measured reliability, and why it's wired as a decoupled scalar;
- read how a new column reached the schema through an ordered **migration**;
- set the knobs and watch a fact be learned with (and recalled by) confidence.

## Prerequisites

- **Lesson 05** (the agent loop) and the reflection step it introduced.
- **Lesson 16** (measure the model) — this consumes a calibration score.
- **Lesson 12 / 15** — the "don't trust the model's own judgement" instinct, now
  applied to the model's own *confidence*.

## Part 1 — a memory is more than its text

Read the shape of a memory on `main`:

```text
internal/memory/memory.go
```

Find `Provenance` and `BaseConfidence`. A `Provenance` says where a memory came from:

- `user_stated` — grounded in the user's own words (a user turn, or a fact distilled
  from what the user said);
- `model_inferred` — the model produced it (an assistant turn, or an inference);
- `tool_observed` — a verified tool result;
- `unspecified` — legacy or unclassified.

`BaseConfidence(p)` maps each to a starting trust: a verified tool result (0.95)
outranks a user statement (0.9), which outranks a model inference (0.5). These are
the *source's* worth, before anything else.

Now — where did the `provenance` and `confidence` columns come from? They weren't in
the original table. Read:

```text
internal/memory/migrate.go
```

This is Layer 15's machinery in action. The schema evolves through an **ordered,
append-only** list of migrations; the applied version is a single integer in the
`meta` table. Migration 1 is the baseline (the `memories` table); **migration 2**
adds `provenance` and `confidence`. Each migration runs once, in its own
transaction with its version stamp, so upgrading an existing database is automatic
and crash-safe. The one rule that makes this trustworthy: *append only — never
reorder, renumber, or edit a shipped migration; fix a mistake with a new one.* A
migration list is a history every database in the world has already run.

## Part 2 — the load-bearing rule: confidence from the source, not the model

Here is the decision that matters most, and it is easy to get wrong. When the agent
distils a fact, how sure should it be? The tempting answer is to *ask the model*:
"how confident are you in this fact?" **Don't.**

> **The core idea.** A model's self-reported confidence is *not calibrated* — it is
> the same fluent, plausible output as everything else it says, and (Lesson 15) a
> model is often most confident when it is most wrong. Confidence you *asked for* is
> just another claim. So Talunor never asks. Confidence is assigned by the **system**
> from *which pipeline produced the memory*: a fact distilled from the user's message
> is `user_stated`; a turn from the assistant is `model_inferred`. Objective, not
> self-graded.

Read how reflection stores a fact — `reflect` in:

```text
internal/agent/agent.go
```

Notice it calls `store.RememberFact(ctx, fact, ProvenanceUserStated, conf)` — the
provenance is fixed by the pipeline (facts come from the user's message), and `conf`
is computed by the agent, not returned by the model. This is the same principle as
Lesson 12's policy and Lesson 15's verification, turned inward: *don't trust the
model's judgement about its own reliability — decide it from the outside.*

## Part 3 — the calibration link

A `user_stated` fact starts at 0.9 confidence. But it still passed through the
*extraction model*, which could have confabulated a "fact" you never stated. How
much should that temper the confidence? Exactly as much as the model is
**measurably** unreliable — which is what Lesson 16's calibration gives you.

Read the scaling in `reflect`:

```go
conf := clamp01(memory.BaseConfidence(memory.ProvenanceUserStated) * a.cfg.ModelConfidence)
```

`Config.ModelConfidence` (from `TALUNOR_MODEL_CONFIDENCE`, default 1.0) is a scalar in
[0,1] — the model's reliability, which you obtain from a `cmd/calibrate` run's overall
pass-rate. A model that scored 0.7 on your suite → set `TALUNOR_MODEL_CONFIDENCE=0.7`
→ its learned facts land at `0.9 × 0.7 = 0.63` instead of `0.9`.

> **Why a scalar, not an API.** The agent does *not* run calibration — that would
> couple two subsystems and slow every turn. It consumes a *number* an operator sets
> from a separate `calibrate` run. The whole "learning informed by calibration"
> promise, delivered by one multiplication and zero coupling. The cheapest
> integration between two systems is often a number, not a call.

And the other side: `Config.RecallMinConfidence` (`TALUNOR_RECALL_MIN_CONFIDENCE`,
default 0 = off) drops recalled memories below a threshold, so a low-confidence
"fact" is not fed back into the prompt as if it were established. Learn cautiously;
recall cautiously.

## Part 4 — watch it learn with humility

First, the deterministic tests — no model needed:

```bash
go test ./internal/memory/ -run 'Fact|Migrate' -v
```

Read `TestFactProvenanceAndConfidence` alongside the code: it stores a fact with an
explicit provenance and confidence and checks recall carries them, and confirms a
turn's provenance is derived from its role.

Now live (needs Ollama). Run with a *deliberately* low model confidence, tell the
agent a durable fact, then inspect memory:

```bash
TALUNOR_MODEL_CONFIDENCE=0.5 go run ./cmd/talunor --plain
```

```text
you> my name is Carlos and I work in Go
you> /list
```

`/list` shows the learned fact with `(user_stated 45%)` — base 0.9 × your 0.5. Set
`TALUNOR_MODEL_CONFIDENCE=1.0` (or leave it unset) and the same fact is learned at
90%. You have just made the agent *doubt* what it learns in exact proportion to how
much you trust the model.

Finally, close the loop with Lesson 16: run `cmd/calibrate` against your model, take
the overall pass-rate, and set `TALUNOR_MODEL_CONFIDENCE` to it. Now the number
isn't a guess — it's *measured*. Optionally set `TALUNOR_RECALL_MIN_CONFIDENCE=0.5`
and watch low-confidence facts stop coming back in recall.

## The principles

```text
Learn in proportion to what you can trust; never let the learner grade its own trust.
```

1. **A memory is metadata, not just text.** Provenance (source) and confidence
   (trust) are what let recall — and the model — weight it.
2. **Confidence comes from the source, never the model's self-report.** Asked-for
   confidence is just another uncalibrated claim.
3. **Measured reliability scales what you learn.** A calibration score, consumed as a
   decoupled scalar, keeps an unreliable model's facts from gaining authority.
4. **Evolve the schema by appending a migration, never by editing a shipped one.**

## Completion checklist

- [ ] I can name the four provenances and explain why tool > user > model inference.
- [ ] I can explain why the agent never asks the model how confident it is.
- [ ] I read `reflect` and can point to the `BaseConfidence × ModelConfidence` line.
- [ ] I can explain why calibration is wired as a scalar, not an API call.
- [ ] I read migration 2 and can state the append-only rule.
- [ ] I ran the agent with `TALUNOR_MODEL_CONFIDENCE=0.5` and saw the reduced confidence.
- [ ] I can explain what `TALUNOR_RECALL_MIN_CONFIDENCE` guards against.

---

## 🎓 About this lesson

This is the first learning lesson of Iteration 4, and it deliberately leads with
*humility*: the agent gains the ability to remember with graded trust before it
gains the ability to remember more. Notice the arc it completes — Lesson 11 caught a
substrate degrading silently, Lesson 16 measured a model's reliability, and this
lesson *spends* that measurement, letting it govern what the agent is allowed to
believe. The next layer builds directly on this metadata: **salience and decay** —
not "how much to trust a memory" but "which memories to keep, strengthen, or let
fade." Trust first; retention next.

Back to the [course index](../).

# 3. A memory's trust model is an explicit, per-domain policy — not "the user is always right"

- **Status:** Accepted (implemented in Layer 21, `v0.20.0`).
- **Date:** 2026-07-28
- **Relates to:** Layer 16 (provenance + confidence), Layer 17 (the independence rule),
  Layer 20 (learn from action), ADR 0002 (provenance from source).

## Context

Layer 21 lets a new fact **supersede** (retire) an older, incompatible one, so memory can
*correct* itself and not just accumulate. That raises the dangerous question: **who is
allowed to correct whom?** The naive answer — a single global rank, "user > tool >
model" — is wrong, and two worked examples show it fails in *both* directions:

- **The flat earth.** A user says "the earth is flat." If `user_stated` globally outranks
  everything, this overwrites a correct world fact. But the user is authoritative about
  *themselves*, not about the world — so a global "user wins" corrupts the memory.
- **The attack signature.** A Verified intrusion-detection tool observes "signature X is
  mitigated by behaviour Y." This is genuine, high-authority evidence that *should* be able
  to retire a stale, model-inferred belief. But if we react to the flat-earth case by making
  memory "a user model only / never store world facts," we lose exactly this — a real agent
  must remember what it observed.

A single global rank satisfies neither. What both share: **authority is per-domain**, and a
fact's *provenance is a proxy for its authority in that domain*.

## Decision

1. **Supersession is decided by two separate things.** An **arbiter** (an LLM step, see
   `internal/agent`) decides the *relationship* — RESTATES / SUPERSEDES / UNRELATED — with a
   hard rule: `SUPERSEDES` requires the two facts to be about the **same subject** and
   **incompatible**. The **trust model** (`memory.Supersedes`) then decides whether that
   proposed supersession is *allowed*. The model proposes; the system gates.

2. **The trust model is one small, named, documented function** — `memory.Supersedes(newer,
   older Provenance) bool` — deliberately in a single place so it can be read, tested, and
   *consciously owned*. A different agent (security, ops, research) replaces **that function
   and nothing else**. The default (personal-assistant) policy:
   - `user_stated` and a Verified `tool_observed` are **authoritative** and may retire beliefs
     of equal-or-lower authority.
   - `model_inferred` is **never** authoritative enough to retire a belief on the strength of
     the model's own guess (the Layer-17 independence rule, extended from *confidence* to
     *truth*).

3. **The flat earth is handled by attribution + the same-subject rule, not by the gate.** A
   user's world-claim is stored as a **belief about the user** ("User believes the earth is
   flat") — the reflection prompt already frames facts as "User …". A belief-about-the-world
   and a world-fact are **different subjects**, so the arbiter returns `UNRELATED`: they
   coexist. The agent remembers your view of the world without adopting it as *the* world.

4. **The attack signature is handled by the `tools.Verified` tier.** A Verified tool's
   observation is `tool_observed` (authoritative in its domain), so it *can* supersede a
   stale `model_inferred` belief. The seam built in Layer 20 (ADR 0002) is exactly the hook.

5. **Supersession is soft** (Layer 21, D3): the retired fact is marked (`superseded_by`) and
   excluded from recall, but the row survives for audit (`/why`) and reversal.

## Consequences

- **A trust model you can read.** "Whose word counts" is ~10 lines you decide, not an
  assumption scattered through the code. That is the point — and the anchor of Lesson 21.
- **Under-claiming by default.** A model inference that contradicts a user fact is *dropped*,
  not stored as a rival; the authoritative belief stands. False positives are bounded to
  same-or-lower-authority beliefs, and soft-supersession makes any wrong call reversible.
- **Extensible without special cases.** Both the flat-earth and attack-signature cases fall
  out of the *same* machinery (arbiter + provenance-as-authority + `Verified` + the gate) —
  no domain classifier, no truth oracle.

## Alternatives rejected

- **A single global provenance rank** ("user always wins"): rejected — fails the flat-earth
  case (corrupts world facts).
- **"Memory is a user model; never store world facts"**: rejected — fails the attack-signature
  case (a real agent must remember authoritative observations).
- **A domain/truth classifier in the gate** ("is this a self-fact? is it true?"): rejected —
  the agent can't be a truth oracle without becoming the over-confident thing we avoid; the
  subject-match rule + belief-attribution get the same result far more cheaply.

## References

- [`internal/memory/supersede.go`](../../internal/memory/supersede.go) — the trust model
  (`Supersedes`) + soft-supersession.
- [`internal/agent/arbiter.go`](../../internal/agent/arbiter.go) — the relationship classifier.
- [`internal/agent/agent.go`](../../internal/agent/agent.go) — `learnOneFact` (proposes → gates).
- Lessons [16](../lessons/16-measure-the-model/), [17](../lessons/17-learning-with-humility/),
  [20](../lessons/20-learn-from-action/); Lesson 21 (planned) teaches this ADR directly.

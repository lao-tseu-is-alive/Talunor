# Lesson 21 — Whose word counts? A trust model is a decision, not a default

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🔍 Exploration + hands-on** (reading `internal/memory`, `internal/agent`; the Layer 21
code shipped at `v0.20.0`, reference docs on `main`) · Level 3 (advanced) · ~65 min

## Why this lesson exists

Layer 20 taught the agent to *learn* from what it observes. Layer 21 gives it a more
dangerous power: to **correct** itself — a new fact can *retire* an old, contradicting one.
The moment memory can retire a belief, one question decides whether it stays trustworthy or
quietly rots:

> **Who is allowed to correct whom?**

The tempting answer is a single global rank — "the user outranks the model, the tool
outranks nothing," pick one order and apply it everywhere. This lesson is about why that
answer is a *trap*, why it breaks in **two opposite directions**, and why the fix is not a
cleverer rank but a small, explicit, *scoped* policy you decide on purpose. It is a meta
lesson, in the spirit of Lesson 15: the thing to carry away is a **habit of thinking**, not
a mechanism.

## Learning objectives

By the end you can:
- explain why a single global provenance rank fails in *both* directions (two worked examples);
- state the principle that resolves both — **authority is per-domain; provenance is a proxy
  for it** — and where the trust model lives in the code (one function);
- read `memory.Supersedes` and explain how the arbiter *proposes* while the trust model *gates*;
- explain how a user's false world-claim is remembered faithfully without corrupting a world fact;
- run a **checklist** against any agent-memory design *before* you build it — and prove, by
  flipping ~10 lines, that the trust model is load-bearing.

## Prerequisites

- **Lesson 16** (provenance & confidence) — provenance is assigned by the system, from the source.
- **Lesson 17** (learning with humility) — the independence rule; here it extends from
  *confidence* to *truth*.
- **Lesson 20** (learn from action) — where world/observation facts enter memory, which is
  what makes this question bite.

## Part 1 — the design that works (until it doesn't)

For a personal assistant, a global rank feels obviously right: **the user outranks the
model.** If the model guessed "User is a Go beginner" and the user says "I'm an expert," the
user wins. Correct. Read the tiers on `main`:

```text
internal/memory/memory.go   (Provenance, BaseConfidence)
```

`user_stated` above `model_inferred` — the user is the authority. For a while this is all
memory holds (facts *about the user*), and the rank is never wrong. Then Layer 20 let world
and observation facts in, and the rank meets reality.

## Part 2 — it breaks in BOTH directions

Two examples. A single rank cannot satisfy both — that is the whole point.

**The flat earth.** The user says "the earth is flat." If `user_stated` globally outranks
everything, this **overwrites a correct world fact**. But the user is authoritative about
*themselves*, not about the world. A global "user wins" *corrupts* the memory.

**The attack signature.** A Verified intrusion-detection tool observes "signature X is
mitigated by behaviour Y." This is real, high-authority evidence that **should** be able to
retire a stale, model-inferred belief about that signature. If you react to the flat-earth
case by declaring "memory is a user model, never store world facts," you lose exactly this —
and a real agent must remember what it observed.

One example pushes you to *lower* the user's authority; the other pushes you to *keep* a
source's authority high. No single global order does both. The failure is not the order you
picked — it's that you picked **one order for all domains**.

## Part 3 — the principle: authority is per-domain

What both cases share: **authority depends on the claim's domain, and a fact's provenance is
a *proxy* for its authority *in that domain*.**

- The user is authoritative about **the user**, not the world.
- A Verified tool is authoritative about **what it observed**, not the user's preferences.
- The model is authoritative about **very little** on its own — its inferences are hypotheses.

So the trust model is not a global rank; it is a **policy** that says who counts *for what*.
And the honest engineering move is to put that policy in **one small, named, documented
place** — so it is a decision you can read, test, and own, not an assumption smeared across
the code.

## Part 4 — read the trust model (it is one function)

```text
internal/memory/supersede.go   (Supersedes, supersedeAuthority)
internal/agent/arbiter.go      (FactArbiter, the SUPERSEDES verdict)
internal/agent/agent.go        (learnOneFact — propose, then gate)
docs/decisions/0003-trust-model-for-supersession.md
```

Two things cooperate, and the split is the safety:

1. **The arbiter *proposes*.** An LLM step classifies a new fact against a near neighbour:
   `RESTATES` / `SUPERSEDES` / `UNRELATED`. `SUPERSEDES` requires the two to be about the
   **same subject** and **incompatible**.
2. **`memory.Supersedes` *gates*.** It answers only the authority question — may a source of
   provenance *newer* retire a fact of provenance *older*? The default:

   ```
   user_stated / tool_observed  → authoritative (may retire equal-or-lower)
   unspecified                  → may retire only the model
   model_inferred               → retires NOTHING (the humility rule)
   ```

The model *proposes*; the system *decides what is allowed*. A model inference that
contradicts a user fact is **dropped** — not stored as a rival — because the model doesn't
get to overwrite the user on the strength of its own guess.

And the flat earth? It never reaches the gate. The user's world-claim is stored as a
**belief about the user** ("User believes the earth is flat"), which is a *different subject*
from a world fact → the arbiter returns `UNRELATED`, and they **coexist**. The agent
remembers your view of the world without adopting it as *the* world. The attack signature
*does* reach the gate, as `tool_observed` from a Verified tool, and is allowed to retire the
stale belief. Same machinery, opposite outcomes — the sign the seam is drawn in the right place.

**Look at how few lines this is.** `Supersedes` + `supersedeAuthority` is the entire trust
model. A security or ops agent replaces *that* function — and nothing else.

## Part 5 — the checklist to carry (the real takeaway)

Before you build memory into any agent, answer these. Write the answers down — that act is
the point.

1. **What kinds of facts will this memory hold** — self, world, domain observations?
2. For each kind, **who is the authority** — and is it the *same* source across all kinds?
3. Is my provenance→trust mapping a **conscious policy**, or did I inherit "the user is
   always right"?
4. When source A contradicts source B, **who wins — and is that answer domain-dependent**?
5. Can a **low-authority source silently corrupt a high-authority fact**? *(the flat-earth failure)*
6. Can a **genuinely authoritative observation update a stale belief**? *(the attack-signature failure)*
7. **Where in my code does the trust model live** — one explicit place, or scattered and implicit?

If the answer to #7 is "scattered and implicit," you have a trust model — you just didn't
write it down. Write it down, and scope it.

## Part 6 — prove it is load-bearing

First, the tests encode the policy:

```bash
go test ./internal/memory/ -run 'Supersede' -v
go test ./internal/agent/  -run 'Supersede|Unrelated' -v
```

Read `TestSupersedeGateProtectsUser`: a `model_inferred` fact that contradicts a
`user_stated` one is **dropped**, and the user's fact stays active. Read
`TestUnrelatedStoresNew`: a nearby-but-unrelated fact is kept as its own row (the flat-earth
coexistence, generalised). `TestSupersedesTrustModel` is the whole policy in a table.

Now make the trust model *fail on purpose* — this is the exercise that makes it stick. In
`internal/memory/supersede.go`, edit `supersedeAuthority` so `ProvenanceModelInferred`
returns `2` (as authoritative as the user), then re-run:

```bash
go test ./internal/agent/ -run 'SupersedeGateProtectsUser' -v
```

It now **FAILS**: the model's inference is allowed to overwrite what the user said. You have
watched "whose word counts" flip — *in ten characters*. Revert the change. That one edit is
the lesson: the trust model is not decoration; it is the epistemics of your agent, and it is
yours to decide.

Live (needs Ollama): tell the agent a fact, then contradict it, and inspect:

```text
you> my favourite language is Python
you> actually my favourite language is Go now
you> /list       # the Python fact is marked ⚠→#N (superseded)
you> /why <the Python fact id>   # shows what retired it
```

## The principles

```text
A memory that can correct itself needs a trust model — and a global "user > model" rank is a hidden, broken one.
```

1. **A single global provenance rank is a hidden trust model, and it breaks both ways** —
   it corrupts world facts (flat earth) or forbids real observations (attack signature).
2. **Authority is per-domain; provenance is a proxy for it.** Who counts depends on *what*
   the claim is about.
3. **Put the trust model in one named, testable place.** "Whose word counts" is a decision
   you own, not an assumption smeared through the code.
4. **When the LLM gains a destructive power, gate it with a non-LLM rule and make it soft.**
   The arbiter proposes; the trust model gates; supersession is reversible.

## Completion checklist

- [ ] I can give two examples where a single global provenance rank fails, in opposite directions.
- [ ] I can state the resolving principle (authority is per-domain; provenance is a proxy).
- [ ] I read `memory.Supersedes` and can explain the propose-then-gate split with the arbiter.
- [ ] I can explain how a user's false world-claim is remembered without corrupting a world fact.
- [ ] I ran the checklist against a memory design (Talunor's, or my own).
- [ ] I flipped `supersedeAuthority` and watched `TestSupersedeGateProtectsUser` fail — then reverted.

---

## 🎓 About this lesson

This is a meta lesson, and its real subject is a mistake you will otherwise make *by
default*. Almost every "give the agent a memory" tutorial reaches for the obvious rank — the
user is the source of truth — because for a toy personal assistant it is never wrong. It
becomes wrong the instant the memory holds anything the user is not the authority on, which
is the instant the agent becomes interesting. The bug does not announce itself; it shows up
later as a memory confidently full of a user's misconceptions, or an agent that can't retain
what its own tools told it.

The habit this lesson wants to leave you with is small and durable: **before you let a memory
correct itself, decide — explicitly, and per domain — whose word counts, and put that
decision in one place you can read.** Talunor's answer is ten lines in `supersede.go`; yours
will be different, because your agent is for something different. The lesson is not the ten
lines. It is that you *wrote them on purpose*.

Next, Iteration 5 turns from *who to trust* to *what you can find*: **Lesson 22** (planned)
adds hybrid recall — vector *and* lexical — so the exact identifier, the rare name, the
number you'd miss by meaning alone can still be retrieved.

Back to the [course index](../).

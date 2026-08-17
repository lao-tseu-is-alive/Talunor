# 4. Authority is a function of (who spoke, what it is about) — so the subject must be data

- **Status:** Accepted (implemented in Layer 23, `v0.22.0`).
- **Refines:** [ADR 0003](0003-trust-model-for-supersession.md) (the trust model for supersession).
  Its decision stands; this ADR replaces the *mechanism* behind its point 3.
- **Date:** 2026-08-17
- **Relates to:** Layer 16 (provenance + confidence), ADR 0002 (provenance from source),
  Layer 20 (learn from action), Layer 21 (contradiction & supersession).

## Context

ADR 0003 established that **authority is per-domain**: a user is authoritative about
themselves, a Verified tool about what it observed, and neither about everything. Its
point 3 explained how the flat-earth case is contained without a truth oracle:

> *"A user's world-claim is stored as a belief about the user ("User believes the earth is
> flat") — the reflection prompt already frames facts as "User …". A belief-about-the-world
> and a world-fact are different subjects, so the arbiter returns UNRELATED."*

Every word of that describes **two LLM calls**. The extraction prompt asks for the
attribution; the arbiter judges the subjects. `parseFacts` accepts any non-empty line, so
nothing enforced the first. `memory.Supersedes` saw only `Provenance`, so nothing could
enforce the second. The deterministic gate — the part of the design that is supposed to be
the backstop — had no representation of the domain at all.

The failure is therefore reachable, and was reproduced before this layer was written:

| Existing fact | New fact (from the user's message) | Result before Layer 23 |
|---|---|---|
| `The earth is round.` · model_inferred | `The earth is flat.` · user_stated | **retired** |
| `Signature X is mitigated by Y.` · **tool_observed (Verified)** | `Signature X is harmless.` · user_stated | **retired** |

The second row is the sharp one: `supersedeAuthority` ranked `user_stated` and
`tool_observed` equally (2), and `2 >= 2` allows the retirement — so a user's assertion
retired exactly the observation ADR 0003 holds up as authoritative in its domain. It takes
both model steps failing at once, which is unlikely per turn and inevitable over a long
enough memory. A guarantee that holds only while two LLM calls behave is not a guarantee;
it is a habit.

A second, quieter problem: **the world-facts case was not reachable at all.** Every source
— including a security tool's output — was asked the same user-focused extraction
question ("what durable facts about the *user*?"). ADR 0003's attack-signature example
had no path by which such a fact could be learned in the first place.

## Decision

1. **A fact records what it is ABOUT, beside who stated it.** `memory.Subject` is
   `user` / `world` / `unspecified` (migration 6). `memory.Attribution{Provenance, Subject}`
   is the pair, and it is what the trust model reads. `RememberFact` takes an
   `Attribution`, not a `Provenance`, so every call site must name the subject.

2. **The subject is assigned by the SYSTEM, from the source — never read back out of the
   model's output.** This is ADR 0002's invariant applied to the second half of the
   credential, and it is enforced the same way: not by trusting an instruction, but by
   **asking a question whose answer can only be of one kind**. Reflection asks the user's
   message *"what is durably true about the USER?"* and a tool observation *"what does
   this state about the WORLD?"*, then stamps each answer with the question it answered.
   A model that ignores the framing and returns a bare world-claim to the user-question
   still gets `SubjectUser` — its authority is user-scoped, and no laundering occurs.

3. **Different subjects never supersede.** `Supersedes` checks `SameSubject` before it
   looks at authority. A claim about the user and a claim about the world are not rivals,
   so one cannot retire the other — by arithmetic, with no model in the loop. The
   candidate search (`agent.knownFact`) applies the same rule earlier still: a
   cross-subject neighbour is not a consolidation candidate, so the **arbiter is never
   consulted** for it. The fragile step is bypassed rather than trusted.

4. **Within one subject, Layer 21 is unchanged.** `model_inferred` retires nothing; a
   Verified tool retires stale inferences in its domain; the user corrects the model about
   themselves.

5. **The policy matrix states cells that today's sources cannot reach.** `user_stated`
   about the `world` scores 0 — "saying it does not make it so" — even though reflection
   never produces that pair (the user's message is only ever asked the user-question). A
   trust model is read as a *policy*: it must say what it would do, so that whoever adds a
   source (a "correct my knowledge base" mode, an imported document) reads the answer
   instead of discovering it.

6. **Legacy rows are not backfilled.** Every pre-Layer-23 row is `unspecified`, and
   `SameSubject` treats `unspecified` as comparable with everything, so those rows keep
   exactly their old (weaker, provenance-only) guarantee. Guessing their subject after the
   fact would be the model labelling stored data — the very laundering this layer exists
   to prevent — and freezing them (comparable with nothing) would make them permanently
   uncorrectable.

## Consequences

- **The per-domain claim is now mechanical.** ADR 0003's headline holds even when the
  extractor drops the attribution *and* the arbiter answers SUPERSEDES; the adversarial
  tests assert exactly that double failure, and fail if `SameSubject` is neutralised.
- **The attack-signature case became reachable.** Tool observations are now asked a
  world-facts question, so an observation about a system can be learned as such —
  previously it had no question that could return it.
- **One less model call on the cross-subject path**, and one less place where a wrong
  verdict matters.
- **A coarse taxonomy, deliberately.** Two subjects, not a topic ontology. Deciding what a
  claim is *really* about is the truth-oracle role ADR 0003 rejected; `user` vs `world` is
  the coarsest split that separates conviction from observation, which is the split the
  failure needed.
- **A tool that observes facts about the user** (a calendar, a device) is world-scoped
  today, so its facts coexist with the user's rather than correcting them. Conservative
  and honest; a tool declaring its own subject scope is the natural next seam, alongside
  `tools.Verified`.
- **An API break inside the module:** `RememberFact`, `Supersedes` and `FactExtractor.Extract`
  changed shape. That is the intended cost — the compiler now asks every call site the
  question the design turns on.

## Alternatives rejected

- **Enforce the "User …" wording in `parseFacts`** (rewrite or reject unattributed lines):
  cheap, but it makes safety depend on string surgery over model prose, and it silently
  discards or distorts real facts. The source already knows what was asked; parsing the
  answer to recover it is strictly worse information.
- **Have the model label each fact's subject.** Rejected for the same reason ADR 0002
  rejected model-reported provenance: a self-declared credential is not evidence, and one
  call that both extracts and labels can launder both fields at once.
- **Keep provenance-only and document the hole** (honest under-claiming): considered
  seriously. Rejected because the project's own lessons argue that a documented gap in a
  *safety* mechanism should be closed when the fix is ~15 lines of policy plus a column,
  and because the doc would have had to walk back ADR 0003's most-taught example.
- **A subject taxonomy** (`self`, `people`, `systems`, `documents`, …): more expressive,
  but each new value needs an authority rule per provenance, and the assignment stops
  being derivable from the source's question.

## References

- [`internal/memory/subject.go`](../../internal/memory/subject.go) — `Subject`, `Attribution`,
  `SameSubject`, and the source-shaped constructors (`UserSaid`, `Observed`).
- [`internal/memory/supersede.go`](../../internal/memory/supersede.go) — the trust model.
- [`internal/agent/reflect.go`](../../internal/agent/reflect.go) — one extraction question
  per subject.
- [`internal/agent/agent.go`](../../internal/agent/agent.go) — `reflect` / `learnFrom` /
  `learnOneFact`: assign, scope, gate.
- ADRs [0002](0002-provenance-from-source.md) and [0003](0003-trust-model-for-supersession.md);
  Lesson [21](../lessons/21-whose-word-counts/) teaches the trust model as a design decision.

# 5. A refused correction is evidence, not nothing — contested claims

- **Status:** Accepted (implemented in Layer 24, `v0.23.0`).
- **Extends:** [ADR 0003](0003-trust-model-for-supersession.md) (the trust model) and
  [ADR 0004](0004-subject-as-data.md) (subject as data). Both decisions stand; this ADR
  says what happens to the claims their gate **refuses**.
- **Date:** 2026-08-26
- **Relates to:** Layer 20 (the evidence trail), Layer 21 (supersession), Layer 17
  (lazy decay — the precedent for deriving rather than storing).

## Context

Layers 21 and 23 gave the memory a way to correct itself, and a deterministic gate
deciding who may correct whom. When the arbiter proposes `SUPERSEDES` and
`memory.Supersedes` refuses, the code does this:

```go
} else {
    // The trust model forbids it — the old belief is more authoritative than
    // this source. Drop the new fact rather than store a contradiction.
    a.trace("supersede.denied", ...)
}
```

The refusal is right. **Throwing the claim away is not.**

At that moment the system knows something it will never know again so cheaply: two
sources disagree about the same subject, one of them was not authoritative enough to
win, and *a specific piece of text* was the disagreement. All of it is discarded,
leaving a debug line that exists only if `TALUNOR_DEBUG` happens to be on.

This is **fail-closed on authority but fail-open on knowledge**. The consequence is
that Talunor's memory cannot represent the one state the whole epistemic-reasoning
vision is about: *knowing that something is disputed*. Every fact is either believed
or gone. There is no "believed, but challenged — and here is the challenge."

The vision document
([epistemic-reasoning-vision.md](../epistemic-reasoning-vision.md) §2, §12) asks for a
`Claim` carrying `counterEvidence[]` and a `status`. Roughly 70% of that structure
already exists — provenance, confidence, sources, evidence, supersession links. The
missing piece is precisely the one this drop-site destroys.

But the vision's proposed state machine (`Unexamined → Hypothesis → Supported →
StronglySupported → Established`) smuggles in the hard problem: **who moves the
token?** If the answer is "the LLM, when it feels confident," that is self-reported
confidence with extra steps — the trap [ADR 0002](0002-provenance-from-source.md)
exists to prevent. Any status this layer introduces must be moved by something the
system controls.

## Decision

**Record the refused correction as counter-evidence against the fact it failed to
retire, and derive "contested" from that evidence.**

1. **The evidence trail gains a polarity.** `evidence.polarity` is `supports` (every
   existing row, correctly) or `contradicts`. A contradicting row also carries
   `detail`: the text of the claim that was refused, so the disagreement can be read
   later. Migration 7; append-only, no backfill, no re-embed.

2. **The refused claim is NOT stored as a memory.** It exists only as the `detail` of
   a counter-evidence row. Storing it as a fact — even flagged — would make it
   recallable, which is exactly the authority the gate just denied it. A claim that
   lost the authority argument does not get to enter the prompt through a side door.

3. **`Contested` is DERIVED, never stored.** A fact is contested if and only if it has
   at least one `contradicts` row:

   ```sql
   EXISTS(SELECT 1 FROM evidence e WHERE e.fact_id = m.id AND e.polarity = 'contradicts')
   ```

   This answers "who moves the token?" without an arbiter of arbiters: **nobody moves
   it.** The status is a function of the evidence, computed at read time. It cannot
   drift from its own justification, because it *is* its justification. This follows
   Layer 17's lazy-decay precedent — the effective salience is likewise computed on
   read rather than written back — and for the same two reasons: one source of truth,
   and no writes on the read path (the store pins a single connection).

4. **A contested fact is still the belief, and still recalled** — flagged, not
   demoted. The gate decided the incumbent wins; contestation records that the
   question was raised, not that it was reopened. Recall marks it in the fenced
   memory block so the model can *say* the belief is disputed instead of asserting it
   flatly.

5. **Counter-evidence does not move confidence.** By construction the contradicting
   source is one the trust model judged insufficiently authoritative for this subject.
   Letting it lower confidence would hand it, by arithmetic, the influence it was
   just denied — a back door into exactly the authority question ADRs 0003 and 0004
   settle. Contestation is *visible*, not *corrosive*.

6. **`/why` shows both sides.** The evidence trail was already the audit surface; it
   now reports what supported a fact and what challenged it, with the challenging
   text and its source.

## Consequences

**Good.**

- The memory can represent a disputed belief, and explain the dispute — the narrow
  vertical the vision document asks for (§17), reached without a protocol, a second
  model call, or a new prompt.
- The trigger is a **disagreement between two mechanisms that already exist** (the
  arbiter's verdict and the trust gate's), so no new judgement is introduced. It is
  fully deterministic.
- Information that was being destroyed is now retained at its cheapest moment.
- `/why <id>` becomes materially more useful: an audit trail with only supporting
  rows can only ever tell a flattering story.

**Costs and limits.**

- **A recorded refusal is permanent, and it was made under one trust policy.**
  `Supersedes` is deliberately swappable — ADR 0003 exists so a security or research
  agent can replace those ~15 lines and nothing else. But a counter-evidence row is
  written at the moment of refusal, so a trail accumulated under the personal-assistant
  policy keeps asserting *that* policy's verdicts after the policy has been replaced.
  A fact contested under the old rules stays contested under the new ones, even where
  the new rules would have allowed the correction outright.

  This does **not** change the decision: a stored `status` column would carry exactly
  the same staleness *plus* the drift problem deriving eliminates. It is recorded here
  because it is a real property of the design and the sort of thing a future reader
  deserves to find written down rather than discover. Whoever swaps the trust model
  should treat existing `contradicts` rows as historical judgements, not current ones —
  and if that matters to them, the fix is to record the deciding policy on the row, not
  to abandon derivation.

- **The flag is structurally coupled to the evidence table, permanently.** Because
  `Contested` *is* `EXISTS(a contradicts row)`, there is no way to express "contested,
  then resolved" without extending the schema — a retraction concept the trail does not
  have (see below). That coupling is the price of having one source of truth, and it is
  paid knowingly: the alternative buys reversibility with a second place for truth to
  live.

- **Unbounded accumulation.** A user who repeats a refused claim adds a
  counter-evidence row each time. Deliberate for now — the trail is append-only by
  design (Layer 20) and the rows are the record. If this becomes noisy, the fix is
  presentation (collapse duplicates in `/why`), not silent dropping.
- **`contested` is one bit, not a scale.** Ten refused claims and one look the same.
  Weighing them needs evidence *independence* (vision §15: `N sources != N
  independent sources`), which today's `EvidenceCredibility` cannot express — it is
  per-provenance-class, so two `user_stated` restatements of one web page count
  twice. That is the next problem, and it is deliberately not solved here.
- **No un-contesting.** Nothing clears the flag. A fact challenged once reads as
  contested forever, even if the challenge is later withdrawn or the incumbent is
  re-confirmed a hundred times. Retraction is a real gap, and the direct consequence of
  the structural coupling above: since the flag is a pure function of the rows, clearing
  it means adding a way for a row to stop counting — a `retracted_by` pointer, or an
  explicit retraction polarity. Both are additive migrations, and neither is attempted
  here. Note the asymmetry this leaves: a belief can be challenged but never vindicated,
  which is a lopsided epistemics to be aware of before leaning on the flag.
- One extra `EXISTS` subquery per recalled row. Negligible at this scale, and it
  keeps the read path write-free.

## Alternatives rejected

- **A `status` column on `memories`.** What the vision literally proposes, and the
  obvious implementation. Rejected because it creates a second place where truth
  lives: a stored status can disagree with the evidence that is supposed to justify
  it, and nothing would detect the drift. Deriving makes that class of bug
  unrepresentable.
- **Store the refused claim as a fact with `status = 'rejected'`.** Rejected per
  decision 2: a stored fact is a recallable fact, and one bad filter anywhere on the
  read path silently grants it the authority the gate denied.
- **Let counter-evidence lower the incumbent's confidence.** Attractive — it feels
  like "weighing the evidence". Rejected per decision 5: it re-litigates authority
  through arithmetic, letting a source that lost the explicit decision win a partial
  one implicitly.
- **Implement the vision's full state machine now** (`Unexamined → … → Established`).
  Rejected: the transitions have no evidence-based definitions yet, so every one of
  them would be decided by the model. That is ADR 0003's mistake — a decision record
  whose mechanism is model behaviour — made a second time, and knowingly. One
  derivable bit is worth more than five states nothing can move honestly.
- **Trigger a Challenger LLM call on contestation** (vision §10). Rejected for this
  layer: a second call to the same model with a "please refute this" prompt is not
  independence, it is the same model wearing a hat. Real challenge comes from a
  different model, context, and tools — which connects back to §15 and belongs in
  whatever layer takes independence seriously.

## References

- Vision: [epistemic-reasoning-vision.md](../epistemic-reasoning-vision.md) §2, §12,
  §15, §17 (and §10 on Proposer/Challenger/Arbiter).
- Code: `internal/memory/evidence.go` (polarity, `RecordCounterEvidence`),
  `internal/memory/migrate.go` (migration 7), `internal/agent/learn.go`
  (`learnOneFact` — the former drop-site), `internal/agent/commands.go` (`/why`).
- Prior decisions: [ADR 0002](0002-provenance-from-source.md),
  [ADR 0003](0003-trust-model-for-supersession.md),
  [ADR 0004](0004-subject-as-data.md).

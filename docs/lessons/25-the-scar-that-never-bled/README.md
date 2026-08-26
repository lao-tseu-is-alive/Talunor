# Lesson 25 — The scar that never bled: designing a decision that binds

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🔍 Historical exploration + design study** (`v0.22.5` → `v0.23.0`, Layer 24) · Level 3
(advanced) · ~75 min

## Why this lesson exists

Four of this course's lessons are post-mortems — [11](../11-when-memory-forgets/),
[14](../14-the-approval-that-didnt-bind/), [22](../22-the-silent-suite/),
[24](../24-the-adr-that-didnt-bind/). Each starts from something that broke, and each
draws its authority from the scar: *we know this matters because it cost us.*

This one is different, and the difference is the lesson.

From `v0.20.0` to `v0.23.0` — **four layers, several weeks** — Talunor's memory destroyed
information every time its trust model refused a correction. Nothing crashed. No test
failed. No user complained. The defect was **fail-open on knowledge**: the system looked
exactly like a system that was working, because refusing a bad correction *is* the right
behaviour, and the forgetting was invisible.

So this is not a lesson written before the scar. It is **the first lesson about a scar
that never bled** — a defect found by *reading* rather than by incident, in code whose
tests were green the entire time. That class of defect deserves its own treatment,
because none of the techniques from the other four post-mortems would have found it: no
failing test to bisect, no error to trace, no review comment to falsify.

Then it does something the course has not done: it takes the **instrument built in
Lesson 24** — *mark each sentence of a decision record by what makes it true* — and
turns it on a decision record written afterwards, [ADR 0005](../../decisions/0005-contested-claims.md).
This time the instrument comes back green, sentence by sentence, and you check that
yourself rather than taking anyone's word for it.

> **A warning about this lesson's own genre.** "Here is a decision we got right" is the
> easiest kind of documentation to write badly. The defence is that you are given the
> tool and the evidence, and you run them. If ADR 0005 fails the test in Part 4 when you
> apply it, the lesson is wrong and you should say so — that is what
> [Lesson 15](../15-dont-trust-the-review/) trained you for.

## Learning objectives

By the end you can:

1. Describe **fail-open on knowledge** and explain why it is harder to detect than a
   crash, a wrong answer, or a failing test.
2. Reproduce, at `v0.22.5`, a gate that refuses correctly and forgets silently.
3. Apply Lesson 24's instrument — *what makes this sentence true?* — to a proposed
   design, and use it to reject a plausible-looking state machine.
4. Explain why **deriving** a status dissolves the question "who moves the token?"
   instead of answering it, and connect it to Layer 17's lazy decay.
5. Recognise the **authority back door**: re-litigating a settled decision through
   arithmetic instead of through a gate.
6. Break the derivation deliberately, manufacture the drift it prevents, and observe
   that nothing detects it.

## Prerequisites

- [Lesson 21](../21-whose-word-counts/) — the trust model and `memory.Supersedes`.
- [Lesson 24](../24-the-adr-that-didnt-bind/) — **required**: this lesson uses its
  instrument as a tool and assumes you have used it once.
- Helpful: [Lesson 18](../18-the-memory-of-the-gesture/) for lazy decay, and
  [Lesson 20](../20-learn-from-action/) for the evidence trail.

## Part 1 — the shape of an invisible defect

Read the drop-site as it stood for four layers:

```bash
git checkout v0.22.5        # detached HEAD — read only (see Lesson 00)
sed -n '/case RelSupersedes:/,/case RelUnrelated:/p' internal/agent/learn.go
```

You will find, in the `else` branch:

```go
} else {
    // The trust model forbids it — the old belief is more authoritative than
    // this source. Drop the new fact rather than store a contradiction.
    a.trace("supersede.denied", ...)
}
```

Stop and read the comment. It is **honest, accurate, and describes a deliberate
choice.** Nobody was careless here; "drop the new fact rather than store a
contradiction" is a sentence someone wrote on purpose.

Now list what the system knows at that instant, and what survives:

| Known at the moment of refusal | Survives the `else` branch |
|---|---|
| Two sources disagree about the same subject | ✗ |
| Which one lacked the authority to win | ✗ |
| The exact text of the losing claim | ✗ |
| The turn it arrived in | ✗ |
| That the question was raised at all | only if `TALUNOR_DEBUG` is on |

Every row is discarded. And here is why no test caught it: **there was nothing to
assert.** A test can check that the refusal happened (`superseded_by == 0` — and one
did, since Layer 23). No test can naturally check that something absent should have been
present, unless someone has already had the idea that it should.

> **Fail-closed on authority, fail-open on knowledge.** The gate correctly refused to let
> a weaker source overwrite a stronger one. It also, silently, made the system unable to
> remember that anyone had tried. The first half is a guardrail; the second half is data
> loss wearing the first half as a disguise.

## Part 2 — reproduce it

Still at `v0.22.5`. Add this probe to `internal/agent/`:

```go
// probe_test.go — at v0.22.5. Delete when done.
func TestProbeRefusalIsSilent(t *testing.T) {
	ctx := context.Background()
	store := testStore(t)
	observed, _ := store.RememberFact(ctx, "Signature X is mitigated by behaviour Y.",
		memory.Observed(true), 0.95)          // tool_observed, world → authority 2
	ag := newLearner(t, store, RelSupersedes) // the arbiter proposes supersession

	ag.learnOneFact(ctx, "Signature X is not mitigated at all.",
		memory.Observed(false), 0.5, 0, 42)   // model_inferred → authority 0

	got, _, _ := store.MemoryByID(ctx, observed.ID)
	t.Logf("superseded_by = %d  (0 = the refusal held — correct)", got.SupersededBy)

	ev, _ := store.EvidenceFor(ctx, observed.ID)
	t.Logf("evidence rows  = %d  (what does the system remember of the challenge?)", len(ev))
}
```

```bash
go test -tags sqlite_fts5 ./internal/agent/ -run TestProbeRefusalIsSilent -v
```

Expect:

```text
superseded_by = 0  (0 = the refusal held — correct)
evidence rows  = 0  (what does the system remember of the challenge?)
```

Both lines matter. The first says the guardrail worked. The second says the event left no
trace anywhere a user or an auditor can reach. **A passing system and an amnesiac one are
the same observation here** — which is precisely why four layers went by.

Delete the probe and return to `main`:

```bash
rm internal/agent/probe_test.go
git checkout main
```

## Part 3 — the tempting design, and the instrument that rejects it

The gap is now obvious, and Talunor's own research note had already described the target
shape. Read it:

```bash
sed -n '/^Unexamined/,/^Superseded/p' docs/epistemic-reasoning-vision.md
```

```text
Unexamined
 -> Hypothesis
 -> Supported
 -> StronglySupported
 -> Established

with possible branches:

Contested
Refuted
Superseded
```

This looks rigorous. It has states, transitions, and vocabulary borrowed from
epistemology. It would be entirely reasonable to implement it.

**Apply Lesson 24's instrument before you do.** For each transition, write down what
would decide it:

| Transition | What moves the token? |
|---|---|
| `Unexamined → Hypothesis` | ? |
| `Hypothesis → Supported` | ? |
| `Supported → StronglySupported` | ? |
| `StronglySupported → Established` | ? |

Do this honestly before reading on.

Every answer is some variant of *"when there is enough evidence"* — and **"enough" has no
definition in the codebase.** Which means, in practice, that the model decides: an LLM is
asked whether the evidence now suffices, and its answer moves the token.

That is [ADR 0002](../../decisions/0002-provenance-from-source.md)'s trap with extra
steps. Layer 16 refused to let a model report its own confidence; a state machine whose
transitions are model judgements is the same thing wearing a diagram. It is also
**exactly the mistake Lesson 24 is about** — a decision record whose mechanism turns out
to be model behaviour — and this time it was available *in advance*, in writing, before a
line of code existed.

> **The transferable move:** a state machine is only as real as its transition
> *functions*. States are cheap; deciding who moves the token is the entire design. If
> you cannot name a deterministic function for a transition, you have drawn a picture of
> a decision, not made one.

## Part 4 — turn the instrument on ADR 0005 itself

Now read the decision that shipped:

```bash
sed -n '/^## Decision/,/^## Consequences/p' docs/decisions/0005-contested-claims.md
```

Take its six numbered decisions and mark each one the way you marked the state machine.
For each, ask: **what makes this true — code, or behaviour?** Then verify your answer
against the repo. The claim of this lesson is that all six come back *code*; check it.

| # | Decision | What makes it true | Verify it yourself |
|---|---|---|---|
| 1 | Evidence rows carry a polarity | schema | `sed -n '/version: 7/,/^\t},/p' internal/memory/migrate.go` |
| 2 | The refused claim is not stored as a memory | code + test | `TestRefusedSupersessionIsRecordedAsCounterEvidence` |
| 3 | `Contested` is derived, never stored | SQL | `grep -n 'func contestedExpr' -A 6 internal/memory/memory.go` |
| 4 | A contested fact is still recalled, marked | code + test | `TestContestedFactIsStillRecalled` |
| 5 | Counter-evidence does not move confidence | code + test | `TestRefusedSupersessionIsRecordedAsCounterEvidence` |
| 6 | `/why` shows both sides | code + test | `sed -n '/func splitEvidence/,/^}/p' internal/agent/commands.go` |

The three load-bearing ones are pinned by assertions inside a single test. Read them, and
notice that each is phrased as the *property*, not the implementation:

```bash
sed -n '/func TestRefusedSupersessionIsRecordedAsCounterEvidence/,/^}/p' \
  internal/agent/agent_test.go | grep -nE 't\.Error|t\.Fatal'
```

You should see, among others:

```text
the refused correction must leave the incumbent contested, not vanish     ← decision 3
a refused claim must not erode the fact                                   ← decision 5
the refused claim was stored as a fact; it must live only as evidence detail ← decision 2
```

Now compare the two tables you have built. The state machine's column reads *model,
model, model, model*. ADR 0005's reads *schema, code, SQL, code, code, code*. **Same
instrument, same afternoon, opposite verdicts** — which is what makes this a
demonstration rather than a claim.

> Note the deliberate choice not to cite line numbers anywhere above. A reference like
> `agent_test.go:1450` rots the moment someone inserts a test above it, and this course
> has already shipped one stale exercise that way (see Lesson 08). **Names and
> `sed`/`grep` recipes survive edits; line numbers are drift with a colon in them.**

## Part 5 — how deriving dissolves the question

Read the mechanism:

```bash
grep -n 'func contestedExpr' -A 6 internal/memory/memory.go
```

```go
func contestedExpr(alias string) string {
	return `EXISTS(SELECT 1 FROM evidence ev WHERE ev.fact_id = ` + alias +
		`.id AND ev.polarity = 'contradicts')`
}
```

That is the whole of it. `Contested` is not a column, not a field anyone writes, not a
flag anyone flips. It is a question asked of the evidence at read time.

Look at what the question "who moves the token?" becomes: **nobody moves it.** There is
no transition to decide, because there is no stored state to transition. The status *is*
the evidence, re-derived on every read.

Two properties follow, and both matter more than they look:

1. **The flag cannot drift from its justification**, because it has no independent
   existence. A stored `contested` column could disagree with the evidence rows that are
   supposed to explain it — and nothing in the system would notice, because nothing
   compares them. Deriving makes that entire class of bug *unrepresentable*.
2. **The read path stays write-free**, which the store requires: it pins
   `SetMaxOpenConns(1)` (Lesson 03), so a recall that wrote back would serialise against
   itself.

If both of those feel familiar, they should:

```bash
grep -n 'LAZY' internal/memory/salience.go | head -3
```

Layer 17 made exactly this trade for retention — effective salience is computed on read,
never written back. **Layer 24 applies the same reasoning to truth.** One is about how
much a memory matters; the other about whether it is disputed; the argument is
identical, and noticing that is worth more than either layer on its own.

## Part 6 — the refusal that sounds most reasonable

ADR 0005 rejects four alternatives. Three are easy. Read the fourth:

```bash
sed -n '/Let counter-evidence lower/,/implicitly\./p' docs/decisions/0005-contested-claims.md
```

The idea is: *a fact contradicted ten times should be trusted a little less than one
contradicted never.* That is not a silly thought. It sounds like weighing the evidence,
which is what an epistemically careful system ought to do.

Follow the mechanics. The contradicting source reached that code path **because the trust
model refused it** — `memory.Supersedes` returned false, meaning: *this source is not
authoritative enough to change this fact.* If its claim then lowers the fact's confidence,
the source has changed the fact anyway, by a smaller amount, through a number nobody
re-derives.

> **The authority back door.** A decision settled explicitly at a gate, re-opened
> implicitly through arithmetic. The gate is visible, named, tested, and documented in an
> ADR; the coefficient is a float in a formula. Both change what the system believes —
> only one of them can be reviewed.

This is Layer 16's refusal one level up. Then: the model may not report its own
confidence. Now: a source that lost the authority argument may not win a partial version
of it. Same shape, and worth recognising as a shape, because it will recur wherever a
system has both a gate and a score.

The cost is real and stated: a fact contradicted fifty times is exactly as contested as
one contradicted once. **One bit, not a scale.** ADR 0005 argues that fixing this needs
evidence *independence* — fifty restatements of one web page are not fifty sources — and
that a confidence coefficient would be a wrong answer to the right question. See the
vision note's §15.

## Hands-on — break it three ways

### 1. Store the status, then manufacture the drift

This is the important one: make the *opposite* design real and watch it fail.

The store does not export its `*sql.DB`, so write this as an **internal** test —
`package memory`, the pattern `internal/memory/lexical_internal_test.go` already uses,
which is how a test reaches `s.db`:

```go
// internal/memory/contested_drift_test.go — package memory. Delete when done.
func TestProbeStoredFlagCanDrift(t *testing.T) {
	store := internalTestStore(t)      // the helper in lexical_internal_test.go
	ctx := context.Background()

	// The design ADR 0005 rejected: a stored flag beside the evidence.
	if _, err := store.db.ExecContext(ctx,
		`ALTER TABLE memories ADD COLUMN contested_flag INTEGER NOT NULL DEFAULT 0`); err != nil {
		t.Fatal(err)
	}
	fact, _ := store.RememberFact(ctx, "The earth is round.", Observed(true), 0.9)

	// Now do what an ordinary bug does: write one, forget the other.
	if _, err := store.db.ExecContext(ctx,
		`UPDATE memories SET contested_flag = 1 WHERE id = ?`, fact.ID); err != nil {
		t.Fatal(err)
	}

	var stored int
	store.db.QueryRowContext(ctx,
		`SELECT contested_flag FROM memories WHERE id = ?`, fact.ID).Scan(&stored)
	m, _, _ := store.MemoryByID(ctx, fact.ID)

	t.Logf("stored flag = %d, derived Contested = %v", stored, m.Contested)
}
```

```bash
go test -tags sqlite_fts5 ./internal/memory/ -run TestProbeStoredFlagCanDrift -v
```

```text
stored flag = 1, derived Contested = false
```

**Two answers to one question, and nothing in the system compares them.** Write the
assertion that would catch it. You will find you have to invent a consistency check that
must itself be run, scheduled, and maintained — a third thing that can rot. That check is
the ongoing cost the derived design does not pay.

### 2. Delete the positive half of the assertion

Open the course's own executable guard:

```bash
grep -n 'lesson 25' -A 12 docs/lessons/assertions.sh
```

It checks **two** things: that no migration adds a stored `contested` column, *and* that
`contestedExpr` exists. Delete the second check, then delete the entire Layer 24 feature,
and re-run:

```bash
make lessons-assert
```

It passes. An absence-only assertion cannot tell "correctly derived" from "removed
entirely" — it is satisfied by a repo where the feature never existed. **An assertion
about what is missing needs a partner assertion about what is present**, or it guards the
empty set.

Restore both halves.

### 3. Make the contested fact silent again

In `internal/agent/turn.go`, remove the `CONTESTED` marker from `fencedMemories` and run:

```bash
go test -tags sqlite_fts5 ./internal/agent/ -run CounterEvidence
```

It fails — deliberately. Recording a fact's contestation and never showing it to the
model would make the whole layer *decorative*: information stored, audited, and never
acted on. Ask yourself which other flags in systems you have worked on are in that state.

## The limits, honestly

A lesson that only praised the design would be the failure mode this one warns about.
Read what ADR 0005 admits, and take these seriously — two came from review *after* the
implementation was written:

- **A recorded refusal is permanent, and was made under one trust policy.**
  `memory.Supersedes` is deliberately swappable (ADR 0003 exists so a security or
  research agent can replace it). But counter-evidence rows are written at the moment of
  refusal, so a trail accumulated under one policy keeps asserting *that* policy's
  verdicts after it is replaced. This does not overturn the decision — a stored status
  would be equally stale *plus* drift-prone — but it is a real property. If it ever
  matters, record the deciding policy on the row; do not abandon derivation.
- **The flag is structurally coupled to the evidence table, forever.** Since `Contested`
  *is* `EXISTS(a contradicts row)`, "contested, then resolved" cannot be expressed without
  a retraction concept the trail does not have.
- **Therefore a belief can be challenged but never vindicated.** That asymmetry is worth
  sitting with before you lean on the flag: nothing clears it, however many times the
  incumbent is later re-confirmed.
- **One bit, not a scale**, as Part 6 explained.

## Postscript — the guard that caught this lesson's own release

While `v0.23.0` was being prepared, `make atlas-check` failed the commit: ADR 0005 was
tracked by git and absent from `docs/atlas.md`. The release could not proceed until the
new decision record was catalogued.

It is a small thing, and it is the lesson in miniature. The release that ships a decision
about **mechanical enforcement** was itself stopped by a **mechanical check** — not by
anyone remembering. That is the standard the whole course argues for, applied to the
course's own paperwork: *the guard you run beats the discipline you intend.*

## The principles

```text
A gate that refuses should record what it refused.
```

1. **Fail-open on knowledge is the hardest defect to see**, because the system looks
   like it is working — the guardrail really did fire. Look for it wherever code
   discards input it has decided not to trust.
2. **A state machine is its transition functions.** States are decoration; if you cannot
   name a deterministic function that moves the token, you have not made the decision.
3. **Derive rather than store, when the stored thing is a function of what you already
   keep.** It removes a class of bug instead of adding a check for it — and the read
   path stays free of writes.
4. **Watch for the authority back door**: a decision settled at a gate and re-opened
   through a coefficient. Gates get reviewed; floats in formulas do not.
5. **Assertions need both arms.** "X is absent" is satisfied by a repo where X was never
   built. Pair it with "Y is present."
6. **Cite names, not line numbers.** A reference that rots on the next insertion is drift
   with a colon in it.

## Completion checklist

- [ ] I can define *fail-open on knowledge* and say why green tests did not catch it.
- [ ] I reproduced the silent refusal at `v0.22.5` and read both log lines.
- [ ] I filled in the state-machine table myself before reading the answer.
- [ ] I applied the instrument to all six decisions of ADR 0005 and verified each.
- [ ] I can explain why deriving dissolves "who moves the token?" rather than answering it.
- [ ] I can state the connection to Layer 17's lazy decay in one sentence.
- [ ] I can explain the authority back door to someone who thinks lowering confidence is obviously right.
- [ ] I manufactured the drift a stored flag allows, and saw that nothing detects it.
- [ ] I broke the absence-only assertion and understood why it needed a partner.
- [ ] I can name at least two limits of this design without looking them up.
- [ ] I returned to `main` and deleted my probe.

---

## 🎓 About this lesson

This is the course's first lesson about a defect that **never produced a symptom** — no
crash, no failing test, no complaint, for four layers. The other post-mortems teach you to
work backwards from damage; this one teaches you that some defects have to be found by
reading, and gives you the specific reading technique that finds them: *list what the
system knows at this instant, then list what survives.*

It is also the answer to [Lesson 24](../24-the-adr-that-didnt-bind/). That lesson ended
with a rule — *a system can label a model's output honestly only in terms of what it
controls.* This one shows the same rule applied **before** the code existed, on a design
that was tempting, well-dressed, and would have failed the test. Nothing here required
being clever. It required running the instrument the previous lesson handed you, on your
own decision, while it was still cheap to change.

Back to the [course index](../).

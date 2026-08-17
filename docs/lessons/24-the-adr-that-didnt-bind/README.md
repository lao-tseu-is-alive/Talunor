# Lesson 24 — The ADR that didn't bind: a decision the code never enforced

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🔍 Post-mortem + historical exploration** (`v0.21.2` → `v0.22.0`, Layer 23) · Level 3
(advanced) · ~80 min

## Why this lesson exists

[Lesson 21](../21-whose-word-counts/) taught you Talunor's proudest safety idea: **a
memory's trust model is a decision you make, not a default you inherit.** It is written
down in [ADR 0003](../../decisions/0003-trust-model-for-supersession.md), argued from two
opposing examples, and implemented in one small named function — `memory.Supersedes` —
precisely so it can be read, tested, and owned.

Nine months of lessons later, an external review asked a rude question: *is it actually
enforced?*

It was not. The ADR's central claim — **authority is per-domain** — was true of the
design and absent from the code. This lesson has you reproduce the resulting hole at
`v0.21.2` with a twelve-line test, understand why the gate could not possibly have
stopped it, and read the fix that made the claim mechanical at `v0.22.0`.

It is the sibling of [Lesson 14](../14-the-approval-that-didnt-bind/), where an approval
bound the tool *names* but not the *arguments*. Same shape, one level up: there a
promise made to the user, here a promise made in an architecture decision record.

## Learning objectives

By the end you can:
- spot the tell that a safety property is unenforced — when its load-bearing step is a
  sentence in a **prompt** or a **model's judgement**;
- explain why `Provenance` alone cannot express "authority is per-domain", and what
  minimal data closes the gap;
- state how a system can assign a label to a model's output **honestly**, without
  parsing that output or trusting an instruction;
- order defensive checks by *reliability* — arithmetic before model — and say why that is
  more than an optimisation;
- explain why a migration that refuses to backfill is making a claim about honesty.

## Prerequisites

- **[Lesson 21](../21-whose-word-counts/)** (the trust model) — this lesson is its
  post-mortem; you need `Supersedes` and the flat-earth vs attack-signature pair.
- **[Lesson 20](../20-learn-from-action/)** — provenance assigned per source, ADR 0002's
  invariant. Layer 23 is that same move applied to a second field.
- Helpful: **[Lesson 14](../14-the-approval-that-didnt-bind/)** (the sibling
  post-mortem) and **[Lesson 15](../15-dont-trust-the-review/)** (this hole was found by
  a review — one whose claims had to be verified before any of them were believed).

## Part 1 — read the claim, and find its load-bearing step

```bash
git checkout v0.21.2        # detached HEAD — read only (see Lesson 00)
```

Open `docs/decisions/0003-trust-model-for-supersession.md` and read decision 3, the one
that contains the flat earth:

> *"A user's world-claim is stored as a **belief about the user** ("User believes the
> earth is flat") — the reflection prompt already frames facts as "User …". A
> belief-about-the-world and a world-fact are **different subjects**, so the arbiter
> returns `UNRELATED`."*

Now read it again as an engineer rather than as an author, and mark what each sentence
*depends on*:

| Step in the argument | Enforced by | Kind of thing |
|---|---|---|
| the fact is phrased "User believes …" | the extraction prompt | **an instruction to an LLM** |
| belief and world fact are different subjects | `FactArbiter.Classify` | **an LLM's judgement** |
| a source may only retire what it outranks | `memory.Supersedes` | code |

Two of the three are model behaviour. Check whether anything enforces the first:

```bash
sed -n '/func parseFacts/,/^}/p' internal/agent/reflect.go
```

It strips list markers and drops empty lines. **Any non-empty line becomes a fact.**
Nothing checks the "User …" framing the ADR leans on.

Then check whether the third step can see what the first two were about:

```bash
sed -n '/func supersedeAuthority/,/^}/p' internal/memory/supersede.go
```

Its parameter is a `Provenance`. Provenance answers *who said it*. The ADR's claim is
about *who said it, and about what* — and that second half is nowhere in the function's
inputs. It could not enforce the per-domain rule if it wanted to.

> **The tell.** When a security argument's load-bearing step is "the prompt already asks
> for X", you have a habit, not a guarantee. Prompts are requests. The question to ask of
> any safety claim is: *which line of code fails closed if the model misbehaves?*

## Part 2 — reproduce it

Two model steps must fail together for the hole to open, which is why nobody had seen it.
In a test you can simply make both fail at once. Create
`internal/agent/zz_probe_test.go` at `v0.21.2`:

```go
package agent

import (
	"context"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/memory"
)

func TestProbeAuthorityLaundering(t *testing.T) {
	ctx := context.Background()

	// Case 1: a world fact the model inferred.
	store := testStore(t)
	old, err := store.RememberFact(ctx, "The earth is round.", memory.ProvenanceModelInferred, 0.6)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	ag := newLearner(t, store, RelSupersedes) // the arbiter says SUPERSEDES (failure 2)
	// The extractor dropped the "User believes …" framing (failure 1): a bare world
	// claim, stamped user_stated because it came from the user's message.
	ag.learnOneFact(ctx, "The earth is flat.", memory.ProvenanceUserStated, 0.9, 0, 1)
	got, _, _ := store.MemoryByID(ctx, old.ID)
	t.Logf("case 1 (world fact was model_inferred): superseded_by = %d", got.SupersededBy)

	// Case 2: a world fact a VERIFIED TOOL observed.
	store2 := testStore(t)
	old2, err := store2.RememberFact(ctx, "Signature X is mitigated by behaviour Y.",
		memory.ProvenanceToolObserved, 0.95)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	ag2 := newLearner(t, store2, RelSupersedes)
	ag2.learnOneFact(ctx, "Signature X is harmless.", memory.ProvenanceUserStated, 0.9, 0, 2)
	got2, _, _ := store2.MemoryByID(ctx, old2.ID)
	t.Logf("case 2 (world fact was tool_observed): superseded_by = %d", got2.SupersededBy)
}
```

```bash
make deps       # if you have not already
go test -tags sqlite_fts5 ./internal/agent/ -run TestProbeAuthorityLaundering -v
```

```
case 1 (world fact was model_inferred): superseded_by = 2
case 2 (world fact was tool_observed): superseded_by = 2
```

Non-zero means **retired**. Sit with case 2 for a moment: the user's assertion retired a
Verified tool's observation — the exact fact ADR 0003 holds up as *authoritative in its
domain*, in the very example written to justify keeping tool observations at all.

Why? Work the arithmetic yourself from the function you read in Part 1:

```
supersedeAuthority(user_stated)   = 2
supersedeAuthority(tool_observed) = 2
Supersedes(newer, older) = na >= supersedeAuthority(older)  →  2 >= 2  →  true
```

Nothing malfunctioned. Every line did exactly what it said. The defect is that the
**inputs did not carry the distinction the decision needed** — which is a design defect
wearing the costume of a working function.

## Part 3 — the fix is not a better prompt

The tempting repair is to enforce the wording: make `parseFacts` reject or rewrite lines
that do not start with "User". Think about what that buys before reading on.

It fails on two counts:

1. **It makes safety depend on string surgery over model prose.** "Lives in Lausanne."
   is a legitimate self-fact phrased lazily; rewriting it to "User believes lives in
   Lausanne" is nonsense, and rejecting it silently loses a real memory.
2. **It throws away better information.** The system already knows where the text came
   from. Parsing the answer to recover something the *question* already determined is
   strictly worse evidence — the same mistake as asking a model to self-report its
   provenance, which [ADR 0002](../../decisions/0002-provenance-from-source.md) rejected
   for exactly this reason.

Now check out the fix and read the data it adds:

```bash
git checkout v0.22.0
cat internal/memory/subject.go
```

```go
type Subject string

const (
	SubjectUser        Subject = "user"        // a claim about the user — including their BELIEFS
	SubjectWorld       Subject = "world"       // a claim about anything outside the user
	SubjectUnspecified Subject = "unspecified" // legacy: written before Layer 23
)

type Attribution struct {
	Provenance Provenance // who stated it
	Subject    Subject    // what it is about
}
```

That is the whole missing half: **authority is a property of the pair, never of either
field alone.** `Supersedes` now takes `Attribution`s, and its first act is not to compare
authority at all:

```bash
sed -n '/^func Supersedes/,/^}/p' internal/memory/supersede.go
```

```go
func Supersedes(newer, older Attribution) bool {
	if !SameSubject(newer.Subject, older.Subject) {
		return false // different domains: not a contradiction, so nothing to retire.
	}
	...
}
```

The flat-earth carve-out, which lived in an arbiter's verdict, is now a string
comparison. Re-run your probe's logic at this tag (the API changed shape — the
adversarial versions live in `internal/agent/agent_test.go`) and both cases hold.

## Part 4 — how the system knows the subject *honestly*

Here is the part worth stealing for your own agent, and the reason this is not merely
"add a column".

A new field is only as trustworthy as whoever fills it. If the model labels its own
output's subject, we have re-created the sycophancy trap Lesson 17 warned about: a
self-declared credential is not evidence. So how does the system assign a label to text
*the model wrote*, without reading that text?

**By controlling the question.** Read `internal/agent/reflect.go` at `v0.22.0`:

```go
func promptFor(about memory.Subject) string {
	if about == memory.SubjectWorld {
		return worldFactPrompt
	}
	return userFactPrompt
}
```

Reflection asks the **user's message** "what is durably true about the USER?", and a
**tool observation** "what does this state about the WORLD?". The answer's subject is
known *before the model replies*, because it is a property of the question — and the
system stamps the answer with the question it answered:

```go
a.learnFrom(ctx, job.userInput, memory.UserSaid(), job.userTurnID)          // → SubjectUser
a.learnFrom(ctx, o.result, memory.Observed(o.verified), job.userTurnID)     // → SubjectWorld
```

Now trace the flat earth through it. You say "the earth is flat". The extractor, ignoring
its instructions, returns the bare claim `The earth is flat.` It is still stamped
`SubjectUser` — because that is the question that was asked — so it can never retire a
world fact, **whatever it says and whatever the arbiter thinks.** The misbehaviour is
contained without anyone inspecting the sentence.

> **The generalisation.** A system can label a model's output honestly only in terms of
> something it controls: the source, the question, the tool that ran. Never the answer.

## Part 5 — order your guards by reliability

Read `knownFact` at `v0.22.0`:

```go
if h.Kind == memory.KindFact && memory.SameSubject(about, h.Subject) {
	return h, true
}
```

A cross-subject neighbour is not a consolidation candidate at all — so for the flat-earth
case, **the arbiter is never called**. That saves an LLM round trip, but the point is not
performance:

```text
A step that cannot run cannot be wrong.
```

The pipeline is now ordered by how much each stage can be trusted: a deterministic
subject check, then the trust arithmetic, and only then the model's judgement — which now
decides *restates vs supersedes within one domain*, a narrower question that a wrong
answer damages less. Compare with Lesson 12's policy (deterministic gate around a
probabilistic actor) and Lesson 16's deterministic matchers (no LLM judge). Same instinct,
third context.

`TestCrossSubjectSkipsTheArbiter` pins the mechanism rather than the outcome — it counts
the arbiter's calls and requires **zero**. Asserting *how* the safety was achieved is what
stops a future refactor from re-introducing the dependency while the outcome test stays
green.

## Part 6 — the example that could not happen

While closing the gate, one more defect surfaced that no review had reported. At
`v0.21.2`, look at what every source is asked:

```bash
git checkout v0.21.2
grep -n "factSystemPrompt" internal/agent/reflect.go
sed -n '/const factSystemPrompt/,/NONE`/p' internal/agent/reflect.go
```

> *"extract only DURABLE facts worth remembering **about the user**"*

**Every** source went through that prompt — the user's message, and a tool's output
alike. So ADR 0003's attack-signature example, a Verified tool observing *"signature X is
mitigated by behaviour Y"*, had **no question that could ever return it**. The ADR's
second worked example, the one written to prove the design was not merely a user model,
was unreachable in the shipped code.

Nobody noticed because the example lived in a document. It had been read many times,
argued from, quoted in a lesson — and executed never.

> **A worked example in a document is not a test.** If an example justifies a design,
> make it run: as a test, or as an assertion the release gate checks (that is exactly
> what `make lessons-assert` does for the course's claims, see
> [Lesson 15](../15-dont-trust-the-review/)).

## Part 7 — the migration that refuses to guess

Migration 6 adds the column and stops there:

```go
// Existing rows default to 'unspecified' and are deliberately NOT backfilled.
// Guessing the subject of already-stored text would be the model labelling data
// after the fact — the exact laundering this layer prevents.
```

Old rows are `unspecified`, and `SameSubject` treats `unspecified` as comparable with
everything — so they keep exactly their old, weaker guarantee (provenance alone). Two
alternatives were available and both are worse:

- **Backfill by classifying the stored text.** That is the model labelling data
  retroactively: the same laundering, done in bulk, with the results looking like ground
  truth forever after.
- **Treat `unspecified` as comparable with nothing.** Safer-sounding, but it freezes
  every pre-existing memory: nothing could ever correct them again.

The honest option is to let old data say *"my subject is unknown"* and behave accordingly.
"Unknown" is a value. Inventing a better-looking one is not an upgrade.

## Hands-on — break it three ways

```bash
git checkout v0.22.0

# 1. Neutralise the mechanism and watch the adversarial tests catch it.
#    In internal/memory/subject.go, make SameSubject always return true:
#        func SameSubject(a, b Subject) bool { return true }
go test -tags sqlite_fts5 ./internal/agent/ ./internal/memory/ \
  -run 'CannotRetire|CrossSubject|SupersedesTrustModel|SameSubject'
#    Expect 5 failures — including the two that ARE the v0.21.2 bug.

# 2. Flip a policy cell instead. In supersedeAuthority, make user_stated about the
#    world authoritative again (return 2 instead of 0), and re-run. Which tests fail
#    now, and which do not? Explain the difference: one cell is reachable from
#    today's sources, the other is a statement of policy.

# 3. Make the SUBJECT dishonest while leaving provenance intact. In agent.reflect,
#    attribute a tool observation to the user's domain:
#        memory.Attr(memory.Observed(o.verified).Provenance, memory.SubjectUser)
go test -tags sqlite_fts5 ./internal/agent/ -run TestReflectLearnsFromToolObservation
#    → "tool-derived subject = user, want world"
#
#    Now delete the two subject assertions from that test and re-run the WHOLE suite.
#    Everything is green again. Sit with that.

git checkout internal/  # restore
```

Experiment 3 is the honest one. The layer's guarantee holds *given* that each source is
attributed correctly, in the one place that knows the source — a six-line surface in
`reflect`. Two assertions stand between that surface and silence; delete them and a
mislabelled subject sails through a fully green suite, because every *other* test asks
about behaviour downstream of the label.

Note which half was already covered before this layer: a Layer-20 test pinned the
**provenance** of a tool-derived fact from the day it shipped. Nothing pinned the
**subject**, because the subject did not exist. New data needs new assertions at the
point of assignment — the field you add is exactly the field your existing tests are
blind to.

## The principles

```text
Authority is a property of (who spoke, about what) — never of either half alone.
```

1. **Find the load-bearing step of every safety claim.** If it is a prompt's wording or a
   model's judgement, the claim is a habit. Ask which line fails closed.
2. **A decision record is not an enforcement mechanism.** ADRs age into folklore unless
   something executes them; the gap is invisible precisely because the document reads
   true.
3. **Label a model's output from what you control** — the source, the question, the tool —
   never from the output itself.
4. **Order guards by reliability:** arithmetic, then policy, then model. A step that
   cannot run cannot be wrong.
5. **Test the mechanism, not only the outcome.** Counting the arbiter's calls is what
   keeps the guarantee from silently reverting to "the model got it right".
6. **State the unreachable cells of a policy.** The next person adding a source reads the
   matrix; a hole makes them infer, and inference is how the original gap appeared.
7. **Don't backfill what you'd have to guess.** A migration that leaves old rows honest is
   worth more than one that makes them look complete.

## Completion checklist

- [ ] I reproduced both laundering cases at `v0.21.2` and can explain `2 >= 2`.
- [ ] I can name the two model-dependent steps ADR 0003 relied on, and show that
      `parseFacts` enforced neither.
- [ ] I can explain why enforcing the "User …" wording would have been the *worse* fix.
- [ ] I can state how the subject is assigned honestly (the question, not the answer) and
      why that is ADR 0002's invariant applied twice.
- [ ] I ran hands-on 1 and saw five tests fail, and can say which two are the old bug.
- [ ] I did hands-on 3, deleted the two subject assertions, and can articulate where this
      layer's trust bottoms out — and why new data needs assertions at the point of
      assignment.
- [ ] I can say why migration 6 does not backfill, and returned to `main`.

---

## 🎓 About this lesson

Three of this course's post-mortems now share one skeleton. Lesson 14: an approval bound
tool *names*, not *arguments*. Lesson 22: a suite reported `ok` for tests it never ran.
Lesson 24: an ADR stated a rule the code could not represent. In each, nothing was
broken — every component did exactly what it said — and the guarantee lived in the space
*between* components, which is precisely where nobody looks.

The other thread worth pulling: this hole was found by an external AI review, and
[Lesson 15](../15-dont-trust-the-review/) exists to teach you not to believe such reviews.
Both are right. The review's finding was *verified against the code* before a line was
changed — reproduced as a failing test, at the tag, before the fix was designed. A review
is a list of things to check, never a list of things to do; and this one earned its keep
precisely because it was checked.

Back to the [course index](../).

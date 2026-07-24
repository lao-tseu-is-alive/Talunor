# Lesson 18 — The memory of the gesture: salience, decay & consolidation

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🔍 Exploration + hands-on** (reading `internal/memory` and `internal/agent`; the
Layer 17 code shipped at `v0.17.0`, reference docs on `main`) · Level 3 (advanced) · ~75 min

## Why this lesson exists

Lesson 17 gave a memory a *trust* — where it came from and how much to believe it.
But it left every memory equally *alive*: a fact you mentioned once a year ago and a
fact you repeat every day sat side by side, and recall treated the store as one
undifferentiated pile. That is not how memory works, for people or for a good agent.

Here is a parallel you have already felt. When a long chat fills the context window,
you run `/compact`: the assistant **consolidates** many turns into a short summary and
**lets the trivial detail fade**. That is exactly what this layer teaches Talunor's
*long-term* store to do — but continuously and gradually, not in one lossy burst under
pressure. Working memory (the context window, the short-term ring buffer) gets
compacted when it overflows; long-term memory (the SQLite store) gets its own
**salience** so it, too, keeps what matters and lets the rest fade.

The biological name for the useful version of this is consolidation: what gets
re-activated is strengthened, what goes unused fades. Layer 17 gives the store two
forces — **decay** (fade by neglect) and **reinforcement** (strengthen by use and
repetition) — and, crucially, does it without betraying the honesty rule you built in
Lesson 17.

## Learning objectives

By the end you can:
- explain why an undifferentiated memory store degrades recall, and what **salience**
  adds on top of relevance and confidence;
- describe **lazy decay** and say why computing it at read time (rather than a
  background sweeper) is the design that respects the store's single connection;
- read how recall now **ranks** by `similarity × confidence × effective-salience` and
  **soft-forgets** faded memories without deleting them;
- explain **consolidation** — why a restated fact reinforces an existing row instead
  of piling up a duplicate — and the **independence rule** that keeps it honest;
- set the knobs and watch a memory fade, be revived, and strengthen.

## Prerequisites

- **Lesson 05** (the agent loop) — where recall and reflection live.
- **Lesson 17** (learning with humility) — provenance & confidence; this lesson builds
  the *retention* half on top of that *trust* half, and leans on the same honesty rule.
- **Lesson 11** (when memory forgets) — the observability instinct; here you watch
  salience and score in `/debug` and `/list`.

## Part 1 — the pile problem, and a new column

Read the shape of a memory on `main`:

```text
internal/memory/memory.go
```

Since Lesson 03, `Recall` returned the *k nearest* memories by cosine distance, later
gated by a `maxDistance` threshold (Lesson 17 added a confidence filter). Relevance and
trust — but nothing about **how much a memory currently matters**. A fact recalled every
day and one recalled once rank identically if they sit at the same distance.

Layer 17 adds that missing axis. Read how the column arrived:

```text
internal/memory/migrate.go
```

**Migration 3** appends three columns to `memories`: `salience` (default `1.0`),
`last_accessed`, and `access_count`. This is Lesson 17's append-only rule in action
again — migration 3 is *added*, never editing 1 or 2, and existing rows start fully
salient and unaccessed, so nothing already stored is retroactively demoted. `doctor`
now prints `schema version: 3`.

## Part 2 — lazy decay: the design that respects the constraint

Now the heart of the layer:

```text
internal/memory/salience.go
```

A memory's salience should fall the longer it goes untouched. The obvious
implementation — a background job that periodically writes a lower salience to every
row — is exactly wrong *here*, and the reason is a gotcha you met in Lesson 02:

> **The constraint.** The store pins `db.SetMaxOpenConns(1)` because the SQLite
> extensions keep the model and vector state in *per-connection* state. A background
> "decay sweeper" writing to every row would fight the very connection that reads use.

So decay is **lazy**. Nothing is ever written just to make a memory fade. The stored
`salience` is its value *as of* `last_accessed`; the **effective** salience at read
time is computed from it. Read `effectiveSalience`:

```go
// salience × 2^(−age / half-life): after one half-life the factor is 0.5, after two 0.25…
return salience * math.Exp2(-float64(age)/float64(halfLife))
```

Half-life form because it is teachable: after `TALUNOR_SALIENCE_HALFLIFE` (default 30
days) of neglect, a memory is worth half as much. The elegant move is to *not store the
decayed value at all* — only the salience-as-of-last-touch — and decay it on the way
out. **Recall performs no writes**, so it stays a pure read on the single connection.

Now read `Recall` in `memory.go` again with that in mind. Two changes:

1. **Ranking.** Relevance is still the *gate* (assistant turns excluded, `maxDistance`
   drops the irrelevant), but among the relevant neighbourhood memories are now ordered
   by a combined score:

   ```go
   h.Score = (1 - h.Distance) * h.Confidence * eff   // similarity × trust × how-much-it-matters-now
   ```

   A trusted, reinforced memory outranks a barely-relevant or long-faded one at a
   similar distance.

2. **Soft forgetting.** A memory whose effective salience has fallen below
   `ForgetFloor` (default `0.05`) is *dropped from recall* — but the row is never
   deleted. Read the comment: it survives, and a restatement revives it. This is a
   deliberate choice over hard deletion: forgetting personal data silently is a bigger
   risk than keeping a faint row that no longer surfaces.

## Part 3 — reinforcement & consolidation: the memory of the gesture

Decay is only half the story; the other half is what *strengthens* a memory. Read the
two reinforcement methods in `salience.go`:

- `Reinforce(ids)` — bumps salience (capped), increments `access_count`, and resets the
  decay clock (`last_accessed = now`). It touches **salience only**.
- `ReinforceFact(id, gain)` — does all of the above *and* raises **confidence** toward a
  ceiling below 1.0, with diminishing returns.

They fire at two well-defined moments, never as a side effect of `Recall` (recall is a
pure read). Find them in `internal/agent/agent.go`:

- **On recall** — `reinforceRecalled` bumps the salience of the memories that shaped a
  turn's prompt. Being retrieved and used is a signal a memory matters.
- **On restatement** — this is the upgrade to what used to be plain de-duplication. Read
  `reflect`. Previously, when the extractor produced a fact already in the store, the
  agent *skipped* it (`factKnown`). Now it **consolidates**: `knownFact` returns the
  existing row, and `ReinforceFact` strengthens it instead of storing a near-duplicate.

That last change is the point of the lesson. A fact you state three times becomes **one
increasingly trusted, increasingly salient row**, not three copies. This is the *memory
of the gesture*: the more a piece of knowledge is re-confirmed, the more it counts —
the same way a motion you repeat becomes second nature.

## Part 4 — the independence rule: keeping repetition honest

Here is the trap, and the most important idea in the layer. "The more a fact is
repeated, the more I trust it" is right — **but only if the repetitions are
independent.** If the *model* restates its own earlier inference and you count that as
confirmation, the agent builds a self-reinforcing echo chamber and talks itself into
false certainty. That is the Lesson 17 sycophancy trap, coming back through the side
door.

Read `EvidenceCredibility` in `salience.go`:

```go
case ProvenanceUserStated, ProvenanceToolObserved:
    return 1.0   // independent, credible corroboration
case ProvenanceModelInferred:
    return 0.0   // the model echoing itself — no confidence gain
```

The parry is to **split the two effects**, which is exactly why Lesson 17 kept salience
and confidence as *separate* axes:

- **Salience** rises on *any* repetition or recall. Frequency means "this matters,"
  regardless of whether it is true. No risk.
- **Confidence** rises *only on independent evidence*. A user restating (or a tool
  re-observing) a fact corroborates it; the model re-inferring its own claim earns
  **zero** confidence gain — salience up, confidence flat.

Read where the agent computes the gain in `reflect`:

```go
gain := clamp01(consolidationGainBase * memory.EvidenceCredibility(prov) * a.cfg.ModelConfidence)
```

Notice the gain also folds in `ModelConfidence` — the same **calibration link** from
Lesson 17. A restatement from an unreliable model earns proportionally less. And the
confidence update itself (in SQL, mirrored by `reinforcedConfidence`) only ever moves a
fraction of the way to a ceiling below 1.0: repetition, however often, never makes a
claim *certain*. That is the humility of Layer 17, preserved into retention.

## Part 5 — watch it fade, revive, and strengthen

First the pure-function tests — no database, no extensions, so they run anywhere:

```bash
go test ./internal/memory/ -run 'EffectiveSalience|Evidence|Reinforced' -v
```

Read them alongside the code: decay halves over a half-life; independent evidence
counts, the model echoing itself does not; confidence rises with diminishing returns
and never passes the ceiling.

Then the store-backed behaviour (needs `make deps`):

```bash
go test ./internal/memory/ -run 'Reinforce|Forget' -v
```

`TestRecallForgetFloorAndRevival` is the one to read: a fresh fact below the forget
floor is soft-forgotten (recall returns nothing, but `Count` is still 1), then
reinforcing it past the floor brings it back. Forgetting without deletion, revival
without magic.

Now live (needs Ollama). Make forgetting easy to see by setting the floor above a fresh
memory's salience, and turn on the in-session trace:

```bash
TALUNOR_FORGET_FLOOR=1.4 go run ./cmd/talunor --plain
```

```text
you> /debug on
you> remember that my cat is called Ada
you> what is my cat called?
```

With the floor at `1.4` and a fresh salience of `1.0`, the fact is stored but
soft-forgotten — the `/debug` recall trace shows it dropping out, and the agent may not
recall Ada. Restate it (`my cat Ada is very old now`) and watch the `reflect: ~fact …
reinforced` line: the salience climbs past the floor and the memory returns to recall.
Run `/list` and you will see the fact annotated `(user_stated 90%, sal 1.5×1)` — trust,
salience, and access count, all visible. This is the Lesson 11 observability instinct
applied to retention: you can *see* why a memory ranks where it does, or why it vanished.

## The principles

```text
Keep what is used, let the unused fade — and let only independent repetition build trust.
```

1. **Salience is a third axis, alongside relevance and confidence.** Recall ranks by
   all three; a memory that matters now outranks one that has faded.
2. **Decay lazily.** Compute it at read time so recall never becomes a write — the
   design that respects the single connection. Store the value-as-of-last-touch, not
   the decayed value.
3. **Forget softly.** Drop a faded memory from recall, but keep the row — a restatement
   revives it. Never delete personal data silently.
4. **Consolidate restatements; repetition strengthens memory.** But salience rises on
   any repetition, while confidence rises **only on independent evidence** — the
   echo-chamber guard that keeps Lesson 17's honesty rule intact.

## Completion checklist

- [ ] I can explain what salience adds that relevance and confidence do not.
- [ ] I can say why decay is computed at read time, and tie it to `SetMaxOpenConns(1)`.
- [ ] I read `Recall` and can point to the `similarity × confidence × salience` score
      and the forget-floor drop.
- [ ] I can explain why a faded memory is soft-forgotten, not deleted.
- [ ] I read `reflect` and can describe how a restatement consolidates instead of duplicating.
- [ ] I can state the independence rule and why salience and confidence stay separate.
- [ ] I ran the agent with a high `TALUNOR_FORGET_FLOOR`, watched a memory vanish, and revived it.

---

## 🎓 About this lesson

This closes the first movement of Iteration 4. Follow the arc: Lesson 11 caught a
substrate degrading *silently*; Lesson 16 *measured* a model's reliability; Lesson 17
*spent* that measurement to govern how much the agent may believe; and this lesson
governs which memories it *keeps*. Trust, then retention — the two halves of learning
with a spine.

Notice the `/compact` parallel one more time, because it is exact: compaction is forced,
lossy consolidation of *working* memory under context pressure; salience is continuous,
graceful consolidation of *long-term* memory. Same instinct — strengthen the
re-activated, fade the trivial — on the two different timescales an agent lives in.

The next layer, **async reflection**, is about *when* learning happens rather than
*what* is learned: reflection currently runs a second model call on the turn's critical
path (you feel it as latency). Moving it to a background worker — one that must own the
single store connection you just spent two lessons respecting — is Layer 18.

Back to the [course index](../).

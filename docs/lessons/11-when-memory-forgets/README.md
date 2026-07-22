# Lesson 11 — When memory silently forgets: embedding provenance & observability

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🔍 Historical exploration** (reading the `v0.11.0` code, with 🛠️ runs on `main`) ·
Level 3 (advanced) · ~75 min

## Why this lesson exists

One day Talunor *forgot who its user was*. The memory was still in the database —
you could see it with `/list` — but when the user said "you know who I am", the
agent drew a blank. Nothing had crashed. No error was logged. Recall simply
returned the wrong neighbours.

This lesson retraces that real bug, because the cause is a trap every
retrieval/embedding system can fall into, and the fix teaches two durable ideas:
**store the provenance of your vectors**, and **make the invisible visible**. It's
the lesson that turned a multi-hour forensic dig into a one-command look.

## Learning objectives

By the end you can:
- explain why an embedding is only comparable with vectors from the **same** model,
  and what silently breaks when the model changes;
- describe how a **canary-vector fingerprint** detects that change on startup;
- use `talunor --reembed` to realign a drifted database;
- turn on the `/debug` trace and read a recall ranking to diagnose "why didn't it
  remember that?".

## Prerequisites

- Lesson 02 (persistent memory) and **Lesson 03 (embeddings & cosine distance)** —
  this lesson builds directly on them.
- Lesson 08 (observability) helps for the second half.

## Part 1 — the bug, as it happened

Picture the database after a few sessions: a `turn` from weeks ago, *"hy my name is
Carlos and i like to develop in Go and Typescript"*, sits safely stored. Later the
user asks, vaguely, *"hello you know who i am"*. Recall runs, and the Carlos memory
is **not** among the results. Why?

Recall works by embedding the query and finding the nearest stored vectors by
cosine distance (Lesson 03). So there are only two moving parts: the query's
vector, and the stored vectors. The query is embedded fresh, now. The Carlos memory
was embedded **weeks ago**. If the model that produced those two vectors is not the
*same*, their distance is meaningless — and that is exactly what had happened. The
embedding model file (fetched, historically, from a *mutable* URL — see the
checksum pin added in `v0.9.1`) had been replaced by a different build. Same
dimension, same table, **different vector space**.

You can feel the problem with one number. Embed the *identical* sentence twice with
the current model and the two vectors are byte-for-byte the same (embedding is
deterministic — cosine distance `0.000000`). But the *old stored* vector for that
same sentence sat a cosine distance of **~0.17** away from a freshly computed one.
Not zero. The vectors had drifted into different spaces, and KNN quietly ranked
them wrong.

> **The core idea.** An embedding is a coordinate in a space that *only that model*
> defines. Vectors from two model builds share a shape but not a meaning. Comparing
> across them produces plausible-looking distances that are simply wrong — the worst
> kind of bug, because nothing errors.

## Part 2 — the guard (read it at `v0.11.0`)

The fix records a fingerprint of the embedding stack *inside the database* and
verifies it every time the store opens. Read the code as it landed:

```bash
git checkout v0.11.0        # detached HEAD — read only (see Lesson 00)
```

Open the new file:

```text
internal/memory/provenance.go
```

Walk through it and find these pieces:

- **A `meta` side-table** (`metaSchemaSQL`) — a tiny key/value store next to
  `memories`. It holds three things: the model's basename, the embedding
  dimension, and — the important one — a **canary vector**.
- **The canary** (`embedCanaryText`) — a fixed sentence that is embedded and whose
  vector is stored. On the next `Open`, the store re-embeds that same sentence and
  compares the new vector with the saved one. Any change to the embedding stack —
  the model file, its config, even the extension build — moves the canary, so the
  comparison catches *all* of them, not just a renamed file. (This is why a canary
  beats hashing the model file: it fingerprints the *behaviour*, not the bytes.)
- **`ProvenanceStatus`** — the three outcomes:
  - `ProvenanceOK` — fresh store, or the canary matches → recall is trustworthy.
  - `ProvenanceStale` — the canary no longer matches → the model changed; old
    vectors are in a dead space.
  - `ProvenanceUnknown` — the store has memories but *no* recorded canary, i.e. it
    predates this feature and can't be verified.
- **`initProvenance`** — called at the end of `bootstrap` (see `store.go`). It
  compares (or, on a fresh store, stamps) the fingerprint and sets the status.
- **`ReEmbed`** — the migration: it recomputes every stored vector with the current
  model and re-stamps the fingerprint. Note the shape of the loop — it reads **all**
  rows into a slice and closes the cursor *before* embedding. That's not a style
  choice: the store pins the pool to a single connection (per-connection model
  state, see Lesson 02's gotcha), so a still-open `rows` cursor would deadlock the
  `Embed` query. A nested query on a one-connection pool is a self-inflicted hang.

Also glance at the pure-Go `cosineDistanceBlob` — it decodes two FLOAT32 BLOBs and
returns `1 − dot product` (the vectors are normalised, so the dot product *is* the
cosine similarity). It's the same distance Lesson 03 explained, written out by hand.

When you're done reading, come back:

```bash
git switch main
```

### Experiment — watch the guard work

The provenance test drives the whole stale→re-embed cycle deterministically (no
model swap needed):

```bash
go test ./internal/memory/ -run TestProvenanceStaleThenReEmbed -v
```

Read that test alongside `provenance.go`: it stores facts, corrupts the canary to
simulate a model change, reopens (→ `ProvenanceStale`), runs `ReEmbed`, and reopens
again (→ `ProvenanceOK`). It's the bug and its fix, in twenty lines.

And on a fresh database you can see the healthy status directly:

```bash
make doctor
# • embedding model: all-MiniLM-L6-v2.f16.gguf (dim 384), provenance: ok
```

## Part 3 — the fix in practice: `--reembed`

When the guard trips on a real database, the app doesn't fail — it **warns** at
startup and points at the fix. Running the migration realigns every old vector into
the current model's space:

```bash
go run ./cmd/talunor --reembed
# re-embedding all memories with all-MiniLM-L6-v2.f16.gguf (dim 384)…
#   10/10
# ✓ re-embedded 10 memories (provenance: unknown … → ok)
```

After that, recall of the old memories works again, because they now live in the
same space as the queries. `/mem` will report `provenance: ok`.

## Part 4 — making the invisible visible: `/debug`

The bug was hard to find for one reason: **you couldn't see the recall ranking**.
The agent had traced it since `v0.9.1`, but only to a log file (`TALUNOR_DEBUG`).
`v0.11.0` adds an interactive switch. Read it:

```text
internal/agent/debug.go     # the /debug toggle and the trace formatting
```

The trick is deliberately small: it does **not** invent a new rendering subsystem.
Debug notes ride the *existing* `Reasoning` channel of `llm.Chunk` — the same
channel tool activity already uses — which both front-ends render dimmed. So one
toggle buys you inline visibility with zero renderer changes. (Answer text is
accumulated from `Content` only, so these notes never pollute the stored reply or
the reflection input.)

### Experiment — read a recall ranking live

Needs Ollama. Start the REPL and turn debug on:

```bash
go run ./cmd/talunor --plain
```

```text
you> /debug on
you> what is my name and what do i like?
```

You'll see, dimmed, exactly what shaped the answer:

```text
· recall: q="what is my name and what do i like?" k=8 max≤0.75 → 3 hit(s)
·     #13 d=0.5154 turn "write me a hello [my name here]…"
·     #1  d=0.6324 turn "hy my name is Carlos and i like to develop in Go…"
· reflect: extracted 0, stored 0, skipped 0
```

That one view — the query, the budget (`k`, the distance threshold), and every hit
with its distance and kind — is the whole diagnosis. A memory sitting *just* above
the `max≤0.75` line is present but excluded; a memory absent entirely means the
embedding didn't rank it. Had `/debug` existed at the time, the Carlos bug would
have been a ten-second look instead of an afternoon.

## The principles

```text
An embedding without its model's identity is a number without units.
```

1. **Store provenance with your data.** Vectors are only comparable within one
   model's space; record which model produced them and check it, or drift will
   corrupt recall in silence.
2. **Pin fetched artifacts by digest from day one.** The root cause was a model
   pulled from a mutable URL before checksums existed. A checksum protects
   *forward*; it can't fix vectors already written by the old file.
3. **The best observability is the one already wired.** The events existed; making
   them *visible* inline was a toggle, not a subsystem.
4. **Non-blocking ≠ invisible** (Lesson 08, again): the app carries on when
   provenance is off, but it says so, loudly, once.

## Completion checklist

- [ ] I can explain why comparing embeddings from two different models is wrong even
      though the distances look normal.
- [ ] I read `provenance.go` at `v0.11.0` and can say what the canary vector detects.
- [ ] I ran `TestProvenanceStaleThenReEmbed` and followed the stale→re-embed cycle.
- [ ] I can say why `ReEmbed` reads all rows before embedding (single connection).
- [ ] I turned on `/debug` and read a recall ranking with distances.
- [ ] I can state the principle: an embedding is meaningless without its model's
      identity.
- [ ] I returned to `main`.

---

## 🎓 About this lesson

This is the newest lesson, and the first drawn from a **real bug fixed in the
project's own history** rather than a planned layer — proof that the course grows
with Talunor. If you've done 00–10, you now also understand how a production
retrieval system fails *quietly*, and how to catch it. That instinct — *"nothing
errored, so distrust everything"* — is worth more than any single feature.

Back to the [course index](../).

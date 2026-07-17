# Lesson 02 — Persistent memory with SQLite

**🔍 Historical exploration** · Level 1 (beginner) · ~45 min

## Why this lesson exists

An agent that forgets everything between messages is just a chatbot. Talunor's
defining feature is **memory that persists** — across turns and across sessions.
This lesson opens the box: how does Talunor *store* things, and what exactly
survives a restart? You'll read the storage layer when it was still small, at
**`v0.2.0`**.

## Learning objectives

By the end you can:
- explain what the `memory.Store` is responsible for (and what it deliberately
  hides);
- describe the lifecycle of the store: `Open` → use → `Close`;
- tell the difference between the **short-term buffer** and the **long-term store**,
  and say what survives a restart.

## Prerequisites

- Lessons 00 and 01 done.

## Check out the memory layer

```bash
git checkout v0.2.0     # detached HEAD — read only (see Lesson 00)
```

> **Files at this tag** (no `docs/atlas.md` yet — here's the whole project):
>
> ```text
> cmd/doctor/main.go             the only program: store a corpus, recall it
> internal/memory/store.go       Open/Close the DB, load the C extensions, the schema, Embed
> internal/memory/memory.go      Remember / Recall / Count — the memory API
> internal/memory/shortterm.go   a small ring buffer of the most recent turns
> internal/memory/cgo_link.go    the cgo glue that makes the C extensions loadable
> Makefile · README.md · CHANGELOG.md
> ```
>
> There's no agent and no LLM yet — Talunor is still just a memory you can talk to
> from a smoke test.

## Read, in this order

```text
internal/memory/store.go       # start here: Open, Close, the schema, Dim, Embed
internal/memory/memory.go      # then the API: Remember, Recall, Count
internal/memory/shortterm.go   # finally the in-RAM recent-turns buffer
```

## The two kinds of memory

This is the key distinction to walk away with:

| | Short-term | Long-term |
|--|-----------|-----------|
| Where | `shortterm.go` — a slice in RAM | `store.go` — a SQLite file on disk |
| Holds | the last few turns, verbatim | everything you've ever remembered |
| Survives a restart? | **No** | **Yes** |
| Purpose | immediate context, cheap | recall by meaning, later |

The short-term buffer is a **ring buffer**: it keeps only the last *N* turns and
drops the oldest — enough to keep a conversation coherent without growing forever.
The long-term store is a single SQLite file, which is why the real agent (from
Lesson 01) still knew things after you quit and relaunched it.

## The store is a boundary, not just "SQL hidden away"

Look at `Open` and `Close` in `store.go`. A store abstraction does more than hide
SQL — it defines **guarantees**:

- *when* is the schema created? (`Open` bootstraps it);
- *what* happens on failure? (`Remember`/`Recall` return an `error` — callers
  decide);
- *what* must be released? (`Close`, so the file handle and C resources are freed).

That's the real lesson of this layer: **a resource has a lifecycle, and a good
abstraction makes that lifecycle explicit.**

## Experiment

Run the smoke test (uses the memory layer end to end):

```bash
make doctor
```

It `Remember`s a small corpus, then `Recall`s it. Read `cmd/doctor/main.go`
alongside the output: find the `Remember(...)` calls and the `Recall(...)` call,
and match them to what prints.

Then a thought experiment — trace it in the code, don't run it:

```text
Remember("My preferred language is Go")   → written to SQLite (survives restart)
short-term buffer                          → holds it in RAM  (lost on restart)
[ restart the program ]
Recall("what language do I like?")         → still finds it — it was on disk
```

When you're done, return to the latest code:

```bash
git switch main
```

## Questions to answer

- Which function creates the database schema, and when does it run?
- If `Remember` fails, who decides what to do — the store, or its caller? Why is
  that the right place?
- After a restart, what does the agent still know, and what has it forgotten?

## Common mistakes

- **Confusing the two memories.** "It remembers within a chat" (short-term) is not
  the same as "it remembers across restarts" (long-term SQLite).
- **Forgetting `Close`.** In Go, a resource you `Open` you must `Close` (often with
  `defer`). Notice where the callers do it.

## Completion checklist

- [ ] I can say, in one sentence, what `memory.Store` is responsible for.
- [ ] I found `Open` and `Close` and can describe the store's lifecycle.
- [ ] I can name one thing that survives a restart and one that doesn't.
- [ ] I ran `make doctor` and matched a `Remember`/`Recall` call to its output.
- [ ] I returned to `main`.

**Next:** [Lesson 03 — Semantic recall & embeddings](../03-semantic-recall/).

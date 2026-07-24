# Lesson 19 — Off the critical path: learning in the background

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🔍 Exploration + hands-on** (reading `internal/agent`; the Layer 18 code shipped at
`v0.18.0`, reference docs on `main`) · Level 3 (advanced) · ~70 min

## Why this lesson exists

Since Lesson 05, every turn has done two model calls: one to *answer* you, and a second,
quieter one to *learn* — the reflection step that distils durable facts from what you
said. And until now that second call was **synchronous**: it ran at the tail of the
turn's goroutine and held the reply channel open until it finished. The answer had
already streamed to your screen, but the turn did not read as *done* — the prompt did
not come back — until fact extraction completed. You were waiting on the agent's
homework.

Layer 18 fixes that by moving learning **off the critical path**: the reply streams, the
turn ends, and learning catches up on a background worker. That sounds like a one-line
change — `go reflect()` — and it is *almost* that. The interesting part is everything
that "almost" hides: a shared database connection that must not be corrupted, a
shutdown that must not drop learning, and a debug view that can no longer work the way it
did. This is the course's concurrency lesson, and it rewards reading the *reasoning* more
than the diff.

## Learning objectives

By the end you can:
- explain what "on the critical path" costs and what moving work off it does and does not
  change;
- state the load-bearing insight — **the single connection is already the lock** — and
  why that means the worker needs *no* extra mutex around the store;
- read the worker + bounded queue and explain why one serial worker (not a
  goroutine-per-turn) is the right shape;
- explain the **shutdown-drain contract** and why `go reflect()` would silently lose
  learning at exit;
- explain why the reflection step's inline `/debug` notes had to move to the log.

## Prerequisites

- **Lesson 05** (the agent loop) — where the two model calls live.
- **Lessons 17 & 18** (learning) — what reflection *does*; this lesson changes *when* it runs.
- **Lesson 02** (persistent memory) — the `SetMaxOpenConns(1)` gotcha is the pivot of this lesson.

## Part 1 — the cost you were paying

Read the tail of the turn on `main`:

```text
internal/agent/agent.go   (reactLoop, near the end)
internal/agent/execute.go (finishAnswer)
```

After the final answer streams and the assistant turn is stored, the loop reaches its
learning step. Before Layer 18 it called `reflect(ctx, out, input)` directly, in the same
goroutine, *before* the deferred `close(out)`. Since `reflect` makes a second LLM call
(fact extraction) and several store queries, the channel — and therefore the caller's
sense that the turn is finished — stayed open for the whole of it.

That is the definition of *on the critical path*: work the user waits for, even though its
result does not change the answer they already have. Learning is exactly that kind of
work — valuable, but not something the next prompt should wait behind.

## Part 2 — the insight: the single connection is already the lock

Here is the reasoning that shapes the whole design, and it is worth slowing down for.

The naive worry about doing reflection concurrently is: *two goroutines will touch the
store at once and corrupt it.* Recall from Lesson 02 why the store pins a single
connection:

```text
internal/memory/store.go   (find SetMaxOpenConns(1))
```

The SQLite extensions keep the model, the embedding context, and `vector_init` in
**per-connection** state, so the pool is capped to one connection. Now here is the part
that turns a problem into a gift:

> **The core idea.** `database/sql` hands that single connection to **one caller at a
> time**. A goroutine that wants the connection while another holds it simply *blocks*
> until it is free. So a background writer (reflection) and a foreground reader (the next
> turn's recall) are **serialised for you** — not by a mutex you wrote, but by the
> connection pool you already have. Async reflection needs **no extra locking around the
> store.**

This is why the layer is safe, and why `go test -race ./internal/agent/` stays clean.
It also reframes *why the worker exists at all*. If correctness were the reason, a mutex
would do. The worker earns its place for three other reasons:

1. **Backpressure** — a bound on how much un-done learning can pile up.
2. **Ordering** — reflections happen in the order turns did.
3. **A clean drain on shutdown** — Part 4.

Naming the *real* reason keeps the design honest: the worker is a scheduling and
lifecycle tool, not a safety device.

## Part 3 — the worker and the bounded queue

Now read the machinery:

```text
internal/agent/agent.go   (reflectJob, reflectWorker, enqueueReflect; the worker fields on Agent; New)
```

The shape is small and deliberate:

- `New` creates a **bounded** channel `reflectCh` (capacity `reflectQueueCap`, 8) and
  starts **one** `reflectWorker` goroutine.
- `reflectWorker` is a plain `for job := range reflectCh` loop: it reflects on each job
  in turn, serially. One worker, so reflection's own store writes never race each other.
- The turn no longer calls `reflect` directly; it calls `enqueueReflect(input)`, which
  puts a job on the channel and returns *immediately*. The reply is already on screen;
  the turn ends now.

Why a *bounded* queue rather than an unbounded one, or a goroutine per turn? A human
converses far slower than reflection completes, so the queue almost never fills. If it
ever did (imagine a script firing turns), a bounded channel means `enqueueReflect`
**blocks briefly** — backpressure — rather than spawning unbounded goroutines or dropping
learning on the floor. The bound is a deliberate, small safety valve.

Notice also the context. The worker reflects with `a.bgCtx`, a background context created
in `New`, *not* the per-turn context. Reflection must outlive the turn that triggered it —
if it used the turn's `ctx`, the turn ending (or being cancelled) would cancel the
learning. Off the critical path means off the turn's *lifetime*, too.

## Part 4 — the shutdown contract: drain, don't drop

A background worker raises a question a synchronous call never had to answer: *what
happens to learning still in flight when the program exits?* The tempting `go reflect()`
has a bad answer — the goroutine is abandoned mid-work, and whatever you told the agent
right before you quit is silently lost.

Read the contract:

```text
internal/agent/agent.go   (Close, Quiesce)
cmd/talunor/main.go       (defer ag.Close())
```

- `Agent.Close()` **closes** `reflectCh`, which makes the worker's `for range` loop finish
  the jobs still queued and *then* exit; `Close` waits for that via `workerWG`. So closing
  the agent **drains** pending learning instead of dropping it. It is idempotent, and it
  cancels `bgCtx` only *after* the drain.
- In `cmd/talunor`, `defer ag.Close()` is registered **after** `defer store.Close()`, so
  by LIFO it runs *first* — the worker finishes writing to the store before the store
  closes underneath it. Ordering matters, and the deferred-LIFO trick makes it correct.
- `Agent.Quiesce(ctx)` blocks until the queue is empty *without* stopping the worker. It
  is what makes the change testable: a turn now returns before its reflection finishes, so
  a test that inspects the store must first wait for the worker to catch up.

The difference between `go reflect()` and this is the difference between "best-effort
background work" and "background work with a shutdown contract" — between an agent that
forgets the last thing you said and one that doesn't.

## Part 5 — async work can't narrate a closed turn

One casualty of the move is worth calling out, because the honest fix teaches something.
Before Layer 18, with `/debug` on, reflection streamed dimmed notes into the
transcript ("+fact …", "reinforced …"). Read `reflect` now:

```text
internal/agent/agent.go   (reflect — note it no longer takes an `out` channel)
```

It lost its stream parameter. It *can't* stream to the turn anymore: by the time the
worker runs, that turn's channel is already closed. Writing to it would be a bug. So the
reflection step's observability moved to the **structured log** (`a.trace` →
`TALUNOR_DEBUG` file/stderr). The *recall* trace, which runs synchronously before the
answer, is still inline; only the *reflection* half moved.

The lesson: **when a piece of work moves in time, its telemetry has to move with it.**
Rather than contort the lifecycle to keep the inline notes (keeping the turn open just to
narrate learning would undo the whole point), the honest move is to route deferred work's
observability to a sink that outlives the turn — and to say so plainly.

## Part 6 — watch it

First, the tests — they encode the two guarantees:

```bash
go test ./internal/agent/ -run 'CloseDrains|Reflection|Consolidat' -v
go test -race ./internal/agent/
```

Read `TestCloseDrainsPendingReflection`: it runs a turn, drains the *reply* stream (which
returns as soon as the answer is done), then calls `Close()` — and asserts the fact was
still stored. That is the drain contract, pinned. The `-race` run is the Part 2 claim,
verified: no data race despite a background writer and foreground reads sharing the store.

Now live (needs Ollama). Send the reflection trace to stderr and watch the *ordering*:

```bash
TALUNOR_DEBUG=stderr go run ./cmd/talunor --plain
```

```text
you> my name is Carlos and I work in Go
```

The reply comes back and the `you>` prompt returns **immediately** — you are not waiting
on extraction. A beat later, a `msg=reflect …` line appears in stderr: learning happened
*after* the turn handed control back to you. Then:

```text
you> /list
```

shows the distilled fact, now stored. You have watched learning move off the critical
path — visible in the log, invisible in your wait.

## The principles

```text
Move slow work off the path the user waits on — but give background work a lock it already has, and a contract for how it ends.
```

1. **On the critical path = the user waits for it.** Learning does not change the answer
   already given, so it should not hold the turn open.
2. **Reuse the lock you have.** `SetMaxOpenConns(1)` already serialises all store access;
   a background writer needs no extra mutex. The worker is for backpressure, ordering, and
   drain — not safety.
3. **Background work needs a shutdown contract.** Drain the queue on `Close`, don't
   fire-and-forget; use a background context so reflection outlives its turn.
4. **Telemetry follows the work in time.** Deferred work can't narrate a closed turn —
   route its observability to a sink that outlives the turn, and say so.

## Completion checklist

- [ ] I can explain what "on the critical path" cost the user before Layer 18.
- [ ] I can state why the single pinned connection means no extra lock is needed, and tie
      it to `database/sql` serialising access.
- [ ] I can give the three real reasons the worker exists (backpressure, ordering, drain).
- [ ] I read `Close`/`Quiesce` and can explain the drain contract and the deferred-LIFO ordering.
- [ ] I can explain why reflection uses `bgCtx`, not the turn's context.
- [ ] I can explain why the reflection `/debug` notes moved to the log.
- [ ] I ran the agent with `TALUNOR_DEBUG=stderr` and saw the reply return before `msg=reflect`.

---

## 🎓 About this lesson

This closes Iteration 4 — the *learning* iteration — and it does so with a systems lesson
rather than a memory one. Follow the whole arc: schema that can evolve (15), memories with
graded trust (16–17), memories with a life (17), and now learning that runs *when* it
should rather than blocking the conversation (18). The agent doesn't just remember more; it
remembers *honestly*, *selectively*, and *without making you wait*.

The concurrency here is deliberately modest — one worker, one bounded channel, one drain —
because the goal was to move work off the critical path *without* importing a whole
concurrency framework or, worse, a subtle data race. The most instructive line in the
whole layer isn't code at all: it's the realisation that the constraint you fought in
Lesson 02 (a single connection) turned out to be the very thing that made this safe. Read
your constraints twice — sometimes the wall is also the floor.

Back to the [course index](../).

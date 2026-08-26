# Lesson 08 — Observability & error handling

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🛠️ Current contribution** · Level 2 · ~45 min

> A **contribution** lesson: work on `main`, on your own branch.

## Why this lesson exists

Not every error should crash the program — but no error should vanish *silently*.
This lesson studies a real, live example in Talunor where an error is deliberately
ignored, asks whether that's right, and has you make the failure **visible** without
breaking the user's experience.

## Learning objectives

By the end you can:
- distinguish *fatal*, *recoverable*, and *best-effort* errors;
- turn a silent failure into an observable one using the agent's trace;
- explain what belongs in a debug log — and what must never.

## Prerequisites

- Lesson 05 (the agent loop). Lesson 04 helps.

## Start a branch

```bash
git switch main
git pull
git switch -c learning/observability
```

## The real case

After a turn, the agent stores the assistant's reply. Find the call — search the
agent for it:

```bash
git show v0.13.2:internal/agent/agent.go | grep -n "_, _ = a.store.Remember"
```

Note the pinned tag. On today's `main` this grep finds **nothing** — the line was
hardened in `v0.13.3`, and this lesson is what it looks like when a course claim
outlives the code it describes (Lesson 15's rule: *a claim is true against a commit*).
Read the original at `v0.13.2`, reason about it, then study the fix at the end.

At that tag you'll find:

```go
_, _ = a.store.Remember(ctx, memory.KindTurn, llm.RoleAssistant, answer)
```

The `_, _ =` throws away **both** return values, including the error. This is a
*deliberate* choice: the user already received their reply, and a storage hiccup
shouldn't retract it. That part is right. **But the error is also invisible** — if
storing the assistant turn keeps failing, long-term memory quietly goes asymmetric
(the question saved, the answer not) and nobody knows why.

That line **has** since been hardened — this lesson landed in the real project. See
exactly how, and note that the deliberate "don't retract the reply" choice survived:

```bash
git diff v0.13.2 v0.13.3 -- internal/agent/agent.go | grep -A6 "store.assistant.error"
```

On current `main` that code lives in `internal/agent/turn.go` (the loop was split out
of `agent.go` in `v0.22.5`).

## Read how observability already works

Talunor has a lightweight trace, off by default. Read:

```text
internal/agent/agent.go     # the a.trace("…", …) helper itself
# its call sites are spread across the package: grep -rn 'a.trace(' internal/agent/
cmd/talunor/main.go         # debugLogger — how TALUNOR_DEBUG is wired
```

`a.trace(...)` does nothing unless `TALUNOR_DEBUG` is set, so instrumentation is
free when disabled. See it live:

```bash
TALUNOR_DEBUG=stderr go run ./cmd/talunor --plain    # (needs Ollama for a full turn)
```

## The exercise

Make the silent store failure observable — without changing the "don't retract the
reply" behaviour. Replace the discarded error with a trace:

```go
if _, err := a.store.Remember(ctx, memory.KindTurn, llm.RoleAssistant, answer); err != nil {
    a.trace("store.assistant.error", "err", err)
}
```

The reply is still returned; the turn still completes; but now a failure leaves a
trail you can find with `TALUNOR_DEBUG`. Run the suite to confirm nothing broke:

```bash
go test ./internal/agent/ -count=1
```

## The principle

```text
Non-blocking error   ≠   invisible error.
```

Ignoring an error is acceptable **only** when the decision is explicit *and*
observable. `_, _ =` is neither obvious nor observable; a trace makes it both.

## What must never go in a log

Talunor's debug trace can include snippets of recalled memory, so it is **opt-in
and local** for a reason. When you add observability:

- **Never** log secrets, API keys, or full user content by default.
- Log *identifiers and shapes* (ids, counts, distances, error types), not raw
  personal data.

## Going further (advanced)

Testing this failure properly means injecting a store that *fails on demand* — but
the `Agent` currently depends on the concrete `*memory.Store`, not an interface. A
clean way is a small local interface:

```go
type memoryStore interface {
    Recall(context.Context, string, int, float64) ([]memory.Hit, error)
    Remember(context.Context, memory.Kind, string, string) (*memory.Memory, error)
}
```

Introduce it *only* if you actually add the error test — otherwise it's an
abstraction without a customer. (Recognising *when* an interface earns its keep is
itself the lesson.)

## Completion checklist

- [ ] I found the `_, _ = a.store.Remember(...)` call and can explain why the error
      was ignored — and why that's still not ideal.
- [ ] I replaced it with a traced version, keeping the reply intact.
- [ ] `go test ./internal/agent/` still passes.
- [ ] I can name two things that must never appear in a log.
- [ ] I can explain, in one sentence, "non-blocking ≠ invisible".

**Next:** [Lesson 09 — Secure web fetching (SSRF)](../09-secure-web-fetching/), an
**advanced** security lesson.

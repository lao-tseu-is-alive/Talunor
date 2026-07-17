# Lesson 05 — Follow the agent loop

**🔍 Historical exploration** · Level 2 · ~60 min

## Why this lesson exists

Everything else — memory, the LLM, tools, safety — exists to serve **one turn of
the agent loop**. If you understand a single turn, the whole project falls into
place. The trick is to read it *before* it grew complex. So this lesson pins to
**`v0.4.0`**, the first version with an agent, where the loop is at its simplest:
recall → build a prompt → ask the model → remember. Then you'll watch it grow.

## Learning objectives

By the end you can:
- trace one turn from user input to a stored reply, without reading line by line;
- name two invariants the loop protects and say why they matter;
- explain how the loop later grew a *tool* phase (the ReAct loop) — by diffing two
  tags yourself.

## Prerequisites

- Lessons 00 and 01 done.
- A first hour of [A Tour of Go](https://go.dev/tour) if channels/goroutines are new
  (this lesson uses both, gently).

## Check out the simple loop

```bash
git checkout v0.4.0     # detached HEAD — read only (see Lesson 00)
```

> **Files at this tag** (no `docs/atlas.md` here yet — here's the layout at
> `v0.4.0`):
>
> ```text
> cmd/talunor/main.go        the interactive agent, wired up (new at this tag)
> internal/agent/agent.go    the cognitive loop  ← this lesson
> internal/llm/              the Provider interface + streaming adapter (Lesson 04)
> internal/memory/           the SQLite store + embeddings (Lessons 02–03)
> internal/render/           prints the streaming reply to the terminal
> ```

Read this one file:

```text
internal/agent/agent.go     # Turn (the entry point) and learnWhileStreaming
```

## The shape of one turn (at v0.4.0)

Here is the whole `Turn`, and it fits on one screen:

```go
func (a *Agent) Turn(ctx context.Context, input string) (<-chan llm.Chunk, error) {
    // Recall against the input *before* storing it, so the current message is
    // not retrieved as its own top match.
    hits, err := a.store.Recall(ctx, input, a.cfg.RecallK, a.cfg.RecallMaxDistance)
    if err != nil {
        return nil, err
    }

    // Reason: build the prompt from prior context, then start streaming.
    msgs := a.buildMessages(hits, input)

    // Store the user turn now (it happened regardless of how the reply goes).
    a.short.Add(llm.RoleUser, input)
    if _, err := a.store.Remember(ctx, memory.KindTurn, llm.RoleUser, input); err != nil {
        return nil, err
    }

    stream, err := a.provider.Chat(ctx, msgs, a.cfg.Options)
    if err != nil {
        return nil, err
    }

    // Tee the stream to the caller while accumulating the answer; store it on
    // clean completion.
    out := make(chan llm.Chunk)
    go a.learnWhileStreaming(ctx, stream, out)
    return out, nil
}
```

As a diagram:

```text
input
  │
  ▼
Recall      a.store.Recall(...)         → relevant past memories (by meaning)
  │
  ▼
Build       a.buildMessages(hits, input)→ [system prompt, memories, recent turns, input]
  │
  ▼
Store user  a.store.Remember(...user...)  (the user's message definitely happened)
  │
  ▼
Reason      a.provider.Chat(msgs, opts) → a live *stream* of reply chunks
  │
  ▼
Learn       learnWhileStreaming(...)    → forward chunks to you; on clean end,
                                          store the assistant's reply
```

`buildMessages` assembles the prompt in a fixed order — system prompt, a block of
recalled memories, the recent short-term turns, then the new input — and
`learnWhileStreaming` does the clever part: it **passes each chunk straight to you**
(so you see the reply appear live) while quietly accumulating the full text, and
saves it **only if the stream finishes cleanly**.

## Guided exploration

Find each of these in the code and be able to point at the line:

1. **Recall happens *before* the user's message is stored.** Why? (Hint: read the
   comment. What would happen if you stored first, then searched?)
2. **A failed or half-finished reply is never stored.** Where does
   `learnWhileStreaming` decide that? Why is a partial answer worse than none?
3. **Recalled memories are injected as a system message.** Find it in
   `buildMessages`. (Keep this in mind — see *How it grew* below.)

## How it grew (the payoff)

The loop you just read is the *skeleton*. On later tags it gained muscles. See it
with your own eyes — this is a safe, offline `git` command:

```bash
git diff v0.4.0 v0.7.0 -- internal/agent/agent.go
```

At **`v0.7.0`**, `learnWhileStreaming` is replaced by **`runLoop`** — the **ReAct
loop**: the model can now ask for a *tool*, the agent runs it, feeds the result
back as an observation, and calls the model again, until it answers (bounded by a
`MaxToolIters` cap so it can't loop forever). Reflection (distilling durable facts)
arrived at `v0.6.0`.

Two details in that early code were **hardened only much later** — a nice reminder
that a teaching repo shows its scars honestly:

- The best-effort `_, _ = a.store.Remember(...)` for the assistant turn (the error
  is swallowed) is still there today — Lesson 08 studies exactly that.
- Injecting memories as a *system* message became a **prompt-injection** concern
  once memories can contain arbitrary recalled text; it was fenced and labelled as
  untrusted data in **`v0.10.1`**. Compare:
  ```bash
  git diff v0.4.0 v0.10.1 -- internal/agent/agent.go | grep -A4 recalled_memories
  ```

Return to the latest code when you're done:

```bash
git switch main
```

## Common mistakes

- **Trying to read the *current* `agent.go` first.** On `main` it also carries the
  tool loop, approval gate, and debug tracing — start from `v0.4.0`, then diff.
- **Reading line by line.** Aim for the *shape*: recall → build → reason → learn.
  The details are labelled by comments when they matter.

## Completion checklist

- [ ] I can trace one turn: input → recall → build → reason → learn.
- [ ] I found where recall happens *before* storing, and can say why.
- [ ] I found where a partial/failed reply is *not* stored.
- [ ] I ran `git diff v0.4.0 v0.7.0 -- internal/agent/agent.go` and can say, in one
      sentence, what `runLoop` added.
- [ ] I returned to `main`.

**Next:** [Lesson 06 — Build your first tool](../06-build-your-first-tool/) (your
first 🛠️ contribution on `main`).

# Lesson 04 — LLM provider & streaming

**🔍 Historical exploration** · Level 2 · ~60 min

## Why this lesson exists

So far Talunor could remember, but not *think*. This layer adds the LLM — and does
it in a way worth studying: the agent never talks to Ollama (or OpenRouter)
directly. It talks to a tiny **interface**. That one decision is what makes the
whole system testable. You'll read it at **`v0.3.0`**, where the LLM layer is
brand new and uncluttered.

## Learning objectives

By the end you can:
- explain why the agent depends on an *interface* (`llm.Provider`) instead of a
  concrete client;
- describe how a reply is **streamed** back over a Go channel;
- write a fake provider that returns a canned reply (the trick behind Talunor's
  deterministic tests).

## Prerequisites

- Lessons 00–02. A little comfort with Go **interfaces** and **channels**; if
  those are new, the first hour of [A Tour of Go](https://go.dev/tour) is enough.

## Check out the LLM layer

```bash
git checkout v0.3.0     # detached HEAD — read only (see Lesson 00)
```

> **Files at this tag** (memory from Lesson 02, now plus an LLM layer):
>
> ```text
> internal/llm/llm.go        the Provider interface + Message / Chunk / Options types
> internal/llm/openai.go     the one concrete adapter (OpenAI-compatible: Ollama, OpenRouter)
> internal/llm/openai_test.go  tests it against a fake HTTP server — no real model
> cmd/chat/main.go           a tiny program that streams one prompt to a model
> internal/memory/…          (unchanged from Lesson 02)
> ```
>
> Still no agent yet — that arrives at `v0.4.0` (Lesson 05).

## Read, in this order

```text
internal/llm/llm.go          # the contract — small on purpose
internal/llm/openai.go       # the implementation — SSE streaming
internal/llm/openai_test.go  # how it's tested without a live model
```

## The contract is tiny

Here is the whole interface (at `v0.3.0`):

```go
type Provider interface {
    // Name identifies the provider (e.g. "ollama") for logs and errors.
    Name() string
    // Chat starts a streaming completion. Setup failures (bad request, connection
    // refused, non-200) are returned as the error; failures mid-stream arrive as a
    // Chunk with Err set. The channel closes when the completion ends or ctx is cancelled.
    Chat(ctx context.Context, msgs []Message, opts Options) (<-chan Chunk, error)
}
```

Two methods. That's the *entire* dependency the rest of Talunor has on "the LLM".
`Chat` returns a **channel of `Chunk`s** — the reply arrives piece by piece, so the
UI can show it live instead of waiting for the whole thing. A `Chunk` carries
`Content` (and `Reasoning`, for "thinking" models); a non-nil `Chunk.Err` is the
last one on the channel.

## Why an interface? (the whole point)

Because the agent depends on `Provider`, not on a concrete Ollama client, you can
hand it *anything* that satisfies those two methods — including a fake. That buys:

- **testability** — swap the real model for a deterministic double (no network, no
  cost, no "the model felt creative today");
- **interchangeability** — Ollama, OpenRouter, or a new provider, behind one seam;
- **decoupling** — the agent's logic doesn't know or care which model answers.

Read `openai_test.go`: it tests the *real* adapter against a fake **HTTP** server
(`httptest`) — same idea, one level down.

## Write a fake provider

Here's a complete, compiling fake (this is exactly the shape Talunor's tests use).
Read it and make sure you understand every line:

```go
type FixedProvider struct{}

func (FixedProvider) Name() string { return "fixed" }

func (FixedProvider) Chat(ctx context.Context, msgs []llm.Message, opts llm.Options) (<-chan llm.Chunk, error) {
    out := make(chan llm.Chunk, 1)
    out <- llm.Chunk{Content: "Hello from the fake provider"}
    close(out)                 // one chunk, then the channel closes = reply done
    return out, nil            // nil error = setup succeeded
}
```

Notice it never touches the network. Anything holding a `Provider` — including the
agent you'll meet in Lesson 05 — can't tell it apart from the real thing. *That* is
how you test an AI agent without a real AI.

## Experiment (optional — needs Ollama)

If you have Ollama running, stream a real reply:

```bash
make chat PROMPT="explain vector search in one sentence"
```

Watch the words appear progressively — that's the channel of `Chunk`s draining in
real time. No Ollama? Skip it; you've already read the more important thing (the
test, and the fake).

Return to the latest code when done:

```bash
git switch main
```

## Questions to answer

- What are the *only* two things the rest of Talunor needs from "the LLM"?
- How does a streamed reply arrive, and why is streaming better than waiting for the
  full text?
- Setup errors vs mid-stream errors are handled differently — where does each go
  (the returned `error` vs a `Chunk.Err`)? Why separate them?

## Common mistakes

- **Getting the signature wrong.** `Chat` returns `(<-chan llm.Chunk, error)` and
  the provider must also implement `Name()`. A fake missing either won't satisfy
  the interface — the compiler will tell you.
- **Forgetting to `close` the channel.** A reader ranges over the channel; if you
  never close it, the reader blocks forever.

## Completion checklist

- [ ] I can state the two methods of `llm.Provider` from memory.
- [ ] I can explain why depending on the interface makes the agent testable.
- [ ] I understand how a reply is streamed over a channel.
- [ ] I read `FixedProvider` and can explain why it's indistinguishable from a real
      provider to its caller.
- [ ] I returned to `main`.

**Next:** [Lesson 05 — Follow the agent loop](../05-follow-the-agent-loop/) — now
you've met memory *and* the model, watch them come together in one turn.

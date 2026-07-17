# Lesson 07 — Test without a real LLM

**🛠️ Current contribution** · Level 2–3 · ~75 min

> A **contribution** lesson: work on `main`, on your own branch.

## Why this lesson exists

How do you test an agent when the "brain" is a large language model that gives a
slightly different answer every time? You **don't call the real model.** You feed
the agent a *scripted* fake that returns exactly the chunks you choose. This lesson
shows Talunor's testing tricks — and you'll write one yourself.

## Learning objectives

By the end you can:
- explain why an agent test must not depend on a live model;
- use a **scripted provider** to drive a deterministic tool-call sequence;
- write a behavioural test that asserts *what the agent did*, not implementation
  details.

## Prerequisites

- Lesson 04 (the `Provider` interface + the idea of a fake provider).
- Lesson 06 helps — you'll test the tool you built there.

## Start a branch

```bash
git switch main
git pull
git switch -c learning/agent-tests
```

## Read the testing toolkit

```text
internal/agent/agent_test.go   # fakeProvider, scriptedProvider, fakeTool — the doubles
internal/llm/openai_test.go    # tests the real adapter against an httptest SSE server
internal/tui/tui_test.go       # drives the TUI with no terminal (synthetic tea.Msgs)
```

Three levels of the same idea — *replace the non-deterministic thing with a
deterministic double*:

- the **agent** tests swap the LLM for a `scriptedProvider`;
- the **llm** tests swap the network for an `httptest` server;
- the **tui** tests swap the terminal for synthetic messages and assert on
  `View()`.

## How a scripted provider works

`scriptedProvider` returns one canned response per `Chat` call. To test a tool
loop, you script two steps: *"call this tool"*, then *"here's the final answer"*:

```go
prov := &scriptedProvider{steps: [][]llm.Chunk{
    // Turn 1: the model asks for a tool (a terminal tool-call chunk).
    {{ToolCalls: []llm.ToolCall{{ID: "c1", Name: "calculator", Args: `{"expression":"2+2"}`}}}},
    // Turn 2: with the observation in context, it answers.
    {{Content: "It's 4."}},
}}
```

The agent runs the tool between the two calls, feeds the result back, and the
second scripted response becomes the reply — all with **no network and no real
model**. See `TestReActToolLoop` in `agent_test.go` for the full pattern.

## The exercise

Write a test that drives **the `unit_convert` tool you built in Lesson 06** through
the agent, deterministically. In `internal/agent/agent_test.go` (or a new
`*_test.go`), model it on `TestReActToolLoop`:

1. Build a `scriptedProvider` whose first step asks for `unit_convert`
   (`Args: '{"value":5,"from":"km"}'`), and whose second step is a final sentence.
2. Register the tool: `cfg.Tools = tools.NewRegistry(tools.UnitConvert{})`.
3. Disable reflection (`cfg.Extractor = DisableReflection()`) so the call count is
   exact.
4. Run a turn, drain the stream, and assert:
   - the final answer came through;
   - the **tool observation** (the miles value) reached the model — scan
     `prov.lastMsgs` for a `RoleTool` message containing it.

Run it:

```bash
go test ./internal/agent/ -run UnitConvert -v
```

## The central point

> A good agent test pins **behaviour**, not the model's mood. "Given this scripted
> model and this tool, the agent runs the tool and feeds the result back" is a fact
> you can assert every time — because you control both ends.

## Common mistakes

- **Asserting on exact wording.** Don't assert the model "said X" — you scripted
  that. Assert on *structure*: the tool ran, the observation flowed back, the turn
  was stored.
- **Leaving reflection on.** It makes an extra model call, throwing off a
  `scriptedProvider` with a fixed number of steps. Disable it in the test.

## Completion checklist

- [ ] I can explain why agent tests use a scripted provider instead of a real LLM.
- [ ] I wrote a test that drives a tool call → observation → final answer.
- [ ] My test asserts the observation reached the model (behaviour, not wording).
- [ ] The test passes and needs no network.

**Next:** [Lesson 08 — Observability & error handling](../08-observability-and-errors/).

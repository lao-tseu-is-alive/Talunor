# Lesson 01 — First contact & first win

**🔍 Historical exploration** (with an optional 🛠️ run on `main`) · Level 1
(beginner) · ~30 min

## Why this lesson exists

The fastest way to lose a beginner is a first step that doesn't work. So this
lesson gets you a **real, running win in the first ten minutes** — with no LLM, no
Ollama, no network — and only then shows you the interactive agent. Along the way
you'll see where Talunor started and build a first mental model of the whole
system.

## Learning objectives

By the end you can:
- run Talunor's offline memory smoke test and read its output;
- describe what Talunor *is*, in one paragraph;
- point at the seed of the project (`v0.1.0`) and say what it did;
- draw a first, rough architecture diagram.

## Prerequisites

- Lesson 00 done.
- Go 1.26+ and a C compiler installed (`go version`, `gcc --version`).

## Step 1 — your first win (offline, on `main`)

From the repo root, on `main`:

```bash
make deps     # one-time: downloads SQLite extensions + embedding model (~52 MB)
make doctor   # runs an offline smoke test of the memory substrate
```

`make doctor` needs **no LLM and no network** — the embeddings run locally. You
should see something like:

```text
✓ store open — embedding dimension = 384
• remembering corpus…
✓ stored 5 memories
• recall: "Which technology keeps a whole database in one file?"  (threshold d≤0.75)
   1. [d=0.2405] SQLite stores an entire relational database in a single file.
• recall: "Tell me about a famous French landmark."  (threshold d≤0.75)
   1. [d=0.6189] The Eiffel Tower was completed in Paris in 1889.
✓ Layers 1–2 OK: in-DB embeddings, KNN recall (thresholded), short-term buffer.
```

**That's the win.** Notice: the query *"a famous French landmark"* recalled the
*Eiffel Tower* memory even though they share no words. That's **semantic search** —
matching by *meaning*, not keywords. It's the foundation everything else is built
on. (You'll dig into it in Lesson 03.)

## Step 2 — see where it all started (`v0.1.0`)

Now travel back to the very first layer:

```bash
git checkout v0.1.0     # detached HEAD — read only, don't commit (see Lesson 00)
```

> **Files at this tag** (there is no `docs/atlas.md` yet — that map is a recent
> addition; here's the whole project at `v0.1.0` instead):
>
> ```text
> cmd/doctor/main.go            the only program: remember a corpus, recall by meaning
> internal/memory/store.go      open SQLite, load the vector/AI extensions, the schema
> internal/memory/cgo_link.go   the cgo glue that makes the C extensions loadable
> internal/version/version.go   the version constant
> Makefile · README.md · CHANGELOG.md
> ```

At `v0.1.0`, Talunor is **only a memory store** — there is no agent, no chat, no
tools. There isn't even a `cmd/talunor` yet; the only program is `cmd/doctor`, the
same smoke test you just ran. Open it:

```text
cmd/doctor/main.go      # ~one screen: open a store, remember a few facts, recall by meaning
```

Read it top to bottom. It's short on purpose — this is the seed the whole agent
grew from. Your `ext/` folder is still there (git ignores it), so you can even run
it here:

```bash
make doctor             # works at v0.1.0 too — same idea, smaller code
```

When you're done, come back:

```bash
git switch main
```

> **What changed since?** The interactive agent (`cmd/talunor`) first appears at
> **`v0.4.0`**, once there's an LLM and an agent loop to drive. You'll follow that
> loop in Lesson 05.

## Step 3 — talk to the agent (optional, needs Ollama)

To actually *chat* with Talunor you need a local [Ollama](https://ollama.com)
running a model. With that in place:

```bash
make run          # launches the TUI (needs a terminal + Ollama)
# or, simpler to read as a beginner:
go run ./cmd/talunor --plain    # a plain line-based REPL, no fancy UI
```

Type a message, then another that refers back to it — the agent *remembers* across
turns because it stored the first one. Type `/help` to see the commands, `/exit` to
quit. If you don't have Ollama yet, skip this step; the rest of the course only
*needs* it occasionally, and always says so.

## Mental model

From what you've seen, Talunor looks like this:

```text
You  (terminal: TUI, or --plain REPL)
  │
  ▼
Agent  — one "turn": recall memories, ask the LLM, maybe use a tool, remember
  │
  ├─► Memory   (SQLite: your turns + facts, searched by meaning)
  ├─► LLM      (Ollama or OpenRouter — the "thinking")
  └─► Tools    (calculator, clock, memory search, and opt-in bash / web_fetch)
```

Keep this picture; the next lessons zoom into each box.

## Common mistakes

- **Skipping `make deps`.** Without it, `make doctor` can't find the extensions or
  the model. Run it once.
- **Expecting `make run` to work without Ollama.** Chat needs a model; the offline
  win (`make doctor`) does not.
- **Committing while on `v0.1.0`.** You're in detached HEAD — read only.

## Completion checklist

- [ ] `make doctor` ran and I saw the recall output.
- [ ] I can explain, in one sentence, why *"French landmark"* recalled *Eiffel
      Tower* (semantic search).
- [ ] I read `cmd/doctor/main.go` at `v0.1.0` and returned to `main`.
- [ ] I can say what `v0.1.0` did and what `v0.4.0` added.
- [ ] I can draw the four-box mental model from memory.

**Next:** [Lesson 05 — Follow the agent loop](../05-follow-the-agent-loop/) *(the
pilot skips ahead to the heart of the system; Lessons 02–04 fill in memory,
embeddings, and the LLM provider).*

# Talunor — a hands-on course in Go, AI agents, and safe-by-design code

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

Talunor is built **one layer at a time, each layer a git tag** (`v0.1.0`,
`v0.2.0`, …). That history is not just a changelog — it's a **course**. You can
check out an early tag to see the project when it was small and simple, understand
one idea in isolation, then come back to the latest code.

This directory turns that idea into a guided path. Each lesson has a clear goal, a
short reading list, a hands-on experiment, and a checklist so you know when you're
done.

> **Status: in progress.** Lessons 00–23 are ready; Iteration 5 is being built.

## Who this is for

Developers who know a little programming and want to learn, by reading and running
real code:
- **Go** — its interfaces, channels, tests, and idioms.
- **AI agents** — memory, retrieval, the reason→act loop, tools, approval.
- **Safe-by-design code** — input validation, SSRF, sandboxing, supply chain.

**You do not need to know Go well.** If Go is brand new, spend an hour on
[A Tour of Go](https://go.dev/tour) first — that's enough to follow along. Some
lessons are marked **Advanced**; it's completely fine to stop before them and come
back later.

## Prerequisites

- **Go 1.26+** and a **C compiler** (gcc/clang) — Talunor uses cgo.
- **git**, and a **Linux x86_64** machine (the smoothest path).
- **Ollama** is only needed from Lesson 01's *optional* step onward — the first
  win runs fully offline.

One-time setup (downloads the SQLite extensions + embedding model, ~52 MB):

```bash
git clone https://github.com/lao-tseu-is-alive/Talunor.git
cd Talunor
make deps
make doctor   # your first win — the memory substrate, running offline
```

## What you'll learn — competency matrix

The course is a narrative, but here is the map of the *skills* underneath it: what
each competency means, which lessons build it, the level you should reach, and where
you prove it. Lesson numbers refer to [the route](#the-route) below.

| Competency | Lessons | Expected level | Prove it |
|---|---|---|---|
| **Go interfaces & composition** | 04 · 06 · 07 | Design a one-method seam and swap the real implementation for a fake | Add a tool (06) and test it against a fake provider (07) |
| **Context, cancellation & timeouts** | 04 · 16 · 19 | Explain context propagation, a bounded operation, and a clean shutdown | Read the `Close`/drain contract (19); reason about a hung provider (16) |
| **Concurrency & the Go memory model** | 05 · 18 · 19 | Justify why shared state needs an atomic/lock, and why one connection can stand in for one | Run `go test -race`; read the single-connection-as-lock insight (19) |
| **Persistence & retrieval (SQLite + embeddings)** | 02 · 03 · 23 | Explain how a text query becomes a ranked, thresholded recall — and what an embedding cannot represent | Tune the recall threshold and watch the results change (03); make an identifier unfindable, then findable (23) |
| **Agentic memory: provenance · confidence · salience** | 11 · 16 · 17 · 18 · 20 | Separate embedding-provenance from fact-provenance; explain why confidence is system-assigned, never self-reported; learn from action without over-claiming | Read migrations 2–3 and `salience.go` (17, 18); trace a fact to its evidence with `/why` (20) |
| **The agent loop & tools (ReAct)** | 05 · 06 · 13 | Trace a tool call from the model's request to the observation fed back | Watch `/debug` follow a tool call end to end (05, 06) |
| **Agent safety: injection · policy · sandbox · SSRF** | 09 · 10 · 12 · 14 · 21 | Name which layer stops which threat — and why fencing text is a mitigation, not a boundary; decide a memory's trust model on purpose | Read the policy gate (12), the approval post-mortem (14), the trust model (21) |
| **Trustworthy evaluation & verification** | 07 · 15 · 16 · 22 | Build a deterministic check (no LLM judge), falsify a claim against the code, and know what your green suite did *not* run | Do the "verify the AI review" exercise (15); read the matchers (16); audit your suite's skips and make a privileged decision testable without the privilege (22) |

## The route

| Lesson | Subject | Level | ~Time | Read at | Status |
|--------|---------|-------|-------|---------|--------|
| [00](00-how-to-use-this-course/) | How to use this course | 0 · orientation | 15 min | — | ✅ ready |
| [01](01-first-contact/) | First contact & first win | 1 · beginner | 30 min | `v0.1.0` → `main` | ✅ ready |
| [02](02-persistent-memory/) | Persistent memory with SQLite | 1 · beginner | 45 min | `v0.2.0` | ✅ ready |
| [03](03-semantic-recall/) | Semantic recall & embeddings | 2 · **advanced** | 60 min | `v0.2.0` | ✅ ready |
| [04](04-llm-provider-and-streaming/) | LLM provider & streaming | 2 | 60 min | `v0.3.0` | ✅ ready |
| [05](05-follow-the-agent-loop/) | Follow the agent loop | 2 | 60 min | `v0.4.0` → `v0.7.0` | ✅ ready |
| [06](06-build-your-first-tool/) | Build your first tool | 2 · 🛠️ contribution | 90 min | `main` | ✅ ready |
| [07](07-test-without-a-real-llm/) | Test without a real LLM | 2–3 · 🛠️ | 75 min | `main` | ✅ ready |
| [08](08-observability-and-errors/) | Observability & error handling | 2 · 🛠️ | 45 min | `main` | ✅ ready |
| [09](09-secure-web-fetching/) | Secure web fetching (SSRF) | 3 · **advanced** | 75 min | `v0.10.0` | ✅ ready |
| [10](10-understand-the-sandbox/) | Understand the sandbox | 4 · **advanced** | 90 min | `v0.9.0` | ✅ ready |
| [11](11-when-memory-forgets/) | When memory silently forgets: provenance & observability | 3 · **advanced** | 75 min | `v0.11.0` → `main` | ✅ ready |
| [12](12-the-open-bar/) | The open bar: why an agent needs a policy | 3 · **advanced** | 75 min | `v0.12.0` → `main` | ✅ ready |
| [13](13-plan-before-you-act/) | Plan before you act: from ReAct to a plan you can read | 3 · **advanced** | 90 min | `v0.13.0` → `main` | ✅ ready |
| [14](14-the-approval-that-didnt-bind/) | The approval that didn't bind: a plan-mode security post-mortem | 3 · **advanced** | 60 min | `v0.13.1` → `main` | ✅ ready |
| [15](15-dont-trust-the-review/) | Don't trust the review: verifying what an AI claims about your code | 2 · meta | 60 min | `main` | ✅ ready |
| [16](16-measure-the-model/) | Measure the model: building a reliability canary | 3 · **advanced** | 75 min | `main` | ✅ ready |
| [17](17-learning-with-humility/) | Learning with humility: what a memory is worth | 3 · **advanced** | 75 min | `main` | ✅ ready |
| [18](18-the-memory-of-the-gesture/) | The memory of the gesture: salience, decay & consolidation | 3 · **advanced** | 75 min | `v0.17.0` → `main` | ✅ ready |
| [19](19-off-the-critical-path/) | Off the critical path: learning in the background | 3 · **advanced** | 70 min | `v0.18.0` → `main` | ✅ ready |
| [20](20-learn-from-action/) | Learn from action: most "tool knowledge" is model-interpreted | 3 · **advanced** | 65 min | `v0.19.0` → `main` | ✅ ready |
| [21](21-whose-word-counts/) | Whose word counts? A trust model is a decision, not a default | 3 · **advanced** | 65 min | `v0.20.0` → `main` | ✅ ready |
| [22](22-the-silent-suite/) | The silent suite: a skipped test is not a passing test | 3 · **advanced** | 70 min | `v0.20.1` → `v0.20.2` | ✅ ready |
| [23](23-two-ways-to-find-a-memory/) | Two ways to find a memory: when meaning is the wrong index | 3 · **advanced** | 75 min | `v0.21.0` | ✅ ready |

## Two kinds of lesson — don't mix them up

Every lesson is one of two kinds, marked at the top with a badge:

**🔍 Historical exploration** — you `git checkout` an old tag to *read* how Talunor
looked at that stage. You are in "detached HEAD". **Never commit here.** When
you're done, `git switch main` to return.

**🛠️ Current contribution** — you change the *current* project. Always start from
`main` and create a branch: `git switch main && git pull && git switch -c learning/my-change`.

Lesson 00 explains this in detail; it's the one thing that trips people up.

## The reference docs

Keep these open as you go — **read them from `main`** (older tags have fewer of
them; Lesson 00 explains why, and each historical lesson maps its own tag):

- **[README.md](../../README.md)** — what Talunor is, quickstart, tools, layout.
- **[CHANGELOG.md](../../CHANGELOG.md)** — the layer-by-layer build log with a
  *"Lessons learned"* section per release. This is the heart of the project.
- **[AGENTS.md](../../AGENTS.md)** — architecture map, conventions, hard-won gotchas.
- **[docs/atlas.md](../atlas.md)** — an annotated map of every file (latest versions).

## How to work through a lesson

1. Read *Why this lesson exists* and *Learning objectives*.
2. Do the checkout (or branch) it asks for.
3. Read the listed files — no need to read line by line; aim for the *shape*.
4. Run the commands and do the experiment.
5. Tick the **Completion checklist**. If every box is checked, move on.

Take your time. The goal isn't speed — it's being able to *explain* how each piece
works and why it was built that way.

# Talunor — a hands-on course in Go, AI agents, and safe-by-design code

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

Talunor is built **one layer at a time, each layer a git tag** (`v0.1.0`,
`v0.2.0`, …). That history is not just a changelog — it's a **course**. You can
check out an early tag to see the project when it was small and simple, understand
one idea in isolation, then come back to the latest code.

This directory turns that idea into a guided path. Each lesson has a clear goal, a
short reading list, a hands-on experiment, and a checklist so you know when you're
done.

> **Status: complete.** All twelve lessons (00–11) are ready.

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

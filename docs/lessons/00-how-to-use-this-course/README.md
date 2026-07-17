# Lesson 00 — How to use this course

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🔍 Historical exploration** · Level 0 (orientation) · ~15 min

## Why this lesson exists

This course asks you to travel through Talunor's history with `git`. Before you do
that, you need to know how to move between versions **safely** — and, crucially,
how to tell the difference between *reading old code* and *changing current code*.
Getting this right once saves a lot of confusion later.

## Learning objectives

By the end you can:
- list Talunor's versions and check one out;
- explain what "detached HEAD" means and why you must not commit there;
- return cleanly to the latest code;
- name the four reference documents and what each is for.

## Prerequisites

- `git` installed. That's it — no Go needed for this lesson.

## The big idea: one layer = one tag

Talunor was built in small steps. Each step is a **git tag** and adds one
capability, with a lesson recorded in the `CHANGELOG.md`. List them oldest-first:

```bash
git tag --sort=version:refname
```

You'll see `v0.1.0`, `v0.2.0`, … up to the latest. `v0.1.0` is Talunor when it was
just a memory store — a handful of files. Later tags add the LLM, the agent loop,
tools, safety, and so on. Reading them in order is like watching the project grow.

## Detached HEAD — read, don't write

To look at an old version, you check out its tag:

```bash
git checkout v0.1.0
```

git will print a warning about being in **"detached HEAD" state**. That sounds
scary but simply means: *you are looking at a snapshot, not at a branch.* You can
read, run, and experiment freely — but **any commit you make here is easy to lose**
and does not belong to the project. So the rule is:

> While exploring a tag, **never commit**. Just read and run.

When you're done exploring, go back to the latest code:

```bash
git switch main
```

## Try it

```bash
git checkout v0.1.0          # jump to the very first layer
ls internal/                 # notice how little there is: just memory/
git switch main              # come back to the latest
ls internal/                 # now: agent, llm, tools, sandbox, webfetch, …
```

That contrast — a few files then many — *is* the story of the project.

## The two kinds of lesson

Every lesson is one of two kinds. Always check the badge at the top:

| Badge | What you do | Where | Commit? |
|-------|-------------|-------|---------|
| 🔍 **Historical exploration** | read how a layer worked | `git checkout vX.Y.Z` (detached) | **No** |
| 🛠️ **Current contribution** | change the current project | branch from `main` | **Yes**, on your branch |

For a contribution lesson you start like this instead:

```bash
git switch main
git pull
git switch -c learning/my-first-change   # a new branch to work on
```

## The reference docs — read these on `main`

Keep these open throughout the course. **Read them from `main`** (the latest
state), even while you explore code at an old tag: they describe the current,
complete project, and — importantly — **older tags have fewer of them.**

- **`README.md`** — the tour: purpose, quickstart, tools, layout. *(since `v0.1.0`)*
- **`CHANGELOG.md`** — the diary: every version with a *"Lessons learned"* note.
  When you wonder *why* something is the way it is, look here first. *(since `v0.1.0`)*
- **`AGENTS.md`** — the map: architecture, conventions, and *hard-won gotchas*
  (traps the authors already hit, so you don't have to). *(added at `v0.6.0`)*
- **`docs/atlas.md`** — a one-line description of every file in the repo.
  *(a recent addition — only on the latest versions)*

That last point is a lesson in itself: **a project's documentation grows with its
code.** So when a lesson sends you to an *old tag*, read the *code* there — but if
you need the map or the conventions, look at `main`, where they're complete. Each
historical lesson also gives you a small "files at this tag" map so you're never
lost.

## Common mistakes

- **Committing while on a tag.** If you did, don't panic: `git switch main` and
  your accidental commit is simply left behind (create a branch first if you want
  to keep it).
- **Editing files while exploring history** and being surprised they "come back"
  when you switch to `main` — that's expected; the tag and `main` are different
  snapshots.

## Completion checklist

- [ ] I listed the tags with `git tag --sort=version:refname`.
- [ ] I checked out `v0.1.0` and saw the smaller `internal/` layout.
- [ ] I returned to `main` with `git switch`.
- [ ] I can explain, in one sentence, the difference between a *historical
      exploration* and a *current contribution* lesson.
- [ ] I know what each of the four reference docs is for.

**Next:** [Lesson 01 — First contact & first win](../01-first-contact/).

# Contributing to Talunor

Talunor is a **course that happens to be a program**: one layer per release, each
with a documented lesson. That shapes what a good contribution looks like here, so
this page is short and mostly points at the files that are already the authority.

**It deliberately does not restate the rules it links to.** A second copy of a
mechanism does not stay a copy — it becomes an older, weaker version of it, and the
part that rots first is the part that matters (that is the lesson of `v0.23.9`, and
it applies to this file too).

## Where your pull request goes

| You are… | The PR goes to |
|---|---|
| working a **course exercise** (Lessons 06–08 ask you to write code) | **your own fork** — see [Lesson 06 › Proposing your patch](docs/lessons/06-build-your-first-tool/README.md#proposing-your-patch) |
| fixing or extending **the project itself** | here, against `main` |

Course exercises are not sent upstream. Every reader of Lesson 06 writes the same
`unit_convert` tool; the hundredth identical PR costs a maintainer real time and adds
nothing the project wants — and `make release-check` refuses it anyway, because the
answer must not sit next to the question. The worked solution lives on the
[`solutions/06-unit-convert`](../../tree/solutions/06-unit-convert) branch.

> A patch is judged by what it changes for the maintainer, not by what it cost you.

## Before you open it

1. **Open an issue first**, stating the problem, before writing the fix. The cheapest
   patch to review is the one whose problem was agreed on first.
2. **`make release-check` must pass.** It is gofmt + vet + tests plus six drift
   alarms (dependency checksums, `docs/atlas.md`, the README banner, the changelog,
   the lessons, the reference docs). Expect `atlas-check` to catch a new file you
   added to the project but not to the map of it — that is the alarm working.
3. **Read your own diff** — `git diff main...HEAD`, the whole thing.

## What the project expects

All of it is in **[`AGENTS.md`](AGENTS.md)**, which is the working agreement, not a
summary of one:

- **"How it is built"** — the release ritual: version bump, a `CHANGELOG.md` section
  **with its "Lessons learned"**, README + `AGENTS.md` in sync, `docs/atlas.md`
  regenerated if files moved, `make release-check`, then the tag.
- **"Next — open threads"** — the work that is actually wanted, ordered by what a
  failure would cost. Start there rather than guessing.
- **"Hard-won gotchas"** — read before touching SQLite extensions, the sandbox, the
  TUI render loop, or a build tag. Each entry is a day someone already spent.
- **"Testing conventions"** — in particular: never `t.Skip` a capability directly, go
  through `internal/testenv`.

Two rules worth stating here because they surprise people:

- **A layer owes a bilingual lesson.** If your change is a new capability, it is not
  finished until `docs/lessons/` has it in English *and* French. A feature without its
  lesson is half of what this project ships.
- **Claims are checked, not trusted.** `docs/lessons/assertions.sh` and
  `docs/assertions.sh` re-derive what the documentation asserts, on every
  `release-check`. If your change makes a page's claim false, fix the page and its
  assertion in the same commit.

## Commits

Conventional Commits, matching the history:

```text
feat(tools): add a unit_convert tool (km→mi, c→f, kg→lb)
fix(memory): reject a schema newer than this binary understands
docs(lessons): …
```

The body says **what changes, why it is wanted, how you know it works, and what it
does not do**. Name the boundary — a reviewer trusts a diff with a stated limit more
than one that implies it is complete.

## Getting set up

```bash
make deps    # required once: fetches the SQLite extensions + the embedding model
make test
make capabilities   # what this host can actually exercise
```

`CGO_ENABLED=1` is mandatory and the FTS5 build tag is part of the feature contract —
both are handled by the Makefile. If you add a `go` command anywhere, pass
`$(GOFLAGS_TAGS)`.

New here? [Lesson 00](docs/lessons/00-how-to-use-this-course/README.md) is the way in,
and [Lesson 06](docs/lessons/06-build-your-first-tool/README.md) walks a first patch
end to end, including the pull request.

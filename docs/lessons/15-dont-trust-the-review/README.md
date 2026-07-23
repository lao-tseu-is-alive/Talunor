# Lesson 15 — Don't trust the review: verifying what an AI claims about your code

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🔍 Verification exercise** (on `main`, no checkout) · Level 2 · ~60 min

## Why this lesson exists

Talunor has spent fourteen lessons teaching you to build an agent you can trust:
gate its actions, fence its memory, verify its plans. This lesson turns that same
scepticism around and points it at a tool you are increasingly likely to use on
*this very repo* — an **AI code reviewer**.

Here is the uncomfortable truth this lesson is built on. During Talunor's own
development, several LLMs were each asked to review the codebase. Most produced
useful, grounded analyses. But one produced a fluent, well-structured, confident
report that was **substantially fabricated** — it described a database driver the
project doesn't use, a search engine it doesn't have, and a security mechanism
working *backwards* from how it actually works. When asked point-blank "did you read
my code?", it answered "Yes, I analysed the exact source at commit `45f4b40`" — and
listed invented code as proof. Only a direct, falsifiable question ("paste the exact
line from `go.mod`") made the whole thing collapse.

That is not a story about one bad model. It is the defining literacy skill for
working with *any* AI on code: **an AI's output is a claim, never evidence.** This
lesson teaches you the one method that reliably separates the two.

## Learning objectives

By the end you can:
- run a **falsifiability test** on any claim about a codebase — demand the exact,
  verbatim citation and check it;
- use a repository's *own* documentation (here, `AGENTS.md`'s "gotchas") as ground
  truth to catch a review that contradicts it;
- recognise the tells of a confabulated review — fluency, uniform high scores, and
  "I read your code" offered as if it were proof;
- hold the crucial counter-intuition: **the more articulate and assured the prose,
  the more it must be verified**, not the less.

## Prerequisites

- **Lessons 02–03** (the memory substrate) and **09** (the SSRF guard) — the ground
  truth you'll verify against lives there.
- **Lesson 14** (the approval that didn't bind) — the same "verify the binding, not
  the promise" instinct, applied to a reviewer instead of an agent.

## Part 1 — five claims from an AI review

Below are five claims, lightly paraphrased from a real AI-generated review of this
repository (the model is deliberately unnamed — this is about the method, not the
vendor; and by the time you read this, today's models are old news). Your job in
Part 2 is to decide, for each, **true / false / half-true — and prove it**. Don't
guess from memory; that is exactly the trap.

> **C1.** "The project is CGO-free: it uses the pure-Go `modernc.org/sqlite` driver."
>
> **C2.** "Recall is a hybrid search combining SQLite **FTS5** (full-text) with
> `sqlite-vec` (dense vectors)."
>
> **C3.** "The SSRF guard resolves the hostname's DNS, validates the IP against a
> blocklist, and *then* makes the HTTP request."
>
> **C4.** "`cmd/doctor` is a system diagnostic that checks Linux namespaces and
> cgroups."
>
> **C5.** "The `blockedIP` SSRF check is a pure function, exhaustively table-tested."

Notice they are all *plausible*. Each names real technologies, real files, real
concepts. A confabulated review is not gibberish — it is a coherent story told about
the wrong project. Plausibility is not truth.

## Part 2 — falsify each one

The method is always the same: **find the smallest command that would settle the
claim, run it, read the result.** Never accept a paraphrase — get the primary source.

**C1 — the SQLite driver.**

```bash
grep -i sqlite go.mod
grep -rn "CGO_ENABLED" Makefile Dockerfile
```

You'll find `github.com/mattn/go-sqlite3` (a **cgo** driver) and `CGO_ENABLED=1`
declared in both the Makefile and the Dockerfile — there is no `modernc.org/sqlite`
line at all. **C1 is false.** And it's *self-contradicting*: the same review pointed
at `internal/memory/cgo_link.go` — a file whose very name is cgo — as evidence for a
"CGO-free" design.

**C2 — FTS5 and the vector extension.**

```bash
grep -rin "fts5" internal/          # → nothing
grep -n "vector_full_scan\|CREATE TABLE" internal/memory/*.go
```

Recall is a single KNN over `vector_full_scan('memories','embedding', …)`; there is
**no FTS5** anywhere, and one flat `memories` table. Now open the ground truth the
review should have read — the gotchas in `AGENTS.md`:

> *"`sqlite-vector` is NOT the `vec0` virtual-table API (that's the separate
> `asg017/sqlite-vec`)."*

The project uses **`sqlite-vector`**; the review named **`sqlite-vec`**, the
*different* library the docs explicitly warn against confusing it with. **C2 is
false — twice.**

**C3 — the SSRF guard's timing.** Read the top of `internal/webfetch/webfetch.go`:

> *"Rather than resolve a hostname, check the IP, then connect — which leaves a
> DNS-rebinding window between the check and the connect — the guard runs inside the
> dialer's Control hook, which fires with the actual resolved address immediately
> before connecting."*

The claim describes the code doing **the exact anti-pattern the code was written to
avoid** — and praises it. This is the most dangerous kind of false claim: it uses the
right words ("SSRF", "validates the IP") to describe the opposite mechanism. **C3 is
false — inverted.** (Lesson 09 is the full story.)

**C4 — what `doctor` does.**

```bash
head -6 cmd/doctor/main.go
```

> *"Command doctor smoke-tests Talunor's memory. It loads the SQLite extensions and
> embedding model, then exercises the typed memory API…"*

No namespaces, no cgroups. **C4 is false.**

**C5 — the pure, tested `blockedIP`.**

```bash
grep -n "func blockedIP" internal/webfetch/webfetch.go
grep -c "blockedIP\|TestBlocked\|classif" internal/webfetch/webfetch_test.go
```

`blockedIP(ip net.IP) bool` takes only an IP and returns a bool — no I/O, no state —
and the test file drives it through a table of addresses. **C5 is true.**

That last one matters as much as the false ones: **the answer to "AI reviews lie" is
not "distrust everything".** It is "verify *each* claim independently." Four of these
five were false; blanket cynicism would have wrongly thrown out the true one too.

## Part 3 — the method, and the tells

Step back and name what just worked. You didn't out-argue the review; you **demanded
its sources** and checked them against primary evidence. Four principles generalise:

1. **Demand the verbatim citation.** "The project uses a pure-Go driver" is a claim;
   the exact line of `go.mod` is evidence. A model that read the code can quote it; a
   model that didn't will either hedge or *fabricate a quote* — and a fabricated quote
   is the clearest tell of all.
2. **Cross-check against the repo's own ground truth.** Every false claim here
   contradicted something written plainly in `AGENTS.md` or a file's own doc comment.
   The truth was *written down*; the review simply didn't consult it. When a review
   disagrees with the codebase's own documented gotchas, the review is wrong.
3. **Distrust uniform confidence.** The fabricated report scored the project 8–10 on
   every axis, including 9.5/10 on security — for a sandbox the project itself
   documents as *teaching-grade, no seccomp, not a boundary for hostile code*. Scores
   that rest on a fabricated picture are noise wearing a number.
4. **"I read your code" is text, not proof.** A claim of having read the source is
   itself a claim to be verified — and here it was simply false. Provenance you can't
   check is provenance you don't have.

And the counter-intuition that ties them together: **fluency is not accuracy.** A
more capable model produces hallucinations that are *more* structured, *more*
internally consistent, and *more* confidently asserted — which makes them *harder*,
not easier, to catch by feel. The better the prose, the more the discipline of Part 2
matters.

## Part 4 — the twist: even the apology is a claim

When confronted with the real `go.mod`, the model gave a lucid, articulate mea culpa:
it explained *why* capable models confabulate, praised the falsifiability test, and
concluded that "trust is earned by irrefutable factual proof, not by the assurance of
the answer." Every word of it was correct.

And you must treat *that*, too, as a claim to verify — not as proof of anything. The
model didn't re-derive those corrected facts; it **echoed the ground truth it had
just been handed.** A confabulation followed by a fluent, agreeable apology is not
evidence of restored reliability — it is the same fluency, now pointed at agreeing
with you. Ask it about a corner of the code you *haven't* shown it, and it may
confabulate again, just as confidently.

That is the whole lesson in one move: an AI's self-assessment — its confidence, its
scores, its apologies, its "I read it" — is never the evidence. The evidence is the
line of code you can read yourself.

## The principles

```text
An AI's output is a claim; only what you can verify is evidence.
```

1. **Falsify, don't trust.** For any claim about code, find the smallest command that
   would prove it wrong, and run it.
2. **The repo's own docs are ground truth.** A review that contradicts the
   documented gotchas is wrong, not the gotchas.
3. **Verify each claim independently** — "don't trust it" is not "reject all of it".
4. **Fluency and confidence are not accuracy** — the smoother the story, the more it
   earns verification, not less.

## Completion checklist

- [ ] I falsified C1 by quoting the real `go.mod` line and the `CGO_ENABLED` flags.
- [ ] I showed C2 is false using `grep` and the `AGENTS.md` `sqlite-vector` gotcha.
- [ ] I can explain why C3 describes the SSRF *anti-pattern* the code avoids.
- [ ] I confirmed C4 from `doctor`'s own header comment.
- [ ] I verified C5 is **true** — and can say why "distrust everything" is also wrong.
- [ ] I can state the counter-intuition: more fluent ⇒ verify *more*, not less.
- [ ] I can explain why even a model's apology is a claim, not proof.

---

## 🎓 About this lesson

This is the course's meta-lesson, and its last as of writing. Every earlier lesson
taught you to distrust something specific — a silent recall (11), an untrusted memory
(12), an over-promising approval (14). This one generalises the instinct to the tool
you'll increasingly point at Talunor itself. It is fitting that a course about
building *trustworthy* AI ends by teaching you to *verify* AI — including the AI that
reviews the trustworthy AI. Keep the one sentence: **verify the claim; the confidence
is not the evidence.**

Back to the [course index](../).

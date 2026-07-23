# Lesson 16 — Measure the model: building a reliability canary

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🔍 Exploration + hands-on** (reading and running `internal/calibration` on `main`) ·
Level 3 (advanced) · ~75 min

## Why this lesson exists

Lesson 15 taught you to verify *one* claim, *once*, by hand: demand the exact line,
check it against ground truth. But you cannot hand-check every model on every
update — providers ship new versions, cheaper "flash" variants, and quiet changes,
and a model you trusted last week can silently get worse today.

This lesson is the engineering counterpart: **automate the verification, and run it
continuously.** Layer 14 (`v0.14.0`) added a small harness — `internal/calibration`
+ `cmd/calibrate` — that runs a fixed suite of known-answer scenarios and scores how
reliably a model gets them right. It is a *reliability canary*, and building one is a
skill you will reuse far beyond Talunor.

The point isn't the CLI. It's the **three design decisions** that make an evaluation
harness trustworthy — the ones that recur in *every* serious LLM eval, and that most
people get wrong.

## Learning objectives

By the end you can:
- explain the load-bearing invariant — **the verifier must be deterministic, never an
  LLM** — and the recursive trap that follows from breaking it;
- separate the **two axes of reliability**, accuracy and consistency, and say why a
  pass-rate near 0.5 is the dangerous case, and where a standard deviation actually
  belongs;
- explain why a calibration's value is the **drift from a baseline**, not the
  absolute score, and state the honest threat model of a shared test suite;
- run the harness, author a scenario, and catch a regression against a baseline.

## Prerequisites

- **Lesson 15** (don't trust the review) — this automates the manual verification it
  taught, and shares its thesis: *an AI's output is a claim, never evidence.*
- **Lesson 11** (embedding provenance) — the same *canary* idea, applied to a model's
  truthfulness instead of an embedding space.
- A little Go, and (for the live parts) a running Ollama.

## Part 1 — the load-bearing invariant: no LLM judge

Read the matcher layer on `main`:

```text
internal/calibration/assert.go
```

An `Assert` is a set of **deterministic** checks on the model's reply: `equals`,
`contains`, `regex`, `number` (with tolerance), `json_valid`, `any_of`/`all_of`. Every
one is a pure function of the reply string. Read the type doc, and notice what is
*absent*: there is no "ask another model whether this answer is right" matcher — on
purpose.

> **The core idea.** A calibration harness measures whether a model is reliable. If
> the harness *judged* answers with a model, the measurement would inherit the exact
> unreliability it exists to catch — a confident-but-wrong judge scoring a
> confident-but-wrong answer. Ground truth must be **machine-checkable**, or it is not
> in a scenario. This is Lesson 15's principle, turned into an architectural rule.

This is the decision most eval frameworks get wrong: "LLM-as-judge" is convenient and
scales to open-ended answers, but it launders the problem rather than solving it. The
deterministic constraint is a real limit — it means scenarios must have
machine-checkable answers (arithmetic, format, exact facts, tool results) — and that
limit is the price of a trustworthy number. Now open:

```text
internal/calibration/scenario.go
```

A `Scenario` is 1–5 turns, each a user message plus its `Assert`. Note `Parse` is
**source-agnostic** — it takes bytes, not a path, so where the suite comes from
(plaintext, a private file, a decrypted blob) is not the parser's concern. And note
the scenarios carry **no session memory**: the runner builds each conversation from
the turns alone, so every model is tested clean-room, and the *same* suite runs
identically against any `llm.Provider`.

## Part 2 — two axes: accuracy and consistency

Read the runner and its metric:

```text
internal/calibration/runner.go
internal/calibration/metrics.go
```

`Run` replays each scenario **N times**. Why repeat, if the answer is fixed? Because
the *model* is not deterministic: at a non-zero temperature the same prompt yields
different replies. So a scenario has two independent failure modes, and you must
measure both:

- **Accuracy** — *does it get it right?* The mean over runs: the **pass-rate**.
- **Consistency** — *does it get it right reliably?* The spread over runs.

Here is the subtle part, and it's a common statistics mistake. For a **binary**
pass/fail outcome, the variance is fixed by the pass-rate (it's a Bernoulli:
variance = p(1−p)). So a separate "standard deviation of pass/fail" tells you nothing
new — the consistency signal *is* the pass-rate's distance from 0 or 1:

- `1.0` → reliably right;
- `0.0` → reliably wrong (bad, but at least predictable);
- **`~0.5` → flaky** — right half the time. This is the most dangerous result,
  because a single test run could show either face.

A standard deviation only earns its keep on a **continuous** metric — which is why
`metrics.go` computes `mean ± stddev` for **latency**, not for pass/fail. Reporting a
stddev of a binary outcome would be noise dressed as rigour; reporting the pass-rate's
distance from the extremes is the real consistency signal. Getting *which statistic
goes where* right is the difference between a harness that surfaces flakiness and one
that hides it.

## Part 3 — the value is the drift, not the score

Read:

```text
internal/calibration/baseline.go
```

`AsBaseline` snapshots a run's pass-rates; `Diff` compares a later run against it and
flags any scope (overall / category / scenario) that dropped past a threshold. That
comparison — not the absolute number — is the point.

> **Why drift, not absolute.** A public suite can be *memorised*: if the scenarios
> live in a public repo, a provider may have trained on them, so a high absolute
> score could mean "it learned the answers", not "it is capable". But the *same*
> suite re-run over time, against the *same* provider, turns "the model silently got
> worse" into a red build. It is the embedding canary of Lesson 11, pointed at
> truthfulness: you detect the *change* before your users do.

Now read the threat model the harness is honest about — the header of the seed suite
and the encryption doc:

```text
docs/calibration.seed.yaml          # the "Threat model (read this)" header
internal/calibration/crypt.go       # optional AES-256-GCM, and its honest limits
```

Two honest limits worth internalising, because over-trusting the number is its own
failure:

1. **Public suite ⇒ weak absolute, strong relative.** Ship a public example set (for
   teaching and drift), keep a *private* suite for a trustworthy absolute measure —
   gitignored, or encrypted (`CALIBRATION_KEY`) if you must version it in a shared repo.
2. **Encryption protects against scrapers, not the provider.** With a *hosted* model
   you hand it the decrypted prompts at inference time, so it can harvest them.
   Encryption stops passive repo-scraping; only a **local** model + a private suite is
   fully private. Naming what a control does *not* cover is as important as the control.

## Part 4 — run it, and catch a regression

First, the deterministic tests — no model needed:

```bash
go test ./internal/calibration/ -v
```

Now live (needs Ollama). Run the seed suite:

```bash
go run ./cmd/calibrate --suite docs/calibration.seed.yaml
```

You'll see a pass-rate per scenario and category, and `latency mean ± stddev`. Now
**author your own scenario** — a one-file suite testing something you care about:

```yaml
# my.yaml
suite: mine
scenarios:
  - id: strict-format
    category: format
    runs: 3
    turns:
      - user: "Reply with exactly the word OK, uppercase, nothing else."
        expect: { equals: "OK" }
```

```bash
go run ./cmd/calibrate --suite my.yaml
```

If the model adds punctuation or prose, the pass-rate drops below 1.0 — a real
instruction-following signal. Finally, feel the **drift** mechanism: save a baseline,
then compare a later run against it:

```bash
go run ./cmd/calibrate --suite my.yaml --save-baseline base.json
go run ./cmd/calibrate --suite my.yaml --baseline base.json   # exit 1 if it regressed
```

Point `--baseline` at a saved snapshot and the CLI exits non-zero on a regression —
the hook you'd put in front of a model upgrade in CI. Swap the model
(`TALUNOR_MODEL=…`) and re-run against the same baseline to watch a weaker model
trip it.

## The principles

```text
You cannot govern what you don't measure — and you can't measure an LLM with an LLM.
```

1. **The verifier must be deterministic.** The moment a model judges the output, the
   measurement inherits the failure it was built to catch.
2. **Accuracy and consistency are different axes.** Read the pass-rate's distance from
   0/1 for a binary check; reserve the standard deviation for a continuous metric.
3. **Measure the drift, not the absolute.** A pinned baseline turns silent
   degradation into a signal; the absolute score of a public suite is soft.
4. **Be honest about what the harness (and its encryption) does not cover.** A number
   over-trusted is worse than no number.

## Completion checklist

- [ ] I can explain why a calibration verifier must not be an LLM.
- [ ] I read `assert.go` and can name the deterministic matchers.
- [ ] I can say why a pass-rate near 0.5 is worse than 0.0, and where a stddev belongs.
- [ ] I read `baseline.go` and can explain drift vs absolute score.
- [ ] I ran the seed suite and authored my own scenario.
- [ ] I saved a baseline and saw a regression exit non-zero.
- [ ] I can state the threat model: public ⇒ relative; hosted ⇒ encryption ≠ private.

---

## 🎓 About this lesson

This closes the course's trust-and-verify arc. Lesson 11 caught a substrate
degrading silently (a canary for embeddings); Lesson 15 caught a *reviewer* lying
(manual falsification); Lesson 16 automates that verification into a *canary for the
model itself*. It is also the bridge to Iteration 4 (learning): before you let an
agent *learn* from a model — consolidating its outputs into long-term memory — you had
better be measuring whether that model is reliable, or you will bake its hallucinations
into the foundation. Measure first; learn second.

Back to the [course index](../).

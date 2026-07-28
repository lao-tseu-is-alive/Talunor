# Lesson 20 — Learn from action: most "tool knowledge" is model-*interpreted*

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🔍 Exploration + hands-on** (reading `internal/agent`, `internal/memory`,
`internal/tools`; the Layer 20 code shipped at `v0.19.0`, reference docs on `main`) ·
Level 3 (advanced) · ~65 min

## Why this lesson exists

For the whole of Iteration 4, the agent learned from exactly one thing: **what you
said**. Every fact reflection stored was tagged `user_stated`. But an agent that *acts*
also *observes* — it fetches a page, runs a command, gets a tool result back. Shouldn't
it learn from those too?

Yes — and the interesting part is *how honestly*. The tempting design is: "it came from a
tool, tools are reliable, mark it `tool_observed`." That single word quietly launders a
model's guess into high confidence. Layer 20 opens Iteration 5 ("truthful memory") by
learning from action **without** that dishonesty, and the discipline it takes is the whole
lesson: **provenance must come from the source, not from eagerness.**

## Learning objectives

By the end you can:
- explain why a fact the model distils from a tool's text output is `model_inferred`, not
  `tool_observed` — and what the narrow `tool_observed` case actually is;
- explain why keeping confidence *system-assigned* forces per-source extraction (and why
  the cheap "one call, let the model label each fact" design is the trap);
- read the multi-source `reflect` / `learnFrom` and name what decides each fact's provenance;
- explain the evidence trail (migration 4) and use `/why` to inspect it;
- state why a capability no shipped tool uses (`tools.Verified`) can still be worth building.

## Prerequisites

- **Lesson 16** (provenance & confidence) — where the `Provenance` tiers and
  system-assigned confidence come from. This lesson finally *populates* the unused tiers.
- **Lesson 17** (learning with humility) — the independence rule (`EvidenceCredibility`)
  is what keeps confidence honest here too.
- **Lesson 18** (off the critical path) — the reflection that Layer 20 widens is the async
  worker from that lesson.

## Part 1 — the tempting lie

Read where facts get their provenance today, on `main`:

```text
internal/memory/memory.go   (Provenance, BaseConfidence)
```

There are four tiers — `user_stated`, `tool_observed`, `model_inferred`, `unspecified` —
and confidence is assigned by the *system* from the tier (a verified tool > the user > the
model). Before Layer 20, only `user_stated` was ever produced by reflection; the others
were defined but dormant.

Now the temptation. The agent runs `web_fetch`, the page says "The population of France is
68 million," and the model distils "France's population is 68M." What provenance?

"It came from a tool → `tool_observed` → 0.95 confidence" **feels** right and is **wrong**.
The tool returned *text*. An LLM *read* that text and produced a fact. The reading is
interpretation, and interpretation is inference — the model could have misread, hallucinated
a number, or trusted a bad page. Tagging that `tool_observed` gives a model guess the
authority of a verified measurement. That is the sycophancy trap Lesson 16 warned about,
wearing a tool's uniform.

## Part 2 — the honest rule (ADR 0002)

Read the decision and the capability it hinges on:

```text
docs/decisions/0002-provenance-from-source.md
internal/tools/tool.go   (the Verified interface)
```

The rule Layer 20 adopts:

> A fact the LLM distils from a tool's **text** output is **`model_inferred`**. A fact is
> **`tool_observed`** only when it comes from a tool that declares its output a
> deterministic, structured fact it asserts directly — the optional `tools.Verified`
> capability (`Verified() bool`).

And the honest kicker: **no shipped tool implements `Verified`.** The calculator and clock
are deterministic but produce nothing durable ("2+2=4" is not a fact to remember);
`web_fetch` and `bash` return prose the model must interpret. So in practice Layer 20
populates `model_inferred`, and `tool_observed` is a **wired, tested seam** waiting for a
future tool that returns durable verified facts.

Is a capability nothing uses worth building? Here, yes — because it's a *tested seam that
resists a bad default*. Without it, the next person adding a tool that returns structured
data is tempted to reach for "`tool_observed`, it's from a tool." The capability makes the
honest path the obvious one: you get `tool_observed` only by *declaring* verification, on
purpose.

## Part 3 — why provenance must be assigned per source

Here is the design constraint that shapes the control flow, and it's worth slowing down for.

Confidence is **system-assigned** (Lesson 16): the model is never asked how sure it is,
because a model's self-reported confidence is not calibrated. Now extend that: the model
must not be asked to label its own *provenance* either. The moment you ask "which of these
facts came from the user vs. a tool vs. your own reasoning?", the model is self-reporting —
and it will happily call its own inference "observed."

So the **system** has to know each fact's source. And the only way for the system to know
is to keep the sources *separate*: extract from the user message, tag the results
`user_stated`; extract from a tool observation, tag those `model_inferred`; never mix them
into one call and ask the model to sort them out.

Read how `reflect` does exactly that:

```text
internal/agent/agent.go   (reflect, learnFrom, toolVerified, worthReflecting)
internal/agent/reflect.go (the source-neutral extractor prompt)
```

- `reflect(job)` loops over the turn's **sources**: the user message, each tool
  observation, and — only if `Config.ReflectAssistant` is on (it is off by default) — the
  assistant answer.
- For each source it calls `learnFrom(text, prov, turnID)` with the provenance the
  *system* chose for that source. The extractor itself is source-neutral ("find durable
  facts in this text"); it does not decide, and is never told to decide, provenance.
- `toolVerified(name)` is the only thing that can upgrade an observation to
  `tool_observed`, and it does so by type-asserting `tools.Verified` — a structural fact
  about the tool, not a model opinion.

The cheap alternative (one extraction call over all sources, the model labelling each fact)
would be fewer tokens and *wrong*: it hands provenance back to the model. The more verbose
per-source shape is what keeps Lesson 16's invariant intact. **The honesty rule dictated the
control flow.**

Note the two guards that keep the extra learning cheap: `worthReflecting` skips empty,
error, and *trivial-tool* observations (the calculator/clock/recall-memory outputs that
never carry a durable fact), and the observation text is size-capped before extraction.
It all rides the single `TALUNOR_REFLECT` toggle and the async worker from Lesson 18 — no
new knobs.

## Part 4 — the evidence trail, and "why do you believe this?"

A fact now carries a provenance *and* a confidence, but not *where it came from*. Layer 20
adds the audit trail:

```text
internal/memory/migrate.go   (migration 4)
internal/memory/evidence.go  (RecordEvidence, EvidenceFor, MemoryByID)
```

Migration 4 (append-only, no change to the `memories` table) adds an `evidence` table. Each
time `learnFrom` **stores** a new fact or **reinforces** an existing one, it appends a row:
which fact, which turn, from which source. So a fact restated across three turns has three
evidence rows — the record of *how belief accumulated*.

This is what makes the agent answerable. Read `WhyMemory` and try it (`/why <id>`): instead
of "I believe X (90%)," you get "I believe X because the user said so in turns #3 and #9."
It is also the raw material Layer 21 will arbitrate on: to decide whether a new fact should
*supersede* an old one, you need to know what supported the old one.

## Part 5 — watch it

The tests pin the two guarantees — honest provenance and the evidence trail:

```bash
go test ./internal/memory/ -run 'Evidence|MemoryByID' -v
go test ./internal/agent/ -run 'LearnsFromToolObservation' -v
```

Read `TestReflectLearnsFromToolObservation`: it runs a turn where the model calls a fake
tool, and asserts the fact distilled from the tool's output is `model_inferred` when the
tool is unverified and `tool_observed` when it declares `Verified()` — with an evidence
row from the right source, and `/why` surfacing it. That is Part 2 and Part 4, pinned.

Now live (needs Ollama). The always-on new behaviour is the evidence trail:

```bash
go run ./cmd/talunor --plain
```
```text
you> my name is Ada and I work mostly in Rust
you> /list
you> /why <the #id of the "User's name is Ada" fact>
```

`/why` shows a `user_stated` evidence row anchored to your turn. To see the tool path,
enable the network opt-in and ask for a fetch:

```bash
TALUNOR_WEBFETCH=1 TALUNOR_DEBUG=stderr go run ./cmd/talunor --plain
```

Watch the stderr trace: a fact distilled from the fetched page is stored as
`model_inferred` — never `tool_observed`, because `web_fetch` does not declare itself
verified. You have watched the agent learn from what it *did*, honestly.

## The principles

```text
Learn from what you observe — but let the SOURCE, not the model's eagerness, set how much you trust it.
```

1. **Interpretation is inference.** A fact the model reads out of a tool's text is
   `model_inferred`. `tool_observed` is the narrow case of a tool that *asserts* a verified
   fact — a seam, honestly labelled, not a default.
2. **System-assigned provenance forces per-source extraction.** If the model labels its own
   provenance, confidence is no longer system-assigned. Keep the sources separate so the
   *system* knows each fact's origin.
3. **Record the evidence, not just the belief.** An audit trail (which turns, which sources)
   makes the agent answerable — and is what a later correction step arbitrates on.
4. **A tested seam can be worth more than a used one.** `tools.Verified` fires for no
   builtin today; it exists so the honest path is the obvious one when a durable-fact tool
   arrives.

## Completion checklist

- [ ] I can explain why a fact distilled from a `web_fetch` result is `model_inferred`, not `tool_observed`.
- [ ] I can state what the narrow `tool_observed` case is, and why no builtin triggers it yet.
- [ ] I can explain why keeping confidence system-assigned means sources must be extracted separately.
- [ ] I read `reflect`/`learnFrom` and can name what sets each fact's provenance.
- [ ] I ran `/why <id>` and saw a fact's evidence trail (turns + sources).
- [ ] I can argue why `tools.Verified` is worth shipping even though nothing implements it.

---

## 🎓 About this lesson

This opens Iteration 5 — "truthful memory" — and it opens it with restraint. The whole
layer could have been "the agent learns everything it sees, and trusts it," which demos
well and rots quietly: a memory full of confidently-wrong facts scraped off pages. Instead
the layer learns from action while *under-claiming* — the default is the humble tier, and
the confident tier is a door you have to open on purpose.

That restraint is the through-line of the whole learning arc. Lesson 16 refused to let the
model rate its own confidence; Lesson 17 refused to let the model corroborate itself; this
lesson refuses to let a tool's *uniform* stand in for a tool's *verification*. Each time,
the honest choice was the more verbose one, and each time it was worth it. An agent you can
trust is not one that knows the most — it is one whose confidence you can *account for*. The
evidence trail is where that accounting finally becomes something you can read.

Next, Iteration 5 turns from *learning more truthfully* to *staying true*: **Lesson 21**
will let a new fact **supersede** an old one — because a memory that can accumulate and
forget, but never *correct*, doesn't really learn.

Back to the [course index](../).

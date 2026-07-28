# 2. Learned-fact provenance comes from the source, not the model's eagerness

- **Status:** Accepted (implemented in Layer 20, `v0.19.0`).
- **Date:** 2026-07-28
- **Relates to:** Layer 16 (provenance + confidence), Layer 17 (the independence rule).

## Context

Layer 20 lets the agent learn not only from the user's message but from what it
**observes** (tool results) and, optionally, what it **says** (the assistant answer).
Each learned fact must be tagged with a `Provenance` (Layer 16), and confidence is
assigned by the system from that provenance — the model is never asked how sure it
is. The question this ADR settles: **what provenance does a fact distilled from a
tool observation get?**

The tempting answer is `tool_observed` — it came from a tool, tools are trustworthy.
That is wrong, and it is the exact sycophancy trap the project warns about.

## Decision

1. **A fact distilled by the LLM from a tool's text output is `model_inferred`, not
   `tool_observed`.** The tool produced *text*; an LLM *interpreted* that text into a
   fact. The interpretation is inference, so the provenance is inference. Calling it
   "observed" would inflate confidence on a model guess.
2. **`tool_observed` is reserved for a narrow, honest case:** a tool that declares its
   output a deterministic, structured fact it asserts directly — via the optional
   `tools.Verified` capability (`Verified() bool`). Only facts from a verified tool are
   `tool_observed`.
3. **No shipped tool is verified today** (the calculator/clock are deterministic but
   produce nothing durable; web_fetch/bash are unverified prose). So in practice Layer 20
   populates `model_inferred`; `tool_observed` is a *wired, tested seam* for a future
   tool that returns durable verified facts. This is deliberate, not an oversight.
4. **The assistant's own answer is `model_inferred` and opt-in** (`Config.ReflectAssistant`,
   off by default, not env-wired). Learning from one's own output is the echo-chamber
   risk; `EvidenceCredibility(model_inferred) = 0` already stops it raising confidence,
   and defaulting it off keeps the honesty margin wide.
5. **Provenance is assigned per source by the system, so sources are extracted
   separately** — never in one combined call that asks the model to label each fact's
   provenance. The moment the model labels its own provenance, confidence is no longer
   system-assigned (Layer 16 is violated).

## Consequences

- **Honesty over eagerness.** The agent under-claims rather than over-claims: a fact it
  read off a web page is `model_inferred` (0.5 base confidence, and — via the Layer 17
  independence rule — a restatement by the *same* model raises salience but not
  confidence). This is the correct default for an autonomous agent.
- **A real but dormant `tool_observed` path.** The `Verified` capability and the
  provenance routing are implemented and tested (with an example verified tool), so a
  future durable-fact tool slots in without re-litigating provenance.
- **Cost.** Per-source extraction means up to ~2 LLM calls per turn (user + a
  worthwhile tool observation); mitigated by async reflection (Layer 18), skipping
  trivial/empty observations, size caps, and the single `TALUNOR_REFLECT` toggle.
- **A teaching point.** "Most 'tool knowledge' is model-interpreted; true observed
  evidence is narrow" is a non-obvious lesson that falls straight out of the design —
  the anchor of Lesson 20.

## Alternatives rejected

- **Tag tool-derived facts `tool_observed`** (generous): rejected — inflates confidence
  on model interpretation; the sycophancy trap.
- **One combined extraction call with the model labelling provenance** (cheap): rejected
  — breaks the Layer 16 invariant that provenance/confidence is system-assigned.
- **Reflect on the assistant answer by default** (more learning): rejected — echo-chamber
  risk for little gain; kept opt-in.

## References

- [`internal/tools/tool.go`](../../internal/tools/tool.go) — the `Verified` capability.
- [`internal/agent/reflect.go`](../../internal/agent/reflect.go) +
  [`agent.go`](../../internal/agent/agent.go) — `reflect`/`learnFrom` per source.
- [`internal/memory/evidence.go`](../../internal/memory/evidence.go) — the evidence trail.
- Lessons [16 (measure the model)](../lessons/16-measure-the-model/),
  [17 (learning with humility)](../lessons/17-learning-with-humility/).

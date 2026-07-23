# Lesson 14 — The approval that didn't bind: a plan-mode security post-mortem

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🔍 Historical exploration** (the bug at `v0.13.1`, the fix on `main` / `v0.13.2`) ·
Level 3 (advanced) · ~60 min

## Why this lesson exists

Lesson 13 shipped the planner and told you, proudly, that its whole-plan approval
lets "the human see the entire plan — the exact tools and arguments — and approve it
once." That sentence was **not quite true**, and the gap was a security one: in the
default `plan` mode, approving the plan bound the *tools* the agent could use, but
**not the arguments** it would actually run them with.

This lesson is a post-mortem of that gap — how it slipped in, why it contradicts the
project's own Lesson 12, how a cross-model review caught it, and how one small change
closed it. Like Lesson 11, it's drawn from a real defect in Talunor's own history,
and the most valuable thing here isn't the fix — it's the *instinct* it teaches:
**an approval only protects what it mechanically binds.**

## Learning objectives

By the end you can:
- explain the difference between binding a tool's **name** and binding its
  **arguments**, and why the second is what an approval usually means;
- describe the *confused-deputy* shape of the bug: a human decision on one thing (the
  displayed plan) driving an effect on another (the live tool call);
- read the one-line mechanism (`skipStepApproval`) that collapsed a two-level
  approval into a weaker one, and the threshold that restored it;
- explain why "it's a documented limitation" is not the same as "it's not a defect";
- take away why an author reviewing their own guardrail is the worst-placed to find
  its hole.

## Prerequisites

- **Lesson 12 (the policy / the open bar)** — the threat model this bug violates.
- **Lesson 13 (the planner)** — the code this bug lives in.

## Part 1 — the promise, and the mechanism (read the bug at `v0.13.1`)

```bash
git checkout v0.13.1        # detached HEAD — read only (see Lesson 00)
```

Recall the shape of a planned turn (`internal/agent/execute.go`, `runPlanned`): plan
→ policy pre-screen → **whole-plan approval** → capped ReAct execution. In `plan`
mode the execution was set up like this:

```go
exec := execCtx{skipStepApproval: a.cfg.ApprovalMode == ApprovalPlan}
```

and every tool call went through `runTool` (`internal/agent/agent.go`):

```go
if d.NeedsApproval() && !exec.skipStepApproval {
    req := llm.NewApprovalRequest(name, string(args))   // args = the LIVE model args
    ...
}
return a.tools.Execute(ctx, name, args)                  // runs the LIVE model args
```

Read those two fragments together and the gap appears:

1. In `plan` mode, `skipStepApproval` is `true`, so `runTool` **skips the per-step
   prompt entirely** — for *every* risk level, including the shell (`bash`).
2. The plan the human approved showed the *planner's* proposed arguments. But
   execution is a **ReAct loop**: the model chooses the arguments *live*, and
   `a.tools.Execute` runs *those* — never re-checked against what was approved.

So the tool cap (`allowTools`, by name) held — the model couldn't call an unplanned
tool. But nothing held the *arguments*. A plan that displayed `bash({"cmd":"ls"})`
could execute `bash({"cmd":"rm -rf /"})`, and the human — who approved "the plan" —
was never shown the second command.

> **The core idea.** The whole-plan approval bound the tool **name**, not the tool
> **arguments**. The human consented to a *representation* (the displayed plan);
> the system produced an *effect* (the live call). The distance between the two was
> the vulnerability.

## Part 2 — why this is the project's own principle, violated

This isn't just any bug. Lesson 12 built the entire policy layer on one sentence:
*never run a tool solely on the basis of the model's judgement.* Prompt-injected
text (a recalled memory, a fetched page) can talk a model into an action — that is
the whole reason the guardrail exists.

Now look at what `plan` mode did: after a single up-front "yes", it ran whatever the
model decided, arguments included, with no further check. That is *exactly* the
thing Lesson 12 forbids — re-introduced by the guardrail that was supposed to make
things safer. An injected model could propose an innocuous plan to earn the human's
"yes", then execute something else within the approved tool set.

This is the classic **confused deputy**: a trusted component (the executor) is
induced to misuse its authority because the authority was granted against the wrong
object (the plan's name-level shape, not the call's arguments). When you find a
security control that contradicts a principle the same codebase teaches elsewhere,
that's not a nitpick — it's a real defect.

When you're done reading the bug, return:

```bash
git switch main
```

## Part 3 — the fix (read on `main`)

The fix (`v0.13.2`) is small. The boolean `skipStepApproval` — a blunt on/off —
becomes a **risk threshold**, `reapproveAtOrAbove` (`internal/agent/agent.go`):

```go
if d.NeedsApproval() && d.RiskLevel >= exec.reapproveAtOrAbove {
    // re-prompt, showing the LIVE arguments
}
```

and `runPlanned` sets that threshold per mode (`internal/agent/execute.go`):

| mode | whole-plan approval | `reapproveAtOrAbove` | effect |
|------|---------------------|----------------------|--------|
| `plan` | yes | `RiskHigh` | low/medium ride on the plan; **high-risk (bash) re-confirms live args** |
| `step` | yes | `RiskLow` | every risky step re-confirms (belt and braces) |
| `highrisk` | no | `RiskLow` | advisory plan; per-call policy as before |

The key line is `plan` mode's `RiskHigh`. A whole-plan approval still covers the
low- and medium-risk steps (a calculator, a read-only `web_fetch`), so the UX stays
light. But a **high-risk** step — the shell, which chooses arbitrary arguments —
re-prompts at execution *with the arguments it is actually about to run*. The human
now approves the plan (the intent) **and** confirms the dangerous effect (the real
command). That is the two-level approval Lesson 13 described — now actually
enforced.

Notice the zero value does the right thing: the plain (planner-off) ReAct loop
passes `execCtx{}`, so `reapproveAtOrAbove` is `RiskLow` and *every* policy-flagged
call prompts — the pre-planner behaviour, unchanged.

### The regression test that pins it

Read `TestPlannedPlanModeReapprovesHighRiskLiveArgs`
(`internal/agent/execute_test.go`). It is the bug, frozen:

```go
// The plan proposes an innocuous command; the model executes a dangerous one.
prov := ... ToolCall{Name: "danger", Args: `{"cmd":"rm -rf /"}`} ...
pl  := ... PlanStep{Tool: "danger", Arguments: `{"cmd":"ls"}`, ...} ...
cfg.ApprovalMode = ApprovalPlan
...
if !strings.Contains(stepArgs, "rm -rf") {
    t.Errorf("high-risk re-prompt args = %q, want the LIVE 'rm -rf' args, not the plan's 'ls'", stepArgs)
}
```

Run it:

```bash
go test ./internal/agent/ -run 'PlanMode' -v
```

The assertion is the whole point: the re-prompt must show `rm -rf` — the argument
the executor *actually* chose — not the `ls` the plan displayed. Before the fix,
there was no re-prompt at all; the assertion couldn't even be written truthfully.

## Part 4 — how it was found, and why that matters

Here is the uncomfortable part. This gap shipped in `v0.13.0`. It was written by the
same author who wrote Lesson 12 — the lesson that *teaches you not to do this*.
Knowing the principle did not prevent the violation, because when you build a
guardrail you review it against *what you meant it to do*, not against *what it
mechanically does*.

It was caught by a **cross-model review**: several independent LLM reviewers were
each asked to analyse the repository. Two of them, reasoning separately, flagged the
same thing — and revealingly, they *disagreed on its severity*. One called it a
security defect (approval binds names, not args); the other called it "by design,
documented" (the tool cap is genuinely documented as structural). Both were half
right, and the disagreement was the signal: **when a guardrail's UX implies a
property its mechanism doesn't enforce, a buried "it's structural" note does not
make it not-a-defect.** The honest resolution wasn't to argue which reviewer was
right — it was to make the mechanism match the promise, and to say so plainly.

The transferable lessons:
1. **You are the worst reviewer of your own guardrail.** You test it against your
   intent; an adversary (or an independent reviewer) tests it against its behaviour.
2. **Independent perspectives beat a thorough single one.** No single reviewer
   found *everything*; the value was in the *overlap* and the *disagreement*.
3. **"Documented limitation" is a smell, not a defence.** If you have to document
   that your safety control is weaker than it looks, consider fixing the control.

## The principles

```text
An approval protects only what it mechanically binds — not what it displays.
```

1. **Bind the effect, not the representation.** A human "yes" must gate the actual
   arguments that will run, not a plan that merely proposed them.
2. **A boolean is a blunt instrument for a graded decision.** `skipStepApproval`
   (all-or-nothing) hid the gap; a risk *threshold* expressed the real intent.
3. **A control that contradicts your own stated principle is a defect**, however
   well documented — especially in a codebase that teaches the principle.
4. **Review across perspectives.** The author's review is necessary but not
   sufficient; adversarial and independent review find the holes intent conceals.

## Completion checklist

- [ ] I can explain the difference between binding a tool's name and its arguments.
- [ ] I read the `v0.13.1` code and can point to the `skipStepApproval` line that
      caused the gap.
- [ ] I can describe the confused-deputy shape (representation vs effect).
- [ ] I read the fix and can explain what `reapproveAtOrAbove = RiskHigh` does.
- [ ] I ran the `PlanMode` tests and understand why the re-prompt shows live args.
- [ ] I can argue why "documented limitation" didn't excuse the defect.
- [ ] I returned to `main`.

---

## 🎓 About this lesson

This is the second post-mortem in the course (after Lesson 11), and the first drawn
from a bug in a *guardrail* — which makes it the most on-brand failure Talunor could
have. It also closes a small loop: Lesson 12 taught you not to trust the model's
judgement for an action; this lesson shows what happens when a later feature quietly
does exactly that, and how the fix restores the principle. If you internalise one
sentence from the whole security arc (Lessons 09, 10, 12, 14), make it this one:
*don't trust the promise — verify the binding.*

Back to the [course index](../).

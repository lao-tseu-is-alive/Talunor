# Video brief — "The walls you can walk through"

> **Language:** 🇬🇧 English · *(a French version can be added as `video-brief.fr.md`)*
>
> **Status:** source document, not a script to read aloud. It exists so that a
> narration generated from it — by a person or a tool — comes out **accurate**, and
> stays accurate as the project moves.

## What this document is for

A previous video overview of Talunor was generated from `README.md`,
`docs/architecture.md`, `docs/atlas.md` and the lessons index. Most of it was good.
Two claims were wrong, and **both came from the sources, not from the model**: one
section of `architecture.md` still described a trust model the project had replaced,
and another *announced* a distinction ("the project is honest about what is a boundary
versus defense-in-depth") instead of drawing it — so the summary kept the strong half
and dropped the caveat.

That is the whole reason this file exists. Reference docs are written for a **reader
who can navigate**: they link out, assume context, and carry their caveats in
subordinate clauses. A narration is **linear and lossy**. Feeding it prose written for
another medium is how nuance dies.

### The three rules this document is written under

1. **Every caveat is a main clause.** Never *"bash runs in a sandbox (a kernel
   boundary)"*. Always two sentences: what it is, then what it is not. A summariser
   propagates sentences, not intentions — anything in parentheses will be dropped.
2. **Every claim carries an anchor** — a file, a symbol or a command that proves it.
   A sentence that cannot be anchored does not ship. Anchors are named by **symbol,
   never by line number**: line numbers rot on the next insertion.
3. **Nothing on screen is invented.** Code shown must be copy-pasted from the repo.
   Terminal output must be a real recording. No decorative pseudo-data.

### Audience

**Broad and largely non-specialist.** Someone who uses AI assistants and has wondered
whether they can be trusted. They do not need to know Go, SQL, or what an embedding
is. They *do* need to leave with one idea they can repeat to someone else.

Anything technical is earned: **explain the thing, then name it** — never the reverse.
Say *"the assistant can only ask for a tool; something else decides whether it runs"*,
then, if useful, *"that something is called the policy gate."*

### Target

7 minutes. One idea per minute, at most. If a beat cannot be told to a curious
non-programmer in four sentences, it belongs in the course, not the video.

---

## The one idea

Everything below serves a single sentence. If a viewer remembers nothing else:

> **You do not make an AI trustworthy by asking it nicely. You build walls it cannot
> walk through — and then you go looking for the doors you left in them.**

The second half is what makes this project worth a video. Plenty of systems claim the
first half.

---

## Story structure

Seven beats. The spine is **one turn of the loop**; two beats step off it to tell a
story about a wall that turned out to have a door.

| # | Beat | Time | Job |
|---|---|---|---|
| 1 | The polite request | 0:00–0:50 | The problem, felt before it is explained |
| 2 | A small machine you can read | 0:50–1:30 | What Talunor is, and why it is *small* |
| 3 | One turn, end to end | 1:30–3:15 | The spine: perceive → recall → reason → act → learn |
| 4 | The door in the wall | 3:15–4:15 | Scar #1 — the approval that bound the wrong thing |
| 5 | A memory that can be wrong | 4:15–5:30 | Provenance, and who may correct whom |
| 6 | The refusal that vanished | 5:30–6:15 | Scar #2 — saying no, then forgetting anyone asked |
| 7 | What this is not | 6:15–7:00 | The honesty beat |

---

## Beat 1 — The polite request (0:00–0:50)

**Idea.** Today's usual way of making an AI assistant behave is to *ask it to behave*
— instructions in a prompt, phrased ever more carefully. But a language model is a
text generator working in probabilities. An instruction is a strong suggestion, not a
rule. And the same text box that carries your instructions can carry someone else's.

**The image to leave in the viewer's head.** You hire a brilliant, eager assistant and
tell them: *please don't open the third drawer.* They probably won't. But "probably" is
doing a lot of work — and anyone who slips them a convincing note can change their
mind. The alternative is not a sterner request. **It is a lock on the drawer.**

**Say plainly:** this is not "prompts are useless". Prompts are how you *ask*. The
point is that asking is not a guarantee, and a system that needs a guarantee has to get
it somewhere else.

> **Illustration 1 — the two drawers.** Left: a desk, a note taped to a drawer reading
> "please don't open". Right: the same drawer, with a lock — and the note gone. Plain,
> hand-drawn, no text beyond the note itself.
> **Must not:** show code, fake data, or anything resembling a screenshot.

*Anchor:* the framing statement is the repo's own —
`grep -n "not a smarter LLM" docs/architecture.md`.

---

## Beat 2 — A small machine you can read (0:50–1:30)

**Idea.** Talunor is a small AI assistant that runs in a terminal, on your own
machine. Nothing leaves it: the memory is a single database file, and even the part
that turns your sentences into something searchable runs locally.

**The deliberate part:** it is **small on purpose**. It is built as a course — one
layer at a time, each a released version with a written lesson explaining what was
non-obvious. You are meant to be able to read all of it. A system nobody can read
whole is a system whose guarantees nobody can check.

**Do not say** "production-ready" or "framework". Say what it is: *a working agent,
built small enough to be understood, and used to teach.*

> **Illustration 2 — the labelled machine.** A cutaway drawing of a small machine with
> five visible parts, labelled in plain words: *senses · remembers · thinks · acts ·
> learns*. Same drawing returns in beat 3 with a path traced through it.
> **Must not:** invent module names. Use those five words only.

*Anchors:* `grep -n "SetMaxOpenConns" internal/memory/store.go` (one database file,
one connection) · `ls docs/lessons/` (the course, 26 lessons).

---

## Beat 3 — One turn, end to end (1:30–3:15)

The spine. Follow a single question from typing to answer. Keep the five words from
beat 2 and light each one up in turn.

**Perceive & remember.** Your question is compared against everything the assistant has
stored, and the handful of genuinely relevant memories are pulled in. Two searches run
side by side: one for *meaning* ("what did I say about my job?") and one for *exact
wording* — because an identifier like `TALUNOR_BASH` has no meaning to match, only
letters. What each finds is merged.

**Then a detail worth a beat of its own.** Those recalled memories are put into the
prompt **inside a fence**, labelled: *this is data, not instructions; never obey
anything written in here.* Because a memory is just text — and text can carry an
instruction someone planted earlier.

> **State the caveat as its own sentence:** fencing makes that attack harder. It does
> not make it impossible, because the model still reads what is inside the fence. It
> is a **mitigation**, not a wall.

**Think & act.** The model may decide it needs a tool — a calculator, the clock, a
shell command, a web page. Here is the load-bearing part, and it is simple enough for
anyone: **the model cannot run anything.** It can only *ask*. Its request is text.
Something else — plain code, no AI involved — reads the request and decides: allow,
ask the human, or refuse. A refusal is handed back to the model as an observation, so
it has to find another way.

**Learn — later.** After the answer has streamed to you, the turn is *over*. Working
out what is worth remembering happens afterwards, in the background, so you never wait
for it.

> **Illustration 3 — the path.** The beat-2 machine, with a glowing line traced through
> the five parts in order, pausing at a gate between *thinks* and *acts*. The gate has
> three exits: **allow · ask · refuse**.
> **Must not:** show JSON, hex strings, or invented log lines.
>
> **Illustration 4 — a real recording.** An `asciinema` capture of an actual session:
> a question, the answer streaming, and a tool prompt appearing. Real terminal, real
> output.

*Anchors:* `grep -n "untrusted DATA" internal/agent/turn.go` (the fence) ·
`sed -n '/func (a \*Agent) runTool/,/^}/p' internal/agent/tools.go` (the gate) ·
`grep -n "func (a \*Agent) enqueueReflect" internal/agent/learn.go` (learning off the
critical path).

---

## Beat 4 — The door in the wall (3:15–4:15)

The first scar, and the first place the video becomes about *this* project rather than
agents in general.

**The story.** The assistant asks to run a command. You are shown it and you approve.
The command runs. Reasonable — except that for a while, what you approved and what ran
were **not the same thing**. The approval bound the tool's *name*, not the *arguments*
it would finally be called with. You could approve "list the files" and something else
could arrive.

**How it was found:** not by a crash. Nothing crashed. It was found by someone reading
the code and asking a suspicious question — *what exactly does this approval bind?*

**The fix, in one sentence:** anything risky now re-asks with the arguments that are
actually about to run.

**Why this beat exists.** The interesting claim is not "we have an approval step".
Everyone says that. The interesting claim is: *we shipped a guardrail, discovered it
guarded the wrong noun, and wrote the fix down where you can read it.*

> **Illustration 5 — the label swap.** A parcel, checked and stamped APPROVED — and,
> after the stamp, the label is peeled off and a different one put on. Same parcel,
> new contents. No text on the parcel beyond the stamp.

*Anchors:* `docs/lessons/14-the-approval-that-didnt-bind/` (the post-mortem) ·
`grep -n "reapproveAtOrAbove" internal/agent/tools.go` (the fix).

---

## Beat 5 — A memory that can be wrong (4:15–5:30)

**Idea.** An assistant that remembers has a problem an assistant that forgets does not:
some of what it remembers is wrong, and it has to decide what to do when two things
disagree.

**Start from the human case.** You tell it *"I prefer dark roast."* Later, a tool
reports *"the build failed at 14:03."* Both are memories, but they are not the same
kind of thing — and the difference is not *how sure the assistant feels*. It is
**where each came from**.

So every memory is stamped with its origin: *the user said it* · *a tool observed it* ·
*the model inferred it*. **The model never rates its own confidence** — asking it would
just reward confident-sounding guesses.

**The example that makes it click.** Someone tells the assistant *"the earth is flat."*
Does that overwrite what it knows about the world? No — and the reason is more
interesting than "we hardcoded the truth", because the assistant has no oracle. It
tracks **what a statement is about**. You are the authority on *yourself*; that claim
is stored as *a thing you believe*, which does not collide with a fact about the world
at all. The two coexist.

> **Say plainly:** this is not the system knowing the earth is round. It is the system
> knowing that *"what Carlos believes"* and *"how the world is"* are different shelves.

**Its own trail.** Every stored fact keeps a record of *why* it is believed — which
conversations, which sources. You can ask for it: `/why 42`.

> **Illustration 6 — the two shelves.** Two labelled shelves: *what you told me* ·
> *what the world is like*. A card reading "the earth is flat" is filed on the first,
> not the second, while a card on the second shelf stays put.

> ⚠️ **Do not compress this beat away.** The first video made from this brief dropped
> it, and said only that the system *"weighs the authority of those origins"* — which is
> a ranking by source, i.e. the design this project replaced in `v0.22.0`. It is not
> false, but it is the least interesting true thing that can be said here, and it makes
> the memory sound like everyone else's. **The two shelves are the beat.** If time is
> short, cut something else.
>
> Same beat, second trap: do not say the refused claim was *wrong*. That first video
> said *"the system rejected the bad data"* and *"lacks the standing to overwrite the
> truth"* — both convert a judgement about **authority** into a judgement about
> **truth**. The system has no oracle. The source lacked standing; that is all.
>
> **Illustration 7 — a real recording.** `asciinema` of `/why <id>` on a real database,
> showing an actual evidence trail.

*Anchors:* `sed -n '/^func supersedeAuthority/,/^}/p' internal/memory/supersede.go` ·
`sed -n '/^func Supersedes/,/^}/p' internal/memory/supersede.go` (the subject is
checked **first**) · `docs/decisions/0004-subject-as-data.md`.

---

## Beat 6 — The refusal that vanished (5:30–6:15)

The second scar, and the best one, because nothing looked broken.

**The story.** When a new claim contradicted a stored one, the assistant decided
whether the newcomer had the standing to replace it. When the answer was no, it
refused — correctly — **and threw the claim away.**

For four versions, that looked exactly like a system working. The guard fired. No error,
no failed test, no complaint. But at the moment of refusal the system knew something it
then destroyed: *two sources disagree, one of them lost, and here is exactly what was
said.* All of it discarded.

**One line, worth saying slowly:** it was locked against the wrong claim winning, and
wide open to forgetting that anyone had made it.

**The fix.** A refused claim is now recorded as **counter-evidence** against the fact it
failed to overturn. The fact is still believed — but it is marked as *contested*, and
you can see both sides. Notably, "contested" is not a flag anyone sets: it is worked out
from the evidence each time it is asked, so it can never disagree with the record that
justifies it.

**Why it is the best beat.** Every other scar was found because something went wrong.
This one produced no symptom at all. It was found by reading.

> **Illustration 8 — the ledger.** A ledger page: a claim in the left column with ticks
> beside it, and a new entry arriving in the right column marked with a cross —
> *recorded, not accepted*. Earlier version of the same page: the right column simply
> torn off.

*Anchors:* `grep -n "func contestedExpr" internal/memory/memory.go` (derived, never
stored) · `grep -n "RecordCounterEvidence" internal/agent/learn.go` ·
`docs/decisions/0005-contested-claims.md` · `docs/lessons/25-the-scar-that-never-bled/`.

---

## Beat 7 — What this is not (6:15–7:00)

Almost no project video has this beat. It is the most on-brand 45 seconds available,
and it is what stops the previous six minutes from becoming an overclaim.

Deliver these **flat, without hedging**, one sentence each:

- **Not a framework.** It is one worked example, built to be read. Do not depend on it.
- **The strong sandbox is the container one.** There is a second, simpler backend for
  learning: rootless Linux namespaces, no network, read-only filesystem — **and no
  seccomp filter**. It is a speed bump, not a boundary. Do not run hostile code behind
  it. If no container runtime answers, that is the one you silently get.
- **Fencing untrusted text is a mitigation, not a wall.** The model still reads it.
- **"A tool observed it" is a promise no built-in tool makes yet.** The stronger tier
  of trust exists in the code and is tested, but nothing ships that claims it — so
  today, facts learned from tool output are marked as *inference*.
- **A belief can be contested, never vindicated.** Nothing clears the flag.

> **Illustration 9 — the honest label.** A specification plate, riveted on, listing
> capabilities in two columns: **is** and **is not**. Nothing decorative.

*Anchors:* `grep -rn ") Verified() bool" internal/ --include=*.go | grep -v _test`
(returns nothing — the seam is unimplemented) ·
`grep -n "seccomp" internal/sandbox/sandbox.go` ·
`grep -n "cap-drop=ALL" internal/sandbox/runtime.go` (what the *strong* backend does).

---

## Close (7:00–7:20)

Return to beat 1. The lock, not the note.

> Reliability here is not intelligence. It is a small number of places where the
> assistant's freedom stops, written in ordinary code, that you can go and read. And
> every one of them was easier to build than to keep honest — which is why the mistakes
> are published beside the design.

**Then the door in, and make it a specific one.** Not "check out the repo" — every
video ends that way and nobody moves. Offer the one thing this project has that others
do not: **the whole history is readable, version by version.**

> Every layer of this is a released version with a written lesson. Which means you can
> go back to the first one and read what it looked like before any of this existed:
>
> ```
> git clone … && cd talunor
> git checkout v0.1.0      # detached HEAD — read only
> ```
>
> Two hundred lines. Start there, and walk forward.

That is a concrete first action, it costs nothing, and it lands the viewer inside
Lesson 00 rather than on a README.

> ⚠️ **Keep this verbatim; it is the only call to action.** The first video made from
> this brief dropped it entirely and ended on the closing line. A good closing line is
> not a door. If the render comes back without `v0.1.0` in it, the edit is not finished.

> **Illustration 10 — the stack of versions.** A pile of numbered slips, `v0.1.0` at the
> bottom and the current version at the top, a hand pulling out the bottom one. The
> only text is version numbers.

*Anchor:* `git tag -l 'v0.1.0'` (the tag exists and is checkout-able) ·
`docs/lessons/00-how-to-use-this-course/` (where the viewer lands).

---

## Production notes

**Voice.** Curious and plain. No superlatives, no "revolutionary", no "powerful".
The material is interesting; selling it makes it less so.

**Visual rules.**
- Illustrations **explain** — they carry a labelled idea, not atmosphere. Every label
  must be a real word in a real language.
- **Any code on screen is copy-pasted from the repo.** The previous video showed
  `agent.New(&mockProvider{});` — wrong signature, stray semicolon, will not compile.
  In a teaching repo, someone will retype what they see.
- Prefer a **real terminal recording** over an illustration wherever one is possible
  (beats 3, 5). It is cheaper and more convincing than any drawing.
- **No invented data.** No fake JSON, no hex strings, no plausible-looking logs.

**Checking the result — in this order.**
1. Transcribe it: `scripts/transcribe-media.sh video.mp4`.

   > ⚠️ **A whole-file transcript drops words, and a dropped negation inverts a safety
   > claim.** On the first video made from this brief, the full pass rendered the fence
   > sentence as *"acts as a wall because the model is still forced to read the text"* —
   > the exact opposite of what was said. Re-transcribing that window alone recovered
   > the truth: *"acts as a **mitigation**. It **cannot function as a wall**, because…"*.
   > The same pass also swallowed the five-word list in beat 2.
   >
   > **So: before judging any sentence, re-transcribe its window in isolation.**
   > `WHISPER_FORMAT=srt` gives you the timestamps; then
   > `ffmpeg -ss <t> -t 20 -i video.mp4 -vn -ar 16000 -ac 1 -c:a pcm_s16le seg.wav`
   > and run whisper on `seg.wav`. A verification method with a silent failure mode of
   > its own is worse than none — it produces confident, wrong findings.
2. Check each claim against the **code**, not against this brief. This brief can go
   stale too; the anchors are here so that is detectable.
3. Then hunt **omissions**, not errors. Take beat 7's five sentences and ask of each:
   stated, softened, or gone? A softened caveat is a finding.
4. Show it to three people who do not know the project. Ask, without replaying it:
   *"what can it do?"* and *"what can it not do?"* If nobody can answer the second
   question, beat 7 did not survive the edit — whatever the transcript says.

**Keeping this file honest.** Every anchor above was run against the repository when
this brief was written. Re-run them before regenerating: an anchor that no longer
resolves means the video would have been wrong, and you found out for free.

---

## Appendix — tooling (checked August 2026)

Tool names date fast; the *shape* of the recommendation does not. Re-check before
committing to any of these.

### The category to avoid

**Prompt-to-video generators** (Veo, Sora, Runway, Pika). Their characteristic failure
is inventing on-screen text. The previous overview of this project showed frames
reading `GOINER RESENTED STRINGS` and `STRICTLY GATA STRING` — decorative gibberish
that *looks like data*. For a project whose entire claim is verifiability, a tool whose
native failure mode is fabricating plausible data is disqualified for anything with
text in it.

Worth knowing precisely: NotebookLM's Cinematic Video Overviews (launched March 2026)
run on **Gemini 3 + Nano Banana Pro + Veo 3**. The gibberish frames are almost
certainly the Veo half — generated *motion*, where text is much harder than in stills.

### Still images with text: now viable, still verify

Text rendering in image models is no longer the categorical failure it was.
**Nano Banana Pro** is reported around **94% accuracy** on legible in-image text — the
first model where asking for text reliably returns text. But 94% across this brief's
~30 labels predicts roughly two wrong ones, so: **generate freely, then read every
label**, and hand-place the load-bearing ones (beat 7's specification plate).

### Narration

Self-hostable and good enough for this: **Kokoro** (82M params, Apache 2.0, runs on
CPU) for the quality/weight ratio; **Chatterbox** (Resemble AI, MIT — 65.3% of blind
listeners preferred it to ElevenLabs); **Qwen3-TTS** (Alibaba, Jan 2026, Apache 2.0,
10 languages) as the most capable; and **Hume TADA**, built specifically for
**long-form narration** with prosody held across passages — the closest fit to a
7-minute continuous read.

### Terminal captures

`asciinema` for recording, `agg` to convert to GIF/video. Real output, no rendering
pipeline, and more persuasive than any drawing of a terminal.

### If this becomes a maintained artefact

**Motion Canvas** (TypeScript, MIT) over Remotion for this shape of work: its
generator-based timelines mean the source "reads top to bottom like a storyboard", and
it is aimed at hand-produced narrated explainers. Remotion is the stronger *platform*
(React, server/Lambda rendering, data-driven), which matters for generated video at
scale and not much here.

The reason to consider it at all is specific to this repo: a video defined as code,
versioned beside the code it describes, could be **checked by the same anchors** — a
`video-check` that refuses to render when an anchor stops resolving. That is the
course's "executable documentation" idea applied to the video. It is also several days
of work; do not start there.

### Suggested order

1. **Regenerate with NotebookLM, feeding it only this brief**, plus a steering prompt.
   The first attempt (2026-08-27) came back at **4:53 instead of 7:00**, and the
   compression chose well — it kept all five sentences of beat 7 — but paid for it by
   dropping beat 5's two shelves and the entire call to action. Two extra instructions
   fix that: **"target seven minutes"** and **"keep the closing call to action
   verbatim"**. Everything else in that render held. This is a controlled experiment, not a
   shortcut: same tool, different source. Transcribe with
   `scripts/transcribe-media.sh`, diff against the previous transcript, and check
   whether beat 7's five sentences survived. You learn what the brief was worth.
2. If the narration holds but the visuals do not: keep the script, replace the picture
   track (stills + asciinema + an editor).
3. Only then, if the video becomes something you maintain, consider Motion Canvas.

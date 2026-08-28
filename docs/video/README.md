# Video — publishing assets

Everything needed to publish a video about Talunor, **except the video file itself.**

## Why this directory exists

Talunor has been the subject of three generated artefacts — two video overviews and a
"scientific foundations" dashboard — and auditing them taught the project something it
now takes seriously: **a generated summary propagates what you wrote, not what you
meant.**

The first video overview was built from `README.md`, `docs/architecture.md`,
`docs/atlas.md` and the lessons index. Most of it was accurate. Two claims were not,
and **both traced back to the sources rather than to the model**: `architecture.md`
still described a trust model [ADR 0004](../decisions/0004-subject-as-data.md) had
replaced, and another section *announced* a distinction ("the project is honest about
what is a boundary versus defense-in-depth") instead of drawing it — so the summary
kept the strong half and dropped the caveat. That episode produced `v0.23.2` (the doc
fixes) and eventually [`make docs-assert`](../assertions.sh), the drift alarm the
reference docs had never had.

The second video was generated from [`../video-brief.md`](../video-brief.md), a source
document written *for the ear*. It came back materially better — all five limitation
sentences survived — and that brief is now the canonical source for any future
version. Read it before regenerating anything.

## What is here

| File | Tracked | What it is |
|---|---|---|
| `talunor-anatomy-of-a-loop.en.srt` | ✅ | Subtitles, **corrected against the audio** (see below) |
| `talunor-anatomy-of-a-loop.description.txt` | ✅ | YouTube description + chapters, carrying the call to action the edit dropped |
| `thumbnail-anatomy-of-a-loop.jpg` | ✅ | 1280×720 thumbnail, a real frame with a three-line hook |
| `thumbnail-anatomy-of-a-loop.plain.jpg` | ✅ | Same frame, no text — if you prefer to caption it elsewhere |
| `*.mp4` | ❌ | The videos themselves — **git-ignored on purpose** |

**Why the `.mp4` files are not committed.** The current one is 43 MB; the whole
repository history is 26 MB. Committing it would nearly triple every clone, forever,
for something that is not source — in a project whose pitch is *"clone it and read the
history version by version"*. They live here untracked so everything about the video
is in one place; publish them to a platform and link to that.

## The subtitles were corrected, and that matters

The `.srt` is **not** the raw output of a transcription pass. Four corrections were
made after re-cutting the audio and transcribing each window on its own:

| Whisper produced | The narrator actually says |
|---|---|
| *…acts as a wall because the model is still forced to read the text* | *…acts as a **mitigation**. It **cannot function as a wall**, because…* |
| *(nothing)* | *Senses, remembers, thinks, acts, and learns.* |
| *For example, the policy gate between…* | *This gate between…* |
| *Talenor* | *Talunor* |

The first is the one to remember: **a whole-file transcript can drop a negation, and a
dropped negation inverts a safety claim.** Shipping the raw pass would have published
the opposite of what the video says, in writing, indexable by search engines.

So: before trusting any sentence in a transcript, re-cut its window and run it alone.

```sh
WHISPER_FORMAT=srt ../../scripts/transcribe-media.sh video.mp4   # get timestamps
ffmpeg -ss 74 -t 20 -i video.mp4 -vn -ar 16000 -ac 1 -c:a pcm_s16le seg.wav
../../scripts/transcribe-media.sh seg.wav
```

## Publishing steps

1. **Upload as *Unlisted* first.** Everything below is easier to check on a real
   player than to imagine.
2. **Description** — paste `talunor-anatomy-of-a-loop.description.txt` whole. It is
   3,160 characters (the limit is 5,000). Two things are deliberate:
   - the first ~150 characters are what shows above *"Show more"*, so the thesis is
     there and the `git checkout v0.1.0` invitation is immediately under it;
   - the nine chapters come from **real timestamps** read out of the `.srt`, not
     estimated. YouTube requires the first to be `0:00` and at least three in total.
3. **Subtitles** — *Subtitles → Add → Upload file → With timing*, then
   `talunor-anatomy-of-a-loop.en.srt`. Do not rely on auto-captions: they will not get
   `Talunor`, `SQLite`, `provenance` or `supersede` right, and the transcript feeds
   search indexing.
4. **Thumbnail** — `thumbnail-anatomy-of-a-loop.jpg`. Already 1280×720 and ~120 KB
   (the limit is 2 MB).
5. **Pinned comment** — the second call-to-action slot, and worth using: link
   [Lesson 14](../lessons/14-the-approval-that-didnt-bind/) and
   [Lesson 25](../lessons/25-the-scar-that-never-bled/), the two scars the video tells.
6. **Check, then make it public.** Watch two minutes with subtitles on, confirm the
   chapters land on the right beats, then switch visibility.
7. **Link it from the README** — in `## Philosophy — and why it's different`, **not**
   in `## Demo`. The GIF there answers *"what does it look like"*; the video answers
   *"why does this exist"*. A README visitor wants to see the tool run in five seconds;
   whoever wants the reasoning scrolls.

## Recommendations for the next version

The current cut is good but **known-incomplete**, and the gaps are documented rather
than glossed:

- It runs **4:53 instead of the 7:00** the brief targets. Length steering did not take.
- It drops **beat 5's "two shelves"** — that a claim about *the user* and a claim about
  *the world* are different shelves, so "the earth is flat" is filed as a belief and
  never collides with a world fact. That is the most distinctive idea in the memory
  design, and its absence leaves the trust model sounding like an ordinary ranking.
- It drops the **call to action** entirely. The description compensates; the video
  itself still ends without a door.

If a regeneration is possible, feed it [`../video-brief.md`](../video-brief.md) alone,
and add two steering instructions: **"keep the closing call to action verbatim"** and
**"do not compress the two-shelves beat"**. Everything else in the render held.

## Verifying a new render

The method is in the brief and in
[`scripts/transcribe-media.sh`](../../scripts/transcribe-media.sh); the short version:

1. Transcribe it — and re-cut any window before judging the sentence in it.
2. Check each claim against the **code**, not against the documents the render was
   built from. Fidelity to the sources only proves the tool ran.
3. Hunt **omissions**, not errors. Take the five "what this is not" sentences and ask
   of each: stated, softened, or gone? A softened caveat is a finding.
4. Show it to three people who do not know the project. Ask, without replaying it:
   *"what can it do?"* and *"what can it **not** do?"* If nobody can answer the second,
   the caveats did not survive the edit — whatever the transcript says.

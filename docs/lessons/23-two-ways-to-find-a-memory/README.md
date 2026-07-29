# Lesson 23 — Two ways to find a memory: when meaning is the wrong index

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🔍 Historical exploration** (Layer 22, at `v0.21.0`) · Level 3 (advanced) · ~75 min

## Why this lesson exists

Lesson 03 taught you the trick that makes Talunor's memory feel intelligent: store a
sentence as a *vector*, and you can retrieve it by **meaning** — "Which technology keeps
a whole database in one file?" finds "SQLite stores an entire relational database in a
single file" without sharing a single word.

Now tell the agent your contract reference is `AFF-2024-113`, and ask for it back.

It fails. Not because the memory is missing, but because **an identifier has no
meaning**. `AFF-2024-113` and `AFF-2024-114` land at almost exactly the same point in
embedding space — the model has no idea one of them is yours — and neither sits near the
sentence you typed to look it up. The very property that made recall feel smart is the
property that loses the value you most needed stored verbatim.

Layer 22's answer is not a better embedding model. It is a second index with the
opposite bias, and an honest way to combine them.

## Learning objectives

By the end you can:
- explain what an embedding cannot represent, and why "just use a bigger model" does not
  fix it;
- describe BM25 in one sentence, and say why an inverted index has exactly the strengths
  a vector index lacks;
- explain why two ranked lists with **incomparable scores** must be fused by *rank*, not
  by score — and name the trap that creates when only one list exists;
- draw the line between *retrieval* ("what might help me answer this?") and *identity*
  ("is this the same fact?"), and say why only one of them may use word matching;
- recognise a capability that depends on how a binary was **compiled**, and treat build
  tags as part of a feature's contract.

## Prerequisites

- **Lesson 03 (semantic recall)** — embeddings, cosine distance, the KNN threshold.
- **Lesson 18 (salience)** — the `similarity × confidence × salience` score this layer
  has to preserve.
- Helpful: **Lesson 22**, whose capability contract this layer is the first real user of.

## Part 1 — feel the gap first

```bash
git checkout v0.21.0        # detached HEAD — read only (see Lesson 00)
make deps                   # if you have not already
```

Before reading any code, watch the failure that motivates the layer. Turn the lexical arm
off, so the store behaves exactly as it did at Layer 17:

```bash
TALUNOR_RECALL=vector make run
```

```
you> My contract reference for the school renovation is AFF-2024-113.
you> /quit
```

Restart, and ask:

```bash
TALUNOR_RECALL=vector make run
```

```
you> What is AFF-2024-113?
```

Then run the same two steps **without** `TALUNOR_RECALL=vector` and compare. The
difference is not subtle, and `/debug` shows you exactly why:

```
recall: q="What is AFF-2024-113?" k=4 max≤0.75 → 1 hit(s)
    #7 v#1 d=0.5340 l#1 score=0.030 fact "The contract reference … is AFF-2024-113."
```

`v#1 … l#1` means both arms found it. Try a query where only one does, and the notation
starts earning its keep.

> **The core idea.** An embedding is a *lossy summary of meaning*. That is a feature when
> you search by idea and a defect when the thing you stored has no idea in it — an
> identifier, a serial number, a version, an error code, a rare proper noun. Those are
> exactly the facts a personal assistant must return **verbatim or not at all**.

## Part 2 — the opposite bias

Read `internal/memory/lexical.go`. The lexical arm is an SQLite **FTS5** index:

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    content,
    content='memories',        -- external content: index only, no copy of the text
    content_rowid='id',
    tokenize="unicode61 remove_diacritics 2"
);
```

Three decisions in four lines:

- **`content='memories'`** — an *external-content* index stores the inverted index only
  and reads the text from `memories`. No duplicated content means no possibility of the
  copy drifting from the original.
- **`remove_diacritics 2`** folds `Genève` and `Geneve` together.
- **No stemmer.** The obvious `porter` tokenizer would improve English recall and mangle
  French, and this memory holds both. A tokenizer is a language decision; making it
  silently English-only would be a bug you only notice in the other language.

Ranking is **BM25**, in one sentence: *a document scores higher when it contains more of
your query's terms, and much higher when those terms are rare in the corpus.* That
second half — inverse document frequency — is the whole reason this arm is worth having.
`AFF-2024-113` appears in exactly one memory, so it dominates any score it touches.

### The query is not a string

You cannot hand user text to `MATCH`. FTS5 has its own query language — `AND`, `OR`,
`NOT`, `NEAR`, `*`, `^`, `"…"` — so `what's my name?` is a **syntax error**, and
`cat AND dog` silently means something the user never asked. `matchExpression` tokenises,
quotes each term (which makes every operator inert), and joins with `OR`:

```go
matchExpression("cat AND dog NOT bird")   // → `"cat" OR "dog" OR "bird"`
```

`OR` rather than `AND` is deliberate: with `AND`, one absent word rejects the document;
with `OR`, BM25's IDF does the discriminating on its own.

## Part 3 — the bug that proves why stopwords are a correctness fix

This is the part the tests could not have told me. The first end-to-end run of hybrid
recall, against five ordinary memories, produced this:

```
q="what language does he like?" → 1 hit
   [l#1 score=0.0148] The user's name is Cedric and he works in Lausanne.
```

Wrong answer, confidently ranked, from the lexical arm alone. It matched on the
pronoun **`he`**.

Sit with why that is worse than a miss:

1. BM25 *down-weights* common terms; it does not **refuse** them. With `OR`, one junk
   term is enough to admit a document.
2. The vector arm has a relevance gate — `maxDistance` — so an unrelated memory is
   dropped outright. The lexical arm has **no equivalent**: matching is binary, and
   ranking only orders whatever matched.
3. So the arm with no gate produced a confident top hit for a question the arm with a
   gate had honestly answered with *nothing*.

The fix is a stopword list (English **and** French), plus one rule that keeps it from
eating the very tokens the layer exists for:

```go
func keepTerm(t string) bool {
	if hasDigit(t) {          // "16" in "PostgreSQL 16.2" is the discriminating part
		return true
	}
	return len([]rune(t)) >= minTermLen && !stopwords[t]
}
```

Read `TestMatchExpressionDropsFunctionWords` — it is that live failure, frozen. And note
the detail in the stopword list worth stealing: it contains **both** `etait` and `était`,
because FTS5's tokenizer folds diacritics but this Go filter sees the raw query. Two
components, two views of the same string.

> A lexical arm that fires on function words does not add recall. It adds lies.

## Part 4 — fusing two lists that share no scale

Now read `internal/memory/hybrid.go`. The vector arm ranks by cosine distance (bounded,
`0` = identical); the lexical arm by BM25 (negative, unbounded, corpus-relative). Ask
yourself what `0.7 × (1 - 0.53)` and `-1.06e-06` should be worth relative to each other.

There is no answer. Any weighted blend is a constant someone tunes forever, and it
re-tunes itself every time the corpus grows. **Reciprocal Rank Fusion** dodges the
question by throwing the scores away and keeping only each arm's *order*:

```go
rrf(memory) = Σ over arms  1 / (rrfK + rank_in_that_arm)      // rrfK = 60
```

Two properties fall out for free:

- **Corroboration wins.** A memory both arms ranked accumulates from both, without anyone
  choosing a weight.
- **The head is flattened.** `rrfK = 60` means rank 1 is worth only slightly more than
  rank 2 — which is what you want when one arm is occasionally confidently wrong (Part 3
  shows it can be).

Confidence and salience are untouched: `score = rrf × confidence × effective-salience`.
Relevance changed shape; *trust* and *mattering* keep the meaning Lessons 17 and 18 gave
them.

### The trap: RRF is not the identity function

Here is the subtlety that is easy to ship a bug through. On a build without FTS5 there is
only one list — so fusion should be a no-op, right?

It is not. Ranking by `1/(60+rank) × conf × sal` is **not** the same order as
`(1-distance) × conf × sal`, because the two relevance terms fall off differently. Work
it yourself:

| hit | distance | confidence | `(1-d)·conf` | rank | `1/(60+rank)·conf` |
|-----|----------|-----------|--------------|------|--------------------|
| A   | 0.10     | 0.5       | **0.45**     | 1    | 0.0082             |
| B   | 0.70     | 1.0       | 0.30         | 2    | **0.0161**         |

Classic scoring puts **A** first; RRF puts **B** first. Same data, same code path,
different answer — and every user who never asked for hybrid recall would have silently
got the second one. So `fuse` special-cases the single-arm case and keeps the Layer-17
formula verbatim, which `TestFuseWithOneArmKeepsLayer17Ranking` pins.

**The general lesson:** when you generalise a mechanism, check that the new
implementation reduces *exactly* to the old one on the old inputs. "It's a special case
of the new thing" is a claim to verify, not to assume.

## Part 5 — retrieval is hybrid; identity is metric

The boundary that cost a regression, and the most transferable idea here.

`Recall` and `RecallForConsolidation` look like the same function with a flag. They ask
fundamentally different questions:

| | question | right tool |
|---|---|---|
| `Recall` | *what might help me answer this?* | **hybrid** — an extra candidate is cheap, a missed identifier is not |
| `RecallForConsolidation` | *do I already hold this fact?* | **vector only** — this is a question about distance |

Wiring the lexical arm into both broke reflection (Lesson 20's machinery). Two sentences
can share the rare token `NX-9000` and state entirely different things:

```
"The Lausanne office network switch is model NX-9000."
"The NX-9000 firmware upgrade is scheduled for the winter break."
```

BM25 ranks them as near-identical; cosine distance knows they are not. When word overlap
was allowed to nominate consolidation candidates, `learnOneFact` started merging new
facts onto merely word-similar ones — and *stopped storing them at all*. The caller's
`maxDistance` is a **cosine radius**, and a lexical hit has no coordinate in it.

Read `TestConsolidationLookupIgnoresLexicalOverlap`. Then note how the bug was found: not
by a new test, but by an **existing Layer-20 test** that had nothing to do with this
layer. Old tests earn their keep in exactly this moment.

## Part 6 — a capability that lives in the build

Everything above depends on a fact invisible in the source and invisible in `go.mod`:

```bash
go test ./internal/memory/ -run TestHybridRecall -v      # no tag
# --- SKIP: fts5 capability unavailable: built without -tags sqlite_fts5
```

`mattn/go-sqlite3` compiles SQLite itself, and **FTS5 only under `-tags sqlite_fts5`**.
The default build has FTS3/4, no `fts5` module, no `bm25()`. So this is Talunor's first
capability that depends on *how the binary was compiled* rather than on what the machine
has installed — and a silent one, because a tagless build still runs perfectly, just
retrieving less.

Three consequences, all visible in the repo:

1. The tag rides on **every** supported build: `GOTAGS` in the Makefile, the Dockerfile,
   `release.yml`. Miss one and the *shipped* binary quietly loses the feature.
2. Degradation is **reported, not silent**: `Store.Lexical()` → `unavailable`, printed by
   `make doctor` and `/mem`.
3. The FTS5 index is **not a migration**. Every byte is rebuildable from `memories`, and a
   tagless build *cannot create it at all* — so putting it in the ordered migration list
   would make `schema_version` claim something the database may be unable to honour. It
   is created idempotently at `Open`, like `vector_init`. **Migrations are for source
   data; derived indexes are rebuilt.**

And this is where Lesson 22's contract pays off one release later:

```bash
TALUNOR_REQUIRE=fts5 go test ./internal/memory/ -run TestHybridRecall
# --- FAIL: TALUNOR_REQUIRE=fts5 declares this host must be able to exercise "fts5",
#     but it cannot: lexical arm is unavailable (built without -tags sqlite_fts5)
```

CI declares `TALUNOR_REQUIRE=ext,fts5`, so a build that lost the tag turns those tests
red instead of skipping them behind a green `ok`.

## Hands-on — break each half and watch what dies

```bash
# 1. Store one meaningful sentence and one identifier, then query both ways.
make run
#   you> The production database runs PostgreSQL 16.2 on the blue cluster.
#   you> /debug
#   you> PostgreSQL 16.2          → note the arms in the trace
#   you> which database do we use? → note the arms again
#   you> /quit

# 2. Kill the lexical arm; repeat. The identifier query degrades, the semantic one does not.
TALUNOR_RECALL=vector make run

# 3. Kill the SEMANTIC arm instead — edit recall() to skip vectorCandidates, then:
go test -tags sqlite_fts5 ./internal/memory/ -run 'TestHybridKeepsSemanticRecall'
#    It fails: "Which technology keeps a whole database in one file?" shares no word
#    with the SQLite memory. Each arm is load-bearing for a different question.

# 4. Remove one stopword — delete "he" from the map — and re-run:
go test -tags sqlite_fts5 ./internal/memory/ -run 'TestMatchExpressionDropsFunctionWords'
#    One deleted map entry is the difference between "no answer" and a confident
#    wrong one.

# 5. Restore everything:
git checkout internal/memory/
```

## The principles

```text
Two indexes with opposite biases beat one index with a better model.
```

1. **Know what your index cannot represent.** Embeddings lose the literal; that is not a
   quality problem to solve with a bigger model, it is a *kind* problem to solve with a
   second index.
2. **Fuse ranks, not scores,** when the scores share no scale — and then check that
   fusion reduces exactly to the old behaviour when there is only one list.
3. **A matcher with no relevance gate needs one built by hand.** BM25 ranks whatever
   matched; deciding what may match at all is your job (stopwords, term rules).
4. **Retrieval and identity are different questions.** Word overlap may nominate
   candidates for "what might help?"; only distance may answer "is this the same?".
5. **Derived indexes are rebuilt, not migrated.** Schema version should never promise
   something a given build cannot create.
6. **Build tags are part of the contract.** A capability that depends on compilation
   flags must be detected at runtime, reported when missing, and declared by anything
   that claims to test it.

## Completion checklist

- [ ] I reproduced the identifier failure with `TALUNOR_RECALL=vector` and the fix without it.
- [ ] I can explain BM25's IDF in one sentence and why `OR` beats `AND` here.
- [ ] I can say why the pronoun "he" produced a confident wrong hit, and what a stopword
      list is fixing (a *correctness* problem, not performance).
- [ ] I worked the RRF table by hand and can explain why the single-arm case is special-cased.
- [ ] I can state the retrieval-vs-identity boundary and which recall path is vector-only.
- [ ] I ran the suite without `-tags sqlite_fts5` and saw the tests skip, then with
      `TALUNOR_REQUIRE=fts5` and saw them fail.
- [ ] I did at least experiments 3 and 4 above, and returned to `main`.

---

## 🎓 About this lesson

This closes Iteration 5's arc — a memory that learns from action (20), corrects itself
(21), and now *finds* what it holds (22) — and it is the course's clearest example of a
recurring theme: **the honest fix for a limitation is usually a second mechanism with
opposite failure modes, not a better version of the first.** You met the shape before, in
Lesson 12 (a policy beside the model's judgement) and Lesson 16 (a deterministic verifier
beside an LLM's answer). Vector plus lexical is the same instinct applied to retrieval.

The other thing worth carrying: two of this layer's three hardest bugs were found by
*running it*, not by testing it. The pronoun match and the consolidation regression were
both invisible in code review and obvious in five minutes of real use.

Back to the [course index](../).

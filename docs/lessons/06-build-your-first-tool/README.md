# Lesson 06 — Build your first tool

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🛠️ Current contribution** · Level 2 · ~2h

> This is a **contribution** lesson: you change the *current* project. Work on a
> branch of **your own fork** — not on a detached tag, and not on this repo's `main`.

## Why this lesson exists

You've read how Talunor works; now you'll *add* to it. The best first change is a
new **tool** — a capability the agent can call — because you can add one **without
touching the agent core**. That's the whole point of the tool interface, and doing
it once teaches you how well-designed extension points feel.

You will also meet, in three lines of Go, the distinction the rest of this course is
about: **what your software guarantees** versus **what you merely ask the model to
do**.

## Learning objectives

By the end you can:
- implement Talunor's `tools.Tool` interface from scratch;
- explain why a JSON Schema constrains the *model* and validates *nothing* in Go;
- register a tool so the agent can call it — and say what registering does and does
  not guarantee;
- write table-driven tests for it;
- explain why adding a capability by *extension* beats editing the orchestrator;
- open a pull request that a maintainer can review without asking you a single
  question.

## Prerequisites

- Lessons 00–05. You've seen the agent loop call tools (Lesson 05).
- `gh` (GitHub CLI) if you want to open the PR from the terminal.

## Fork first, then branch

This lesson ends with a real pull request, so start from a fork:

```bash
gh repo fork lao-tseu-is-alive/Talunor --clone --remote   # first time
# or, from a clone you already have:
git remote add fork git@github.com:<your-user>/Talunor.git

git switch main
git pull
git switch -c learning/unit-convert-tool
```

Why a fork: the PR you open at the end targets **your** repository. Section
[Proposing your patch](#proposing-your-patch) explains why — read it before you push.

## Read your template

```text
internal/tools/tool.go       # the Tool interface + the Registry
internal/tools/builtin.go    # Calculator and Clock — copy their shape
internal/tools/tools_test.go # how the builtins are tested
```

The whole contract is four methods:

```go
type Tool interface {
    Name() string                 // stable id the model calls, snake_case
    Description() string           // what it does / when to use it (the model reads this)
    Schema() json.RawMessage       // JSON Schema for the arguments (an "object")
    Execute(ctx context.Context, args json.RawMessage) (string, error)
}
```

`Execute` returns the string the model will *observe*. A returned `error` is not
fatal — `Registry.Execute` turns it into an observation prefixed with `error:` and
hands it to the model so it can recover.

## The exercise — a `unit_convert` tool

Add a tool that converts between a few units:

- kilometres → miles
- Celsius → Fahrenheit
- kilograms → pounds

### Step 1 — predict, before you write anything

Here is the skeleton. **It contains a deliberate flaw** — that is the exercise, not
a bug to report. Read it, then answer the question below *before* you run anything.

```go
package tools

import (
    "context"
    "encoding/json"
    "fmt"
)

type UnitConvert struct{}

func (UnitConvert) Name() string { return "unit_convert" }

func (UnitConvert) Description() string {
    return "Convert a value between units. Supported: km→mi, c→f, kg→lb."
}

func (UnitConvert) Schema() json.RawMessage {
    return json.RawMessage(`{
        "type": "object",
        "properties": {
            "value": { "type": "number", "description": "the amount to convert" },
            "from":  { "type": "string", "description": "source unit: km, c, or kg" }
        },
        "required": ["value", "from"]
    }`)
}

func (UnitConvert) Execute(_ context.Context, args json.RawMessage) (string, error) {
    var in struct {
        Value float64 `json:"value"`
        From  string  `json:"from"`
    }
    if err := json.Unmarshal(args, &in); err != nil {
        return "", fmt.Errorf("invalid arguments: %w", err)
    }
    switch in.From {
    case "km":
        return fmt.Sprintf("%.6g mi", in.Value*0.621371), nil
    // TODO: "c"  -> Fahrenheit:  value*9/5 + 32
    // TODO: "kg" -> pounds:      value*2.2046226
    default:
        return "", fmt.Errorf("unsupported unit %q (use km, c, or kg)", in.From)
    }
}
```

> **The question.** The schema says `"required": ["value", "from"]`. The model sends
> `{"from":"c"}` — valid JSON, no `value`. What does `Execute` return: an error, or a
> result? Write your answer down before reading on.

### Step 2 — fill in the TODOs, then test your prediction

Create `internal/tools/unitconvert.go` with the skeleton above and complete the two
`TODO`s. Then check your prediction with a throwaway test:

```go
func TestPrediction(t *testing.T) {
    got, err := tools.UnitConvert{}.Execute(context.Background(),
        json.RawMessage(`{"from":"c"}`))
    t.Log(got, err)
}
```

```bash
go test ./internal/tools/ -run Prediction -v
```

`json.Unmarshal` succeeds. `Value` is left at the zero value of `float64`, so the
tool answers **`32 f`** — a confident, well-formatted, entirely invented conversion
of a temperature nobody sent.

`required` is a line in a document you hand to the model. `encoding/json` has never
heard of your schema. **Nothing in the Go type system distinguishes "the field was
absent" from "the field was zero"** — and here zero is a perfectly legitimate input,
because 0 °C is a real temperature.

### Step 3 — represent "absent"

Which Go type can hold *both* a `float64` and the fact that no `float64` was given?
Change one line:

```go
Value *float64 `json:"value"`
```

Now the states are distinct:

```text
field absent      → nil
"value": null     → nil
"value": 0        → pointer to 0
wrong JSON type   → decode error
```

Validate after decoding, before converting:

```go
if in.Value == nil {
    return "", errors.New("missing required argument \"value\"")
}
```

…and dereference (`*in.Value`) where you do the arithmetic — the compiler will remind
you.

Do the same reasoning for `From`, and notice it needs *no* pointer: the zero value of
`string` is `""`, which is not a supported unit, so it falls through to your `default`
branch and errors on its own. **Reach for a pointer when the zero value is a valid
input, not by reflex.**

## What Go guarantees, what the model decides

This is the table to remember, and the reason this lesson comes before the safety
lessons:

```text
Go guarantees                          The model decides
-------------------------------------  -------------------------------------
the tool is in the Registry            whether to call it at all
routing is exact, by Name              which arguments it invents
the validation you wrote in Execute    how it reads your Description
the conversion arithmetic              how it interprets your observation
the observation is fed back            whether it recovers from your error
```

`Name`, `Description` and `Schema` are a **contract offered** to the model.
`Execute` is the **only contract enforced**. A schema that says `required` and an
`Execute` that assumes it was honoured is a guarantee about the qualifier, not about
the input — you will meet that exact shape again in Lesson 14, one floor down, where
an approval bound a tool's *name* but not its *arguments*.

## Write table tests

Create `internal/tools/unitconvert_test.go`. A table test drives many cases through
one loop (see `tools_test.go` for the pattern). Cover at least:

```text
1 km    → "0.621371 mi"
0 c     → "32 f"        ← proves 0 is accepted…
value absent → error    ← …while absence is refused. Both, or neither proves anything.
-40 c   → "-40 f"
unknown unit  → error
malformed JSON / wrong type → error
```

Two conventions worth copying:

- **Use `package tools_test`.** This package is honestly mixed —
  `tools_test.go` is external, `bash_test.go` and `webfetch_test.go` are internal —
  so this is a reasoned choice, not a house rule: your tool exposes nothing
  unexported, so test it through the same door the agent uses. (Check for yourself:
  `head -3 internal/tools/*_test.go`. Verifying a claim like this one is Lesson 15's
  whole subject.)
- **Assert `wantErr bool`, not the error text.** The message is not part of the
  contract and will be reworded. If the *kind* of failure ever becomes part of the
  contract, that is what a sentinel error and `errors.Is` are for.

```bash
go test ./internal/tools/ -run UnitConvert -v
```

## Register it — a separate guarantee

Green tests prove your conversions are right. They prove nothing about the agent
being *able* to call your tool: a tool that is never registered is dead code, and
every one of your tests still passes.

Find where the builtins are registered in `cmd/talunor/main.go` (search for
`tools.NewRegistry`) and add `tools.UnitConvert{}`:

```go
reg := tools.NewRegistry(
    tools.Calculator{},
    tools.Clock{},
    tools.UnitConvert{},
    tools.NewRecallMemory(store),
)
```

That line guarantees the tool can be *announced and routed*. It does not guarantee
the model will choose it.

## Try it end to end (optional, needs Ollama)

```bash
TALUNOR_TOOLS=1 go run ./cmd/talunor --plain
# then ask: "how far is 5 km in miles?"
```

```text
🔧 unit_convert({"value":5,"from":"km"})
   ↳ 3.10685 mi
```

Watch the whole path once: `Registry.Defs()` → `Agent.toolSpecs()` → `opts.Tools` →
the model emits a `ToolCall` → `reactLoop` → `runTool` → `policy.Evaluate` →
`Registry.Execute` → your `Execute` → an observation with `RoleTool` → a new call to
the provider. Deterministic everywhere except the two ends.

## If you get stuck

There is a worked solution, on its own branch:

```bash
git fetch origin solutions/06-unit-convert
git diff HEAD origin/solutions/06-unit-convert -- internal/tools/
```

Read it **after** you have tried, and ideally after you have answered the prediction
question — that one only works once. It lives off `main` on purpose: on `main` the
answer would sit next to the question, and `make release-check` refuses it (the
assertion `06 unit_convert is still NOT implemented upstream`). The branch's
`SOLUTION.md` says why, and why `release-check` deliberately **fails** there.

Your version does not have to match it. Two things are worth comparing: that `Value`
is a `*float64` while `From` is not, and that your table carries **both** `0 c` and a
missing `value`.

## The principle

> Adding a capability by **extension** (a new `Tool`) is safer than **modifying**
> the orchestrator. The agent loop never changed — you only added something it can
> choose to call. Good architecture makes the *common* change (a new capability)
> the *easy* change.

Look at what your branch actually touches: one new file, one test file, one line of
composition. That is the measurement, and it is why the interface exists — not
because interfaces are good, but because this diff is small.

## Proposing your patch

You now have working code and no idea what to do with it. That gap is where most
first contributions die, so this part is the lesson too.

### Where this PR goes — and where it does not

**Open the pull request against your own fork** (`your-user/Talunor`, branch →
`main`). It is a real PR: real diff, real description, real review, real merge.

```bash
git push -u fork learning/unit-convert-tool
gh pr create --repo <your-user>/Talunor --base main --head learning/unit-convert-tool
```

> `gh pr create` targets the **upstream** repository by default when it detects a
> fork. The explicit `--repo` is not decoration.

**One honest note about these four commands.** They are the only instructions in this
course that the maintainer cannot run: GitHub does not let an owner fork their own
repository. The `--repo` behaviour above is verified against `gh`'s own documentation, but
the end-to-end path has not been executed from another account. If it diverges for you,
say so in [issue #3](https://github.com/lao-tseu-is-alive/Talunor/issues/3) — that is a
real contribution, and it needs no Go.

**Do not send `unit_convert` upstream.** Every reader of this lesson writes the same
tool; the hundredth identical PR costs a maintainer real time and adds nothing the
project wants. This is not gatekeeping, it is the first rule of contributing:

> A patch is judged by what it changes for the maintainer, not by what it cost you.

### Self-review before you ask for review

Read your own diff before anyone else does — `git diff main...HEAD`, whole thing,
out loud if that helps. Then run the gate this project actually uses:

```bash
make release-check
```

Expect it to **fail**, and expect the failure to be interesting:

```text
atlas: not referenced: internal/tools/unitconvert.go
docs/atlas.md is stale — regenerate it
```

`atlas-check` walks `git ls-files` and requires every tracked file to appear in
`docs/atlas.md`. Untracked files pass; the moment you commit, they don't. Nothing was
broken by your code — a **drift alarm** noticed that you added a file to the project
and not to the map of the project. Regenerate the atlas (the `repo-atlas` skill, or
add the two lines by hand) and run it again.

That failure is the most transferable thing in this lesson: a mature repository tells
you what you forgot, at the cost of one command, *before* a human has to.

Then write the commit and the PR body:

- **Conventional Commits**, like the rest of the history:
  `feat(tools): add a unit_convert tool (km→mi, c→f, kg→lb)`.
- **The PR body answers three questions**: what changes, why it is wanted, how you
  know it works. Two sentences and the test command is enough. If you cannot say why
  it is wanted, that is information about the patch, not about your writing.
- **Name what you did not do.** "Only three units; no imperial→metric direction." A
  reviewer trusts a diff with a stated boundary more than one that implies it is
  complete.

### What this repository actually wants

If you want to contribute here for real, the open work is listed in `AGENTS.md`,
under **"Next — open threads"** — findings, fuzz targets, benchmarks, missing
coverage. The bar is written in the same file, under *"How it is built"*:
`make release-check` green, `docs/atlas.md` regenerated when files move, and — if
your change is a *layer* — a bilingual lesson in `docs/lessons/`, because a feature
without its lesson is only half of what this project ships.

Start by opening an issue that states the problem before you write the fix. The
cheapest patch to review is the one whose problem was agreed on first.

## Common mistakes

- **Trusting the schema.** `required` is a request to the model. Validate in
  `Execute` or you validate nowhere.
- **A pointer everywhere.** Only where the zero value is a legal input. `From` does
  not need one.
- **A vague `Description`.** The model decides whether to call your tool from this
  text — be concrete about what it does and when.
- **Forgetting to register the tool.** If it's not in the registry, the agent never
  sees it — and every test still passes.
- **Asserting error strings.** They get reworded; your suite goes red for nothing.
- **Opening the PR against upstream.** See above; check the `--repo` flag.

## Completion checklist

- [ ] I predicted what `{"from":"c"}` would do, and I was able to explain *why* the
      answer was `32 f` before I changed anything.
- [ ] I implemented all four `Tool` methods, with validation in `Execute`.
- [ ] I can state one thing `Schema()` guarantees and one thing it does not.
- [ ] I registered `unit_convert` in `cmd/talunor/main.go`.
- [ ] My table tests include **both** `0 c` and a missing `value`.
- [ ] I ran `make release-check`, understood why `atlas-check` failed, and fixed it.
- [ ] I opened a PR **against my own fork**, with a body a stranger could review.
- [ ] I can explain why this didn't require changing the agent loop.

**Next:** [Lesson 07 — Test without a real LLM](../07-test-without-a-real-llm/).

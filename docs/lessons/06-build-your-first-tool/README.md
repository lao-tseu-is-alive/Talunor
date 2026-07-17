# Lesson 06 — Build your first tool

**🛠️ Current contribution** · Level 2 · ~90 min

> This is a **contribution** lesson: you change the *current* project. Work on
> `main`, on your own branch — not on a detached tag.

## Why this lesson exists

You've read how Talunor works; now you'll *add* to it. The best first change is a
new **tool** — a capability the agent can call — because you can add one **without
touching the agent core**. That's the whole point of the tool interface, and doing
it once teaches you how well-designed extension points feel.

## Learning objectives

By the end you can:
- implement Talunor's `tools.Tool` interface from scratch;
- register a tool so the agent can call it;
- write table-driven tests for it;
- explain why adding a capability by *extension* beats editing the orchestrator.

## Prerequisites

- Lessons 00–05. You've seen the agent loop call tools (Lesson 05).

## Start a branch (on `main`)

```bash
git switch main
git pull
git switch -c learning/unit-convert-tool
```

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
fatal — it's handed to the model as an observation so it can recover.

## The exercise — a `unit_convert` tool

Add a tool that converts between a few units:

- kilometres → miles
- Celsius → Fahrenheit
- kilograms → pounds

Create `internal/tools/unitconvert.go`. Here's the skeleton — fill in the `// TODO`s
(use `Calculator` in `builtin.go` as your reference):

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

Then **register it** so the agent is offered it — find where the builtins are
registered in `cmd/talunor/main.go` (search for `tools.NewRegistry`) and add
`tools.UnitConvert{}` to the list.

## Write table tests

Create `internal/tools/unitconvert_test.go`. A table test drives many cases through
one loop (see `tools_test.go` for the pattern). Cover at least:

```text
1 km   → "0.621371 mi"
0 c    → "32 f"          (or however you format it)
invalid unit  → error
missing value → error (or a documented default)
```

Run them:

```bash
go test ./internal/tools/ -run UnitConvert -v
```

## Try it end to end (optional, needs Ollama)

```bash
TALUNOR_TOOLS=1 go run ./cmd/talunor --plain
# then ask: "how far is 5 km in miles?"
```

## The principle

> Adding a capability by **extension** (a new `Tool`) is safer than **modifying**
> the orchestrator. The agent loop never changed — you only added something it can
> choose to call. Good architecture makes the *common* change (a new capability)
> the *easy* change.

## Common mistakes

- **A vague `Description`.** The model decides whether to call your tool from this
  text — be concrete about what it does and when.
- **Not validating arguments.** Return a clear `error` for bad input; the model
  will see it and can correct itself.
- **Forgetting to register the tool.** If it's not in the registry, the agent never
  sees it.

## Completion checklist

- [ ] I implemented all four `Tool` methods.
- [ ] I registered `unit_convert` in `cmd/talunor/main.go`.
- [ ] I wrote table tests, including at least one error case, and they pass.
- [ ] I can explain why this didn't require changing the agent loop.
- [ ] My work is on a `learning/…` branch, not on `main` directly.

**Next:** [Lesson 07 — Test without a real LLM](../07-test-without-a-real-llm/).

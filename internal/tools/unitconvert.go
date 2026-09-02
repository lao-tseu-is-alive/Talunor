package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// UnitConvert converts a value between a few units. It is the worked solution to
// course Lesson 06 — see SOLUTION.md at the root of this branch. It lives here,
// off `main`, on purpose: on `main` the answer would sit next to the question.
type UnitConvert struct{}

// Name is the identifier the model calls.
func (UnitConvert) Name() string { return "unit_convert" }

// Description is what the model reads to decide whether this tool applies.
func (UnitConvert) Description() string {
	return "Convert a value between units. Supported: km→mi, c→f, kg→lb."
}

// Schema describes the arguments to the model. It is a contract OFFERED, not
// enforced: "required" below constrains nothing at runtime, which is why Execute
// validates again. See the lesson.
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

// Execute converts the value and returns the string the model will observe.
//
// Value is a *float64, not a float64: encoding/json enforces no schema, so an
// absent "value" would decode to the zero value and 0 °C — a legitimate input —
// would be indistinguishable from "the model sent nothing". From needs no
// pointer: its zero value ("") is not a supported unit, so it fails on its own
// in the default branch.
func (UnitConvert) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Value *float64 `json:"value"`
		From  string   `json:"from"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if in.Value == nil {
		return "", errors.New(`missing required argument "value"`)
	}
	switch in.From {
	case "km":
		return fmt.Sprintf("%.6g mi", *in.Value*0.621371), nil
	case "c":
		return fmt.Sprintf("%.6g f", *in.Value*9/5+32), nil
	case "kg":
		return fmt.Sprintf("%.6g lb", *in.Value*2.2046226), nil
	default:
		return "", fmt.Errorf("unsupported unit %q (use km, c, or kg)", in.From)
	}
}

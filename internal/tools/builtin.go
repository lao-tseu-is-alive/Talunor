package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"strconv"
	"time"
)

// Calculator evaluates a basic arithmetic expression. It is deliberately tiny
// and safe: it parses the expression to an AST and walks only numeric literals,
// parentheses, unary +/-, and the binary operators + - * / — anything else
// (identifiers, function calls, …) is rejected, so no code is ever executed.
type Calculator struct{}

func (Calculator) Name() string { return "calculator" }

func (Calculator) Description() string {
	return "Evaluate a basic arithmetic expression with + - * / and parentheses, " +
		"e.g. \"12 * (3 + 4)\". Use this instead of doing arithmetic yourself."
}

func (Calculator) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"expression": {
				"type": "string",
				"description": "The arithmetic expression to evaluate, e.g. \"2 + 2 * 5\"."
			}
		},
		"required": ["expression"]
	}`)
}

func (Calculator) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Expression string `json:"expression"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	if in.Expression == "" {
		return "", fmt.Errorf("expression is required")
	}
	expr, err := parser.ParseExpr(in.Expression)
	if err != nil {
		return "", fmt.Errorf("cannot parse %q: %w", in.Expression, err)
	}
	v, err := evalArith(expr)
	if err != nil {
		return "", err
	}
	// Format whole numbers as integers (no scientific notation); otherwise a
	// plain decimal without an exponent.
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatInt(int64(v), 10), nil
	}
	return strconv.FormatFloat(v, 'f', -1, 64), nil
}

// evalArith evaluates the arithmetic-only subset of a Go expression AST.
func evalArith(n ast.Expr) (float64, error) {
	switch e := n.(type) {
	case *ast.BasicLit:
		if e.Kind != token.INT && e.Kind != token.FLOAT {
			return 0, fmt.Errorf("unsupported literal %q", e.Value)
		}
		return strconv.ParseFloat(e.Value, 64)
	case *ast.ParenExpr:
		return evalArith(e.X)
	case *ast.UnaryExpr:
		v, err := evalArith(e.X)
		if err != nil {
			return 0, err
		}
		switch e.Op {
		case token.ADD:
			return v, nil
		case token.SUB:
			return -v, nil
		}
		return 0, fmt.Errorf("unsupported unary operator %q", e.Op)
	case *ast.BinaryExpr:
		l, err := evalArith(e.X)
		if err != nil {
			return 0, err
		}
		r, err := evalArith(e.Y)
		if err != nil {
			return 0, err
		}
		switch e.Op {
		case token.ADD:
			return l + r, nil
		case token.SUB:
			return l - r, nil
		case token.MUL:
			return l * r, nil
		case token.QUO:
			if r == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			return l / r, nil
		}
		return 0, fmt.Errorf("unsupported operator %q", e.Op)
	default:
		return 0, fmt.Errorf("unsupported expression (only numbers and + - * / are allowed)")
	}
}

// Clock reports the current time, optionally in a given IANA timezone.
type Clock struct{}

func (Clock) Name() string { return "current_time" }

func (Clock) Description() string {
	return "Get the current date and time. Optionally pass an IANA timezone " +
		"(e.g. \"Europe/Zurich\", \"UTC\"); defaults to UTC."
}

func (Clock) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"timezone": {
				"type": "string",
				"description": "IANA timezone name, e.g. \"Europe/Zurich\". Defaults to UTC."
			}
		}
	}`)
}

func (Clock) Execute(_ context.Context, args json.RawMessage) (string, error) {
	in := struct {
		Timezone string `json:"timezone"`
	}{Timezone: "UTC"}
	// Arguments are optional; ignore an empty/absent body.
	if len(args) > 0 {
		if err := json.Unmarshal(args, &in); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
	}
	if in.Timezone == "" {
		in.Timezone = "UTC"
	}
	loc, err := time.LoadLocation(in.Timezone)
	if err != nil {
		return "", fmt.Errorf("unknown timezone %q", in.Timezone)
	}
	return time.Now().In(loc).Format("2006-01-02 15:04:05 MST"), nil
}

package calibration

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Assert is a deterministic check on an assistant's reply. Every set field must
// hold (logical AND); AnyOf / AllOf compose nested asserts. There is deliberately
// NO LLM-based matcher: a calibration verifier that itself asked a model to judge
// the answer would inherit the very unreliability the harness exists to measure.
// All matching is done on the whitespace-trimmed reply.
type Assert struct {
	Equals         *string      `yaml:"equals,omitempty"`           // trimmed reply == value (trimmed)
	IEquals        *string      `yaml:"iequals,omitempty"`          // case-insensitive equals
	Contains       *string      `yaml:"contains,omitempty"`         // substring present
	ContainsAll    []string     `yaml:"contains_all,omitempty"`     // every substring present
	ContainsAny    []string     `yaml:"contains_any,omitempty"`     // at least one substring present
	NotContains    *string      `yaml:"not_contains,omitempty"`     // substring absent
	NotContainsAny []string     `yaml:"not_contains_any,omitempty"` // every substring absent
	Regex          *string      `yaml:"regex,omitempty"`            // matches this RE2 pattern
	Number         *NumberMatch `yaml:"number,omitempty"`           // a numeric token ≈ a value
	JSONValid      *bool        `yaml:"json_valid,omitempty"`       // reply parses (or not) as JSON
	AnyOf          []Assert     `yaml:"any_of,omitempty"`           // at least one sub-assert holds
	AllOf          []Assert     `yaml:"all_of,omitempty"`           // every sub-assert holds
}

// NumberMatch holds when any numeric token in the reply equals Equals within an
// absolute Tolerance — handy for arithmetic where the model may wrap the number
// in prose ("The answer is 548.") or format it as "548.0".
type NumberMatch struct {
	Equals    float64 `yaml:"equals"`
	Tolerance float64 `yaml:"tolerance,omitempty"`
}

// numberToken matches a signed integer or decimal.
var numberToken = regexp.MustCompile(`-?\d+(?:\.\d+)?`)

// Check reports whether the reply satisfies the assert, with a short reason on
// failure. It is pure and deterministic.
func (a Assert) Check(reply string) (bool, string) {
	s := strings.TrimSpace(reply)

	if a.Equals != nil && s != strings.TrimSpace(*a.Equals) {
		return false, fmt.Sprintf("equals %q, got %q", *a.Equals, s)
	}
	if a.IEquals != nil && !strings.EqualFold(s, strings.TrimSpace(*a.IEquals)) {
		return false, fmt.Sprintf("iequals %q, got %q", *a.IEquals, s)
	}
	if a.Contains != nil && !strings.Contains(s, *a.Contains) {
		return false, fmt.Sprintf("missing %q", *a.Contains)
	}
	for _, sub := range a.ContainsAll {
		if !strings.Contains(s, sub) {
			return false, fmt.Sprintf("missing %q", sub)
		}
	}
	if a.ContainsAny != nil && !containsAny(s, a.ContainsAny) {
		return false, fmt.Sprintf("none of %v present", a.ContainsAny)
	}
	if a.NotContains != nil && strings.Contains(s, *a.NotContains) {
		return false, fmt.Sprintf("must not contain %q", *a.NotContains)
	}
	for _, sub := range a.NotContainsAny {
		if strings.Contains(s, sub) {
			return false, fmt.Sprintf("must not contain %q", sub)
		}
	}
	if a.Regex != nil {
		re, err := regexp.Compile(*a.Regex)
		if err != nil {
			return false, fmt.Sprintf("bad regex %q: %v", *a.Regex, err)
		}
		if !re.MatchString(s) {
			return false, fmt.Sprintf("regex %q did not match", *a.Regex)
		}
	}
	if a.Number != nil && !a.Number.match(s) {
		return false, fmt.Sprintf("no numeric token ≈ %g (±%g)", a.Number.Equals, a.Number.Tolerance)
	}
	if a.JSONValid != nil {
		if valid := json.Valid([]byte(s)); valid != *a.JSONValid {
			return false, fmt.Sprintf("json_valid=%v, got valid=%v", *a.JSONValid, valid)
		}
	}
	for _, sub := range a.AllOf {
		if ok, why := sub.Check(reply); !ok {
			return false, "all_of: " + why
		}
	}
	if len(a.AnyOf) > 0 {
		matched := false
		for _, sub := range a.AnyOf {
			if ok, _ := sub.Check(reply); ok {
				matched = true
				break
			}
		}
		if !matched {
			return false, "any_of: none matched"
		}
	}
	return true, ""
}

func (m NumberMatch) match(s string) bool {
	for _, tok := range numberToken.FindAllString(s, -1) {
		if v, err := strconv.ParseFloat(tok, 64); err == nil {
			if abs(v-m.Equals) <= m.Tolerance {
				return true
			}
		}
	}
	return false
}

// isEmpty reports whether the assert checks nothing. An empty assert would pass
// any reply, which is almost always an authoring mistake, so Validate rejects it.
func (a Assert) isEmpty() bool {
	return a.Equals == nil && a.IEquals == nil && a.Contains == nil &&
		len(a.ContainsAll) == 0 && len(a.ContainsAny) == 0 &&
		a.NotContains == nil && len(a.NotContainsAny) == 0 &&
		a.Regex == nil && a.Number == nil && a.JSONValid == nil &&
		len(a.AnyOf) == 0 && len(a.AllOf) == 0
}

// validate checks the assert is well-formed: non-empty, a compilable regex, a
// non-negative tolerance, and valid sub-asserts.
func (a Assert) validate() error {
	if a.isEmpty() {
		return fmt.Errorf("assert checks nothing")
	}
	if a.Regex != nil {
		if _, err := regexp.Compile(*a.Regex); err != nil {
			return fmt.Errorf("bad regex %q: %w", *a.Regex, err)
		}
	}
	if a.Number != nil && a.Number.Tolerance < 0 {
		return fmt.Errorf("number tolerance must be >= 0")
	}
	for i, sub := range a.AllOf {
		if err := sub.validate(); err != nil {
			return fmt.Errorf("all_of[%d]: %w", i, err)
		}
	}
	for i, sub := range a.AnyOf {
		if err := sub.validate(); err != nil {
			return fmt.Errorf("any_of[%d]: %w", i, err)
		}
	}
	return nil
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

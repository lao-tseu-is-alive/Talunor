package calibration

import (
	"strings"
	"testing"
)

func sp(s string) *string { return &s }
func bp(b bool) *bool     { return &b }

func TestAssertCheck(t *testing.T) {
	tests := []struct {
		name   string
		assert Assert
		reply  string
		want   bool
	}{
		{"equals ok (trimmed)", Assert{Equals: sp("548")}, "  548\n", true},
		{"equals fail", Assert{Equals: sp("548")}, "549", false},
		{"iequals ok", Assert{IEquals: sp("Paris")}, "paris", true},
		{"contains ok", Assert{Contains: sp("274")}, "the answer is 274.", true},
		{"contains fail", Assert{Contains: sp("274")}, "no number here", false},
		{"contains_all ok", Assert{ContainsAll: []string{"a", "b"}}, "a and b", true},
		{"contains_all fail", Assert{ContainsAll: []string{"a", "z"}}, "a only", false},
		{"contains_any ok", Assert{ContainsAny: []string{"x", "b"}}, "just b", true},
		{"contains_any fail", Assert{ContainsAny: []string{"x", "y"}}, "neither", false},
		{"not_contains ok", Assert{NotContains: sp("1918")}, "RFC 6598", true},
		{"not_contains fail", Assert{NotContains: sp("1918")}, "RFC 1918", false},
		{"not_contains_any fail", Assert{NotContainsAny: []string{"foo", "bar"}}, "has bar", false},
		{"regex ok", Assert{Regex: sp(`^\d{3}$`)}, "548", true},
		{"regex fail", Assert{Regex: sp(`^\d{3}$`)}, "54", false},
		{"number in prose", Assert{Number: &NumberMatch{Equals: 548}}, "The answer is 548.", true},
		{"number tolerance exceeded", Assert{Number: &NumberMatch{Equals: 3.14, Tolerance: 0.01}}, "the value is 3.2", false},
		{"number tolerance ok", Assert{Number: &NumberMatch{Equals: 3.14, Tolerance: 0.2}}, "about 3.0", true},
		{"number none", Assert{Number: &NumberMatch{Equals: 548}}, "no digits", false},
		{"json valid true", Assert{JSONValid: bp(true)}, `{"a":1}`, true},
		{"json valid true but not json", Assert{JSONValid: bp(true)}, "hello", false},
		{"json valid false ok", Assert{JSONValid: bp(false)}, "hello", true},
		{"AND of two fields", Assert{Contains: sp("274"), NotContains: sp("error")}, "got 274 ok", true},
		{"AND fails on second", Assert{Contains: sp("274"), NotContains: sp("error")}, "274 but error", false},
		{"any_of ok", Assert{AnyOf: []Assert{{Equals: sp("yes")}, {Equals: sp("no")}}}, "no", true},
		{"any_of fail", Assert{AnyOf: []Assert{{Equals: sp("yes")}, {Equals: sp("no")}}}, "maybe", false},
		{"all_of ok", Assert{AllOf: []Assert{{Contains: sp("a")}, {Contains: sp("b")}}}, "a b", true},
		{"all_of fail", Assert{AllOf: []Assert{{Contains: sp("a")}, {Contains: sp("z")}}}, "a only", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, reason := tt.assert.Check(tt.reply)
			if got != tt.want {
				t.Errorf("Check(%q) = %v (%s), want %v", tt.reply, got, reason, tt.want)
			}
			if !got && reason == "" {
				t.Error("a failed check must give a reason")
			}
		})
	}
}

func TestAssertValidate(t *testing.T) {
	tests := []struct {
		name    string
		assert  Assert
		wantErr string // "" = valid
	}{
		{"valid", Assert{Equals: sp("x")}, ""},
		{"empty", Assert{}, "checks nothing"},
		{"bad regex", Assert{Regex: sp("(")}, "bad regex"},
		{"negative tolerance", Assert{Number: &NumberMatch{Equals: 1, Tolerance: -1}}, "tolerance must be >= 0"},
		{"empty nested any_of", Assert{AnyOf: []Assert{{}}}, "checks nothing"},
		{"valid nested", Assert{AllOf: []Assert{{Contains: sp("a")}}}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.assert.validate()
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

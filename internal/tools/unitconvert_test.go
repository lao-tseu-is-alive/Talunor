package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/lao-tseu-is-alive/Talunor/internal/tools"
)

// TestUnitConvert drives the whole contract through one table. wantErr is a bool
// rather than the error text: the message is not part of the contract and will be
// reworded. If the KIND of failure ever becomes contractual, that is what a
// sentinel error and errors.Is are for.
//
// The two cases that carry the lesson are "c freezing" and "err missing value":
// 0 must be ACCEPTED and absence must be REFUSED. Either one alone proves nothing
// — a float64 field passes the first and fails the second.
func TestUnitConvert(t *testing.T) {
	c := tools.UnitConvert{}
	cases := []struct {
		name    string
		args    string
		want    string
		wantErr bool
	}{
		// kilometres → miles
		{name: "km base", args: `{"value": 1, "from": "km"}`, want: "0.621371 mi"},
		{name: "km large", args: `{"value": 100, "from": "km"}`, want: "62.1371 mi"},
		{name: "km zero", args: `{"value": 0, "from": "km"}`, want: "0 mi"},

		// Celsius → Fahrenheit
		{name: "c freezing", args: `{"value": 0, "from": "c"}`, want: "32 f"},
		{name: "c boiling", args: `{"value": 100, "from": "c"}`, want: "212 f"},
		{name: "c negative", args: `{"value": -40, "from": "c"}`, want: "-40 f"},

		// kilograms → pounds
		{name: "kg base", args: `{"value": 1, "from": "kg"}`, want: "2.20462 lb"},
		{name: "kg fractional", args: `{"value": 2.5, "from": "kg"}`, want: "5.51156 lb"},
		{name: "kg large", args: `{"value": 50, "from": "kg"}`, want: "110.231 lb"},

		// failures the model must be able to observe and recover from
		{name: "err unsupported unit", args: `{"value": 10, "from": "miles"}`, wantErr: true},
		{name: "err missing from", args: `{"value": 10}`, wantErr: true},
		{name: "err missing value", args: `{"from": "km"}`, wantErr: true},
		{name: "err wrong value type", args: `{"value": "not-a-number", "from": "km"}`, wantErr: true},
		{name: "err invalid json", args: `{value: 1, "from": "km"}`, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.Execute(context.Background(), json.RawMessage(tc.args))
			if (err != nil) != tc.wantErr {
				t.Fatalf("Execute() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("Execute() = %q; want %q", got, tc.want)
			}
		})
	}
}

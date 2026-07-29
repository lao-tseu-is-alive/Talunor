package testenv

import "testing"

func TestRequiredParsesTheList(t *testing.T) {
	cases := []struct {
		env  string
		cap  string
		want bool
	}{
		{"", CapSandbox, false},
		{"sandbox", CapSandbox, true},
		{"ext,docker", CapSandbox, false},
		{"ext, sandbox , docker", CapSandbox, true},
		{"all", CapExt, true},
		{"ALL", CapDocker, true},
		{"Sandbox", CapSandbox, true},
		{"sandboxes", CapSandbox, false}, // no prefix matching
	}
	for _, tc := range cases {
		t.Setenv(EnvRequire, tc.env)
		if got := Required(tc.cap); got != tc.want {
			t.Errorf("%s=%q: Required(%q) = %v; want %v", EnvRequire, tc.env, tc.cap, got, tc.want)
		}
	}
}

// TestRequireIsANoOpWhenAvailable: the common path must not touch the env at all
// — a capability that works is never anyone's business.
func TestRequireIsANoOpWhenAvailable(t *testing.T) {
	t.Setenv(EnvRequire, "all")
	Require(t, CapSandbox, nil) // neither skips nor fails
}

// The skip/fail branches end the calling test by construction, so they cannot be
// asserted on a real *testing.T. Required — where the decision actually lives —
// is pinned above; what is left in Require is a three-way switch over it.

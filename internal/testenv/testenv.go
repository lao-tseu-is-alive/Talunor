// Package testenv turns "I cannot test this here" into an explicit, per-host
// decision.
//
// Several of Talunor's tests need something the machine may not have: the
// fetched SQLite extensions (`make deps`), unprivileged user namespaces, a
// container runtime. Skipping those tests is the right default — a contributor
// without user namespaces should not get a red suite — but the default has a
// sharp edge: `go test` prints the same `ok` whether a package ran twenty tests
// or none, so a host can quietly *lose* a capability and the suite will keep
// looking green while covering less and less. That is exactly how a broken
// sandbox backend survived a green run (see docs/lessons/22-the-silent-suite).
//
// So a host can declare what it must be able to exercise:
//
//	TALUNOR_REQUIRE=sandbox        # this machine must be able to run the namespaces backend
//	TALUNOR_REQUIRE=ext,docker     # …several capabilities
//	TALUNOR_REQUIRE=all            # a full-fidelity dev machine (what the maintainer exports)
//
// On such a host a missing capability FAILS instead of skipping. Everywhere
// else — a contributor's laptop, a minimal CI runner — behaviour is unchanged.
//
// This is deliberately an environment variable and not a `.env` entry:
// `go test` does not load `.env` (only cmd/talunor and cmd/calibrate do), so
// export it from your shell profile or pass it on the command line.
package testenv

import (
	"os"
	"strings"
	"testing"
)

// The capabilities a test can require. Keep these in sync with the docs in
// AGENTS.md and .env_sample.
const (
	// CapExt is the fetched embedding stack: ext/vector.so, ext/ai.so and the
	// GGUF model (`make deps`).
	CapExt = "ext"
	// CapSandbox is the rootless namespaces sandbox backend: Linux, plus
	// unprivileged user namespaces the AppArmor gate has not disabled.
	CapSandbox = "sandbox"
	// CapDocker is a container runtime on PATH (nerdctl or docker) for the OCI
	// sandbox backend.
	CapDocker = "docker"
	// CapFTS5 is SQLite's FTS5 module, which mattn/go-sqlite3 compiles in only
	// under `-tags sqlite_fts5`. Without it, hybrid recall (LAYER 22) has no
	// lexical arm — a capability that depends on how the binary was BUILT rather
	// than on what the machine has installed, and therefore especially easy to
	// lose without noticing.
	CapFTS5 = "fts5"
)

// EnvRequire names the variable holding the comma-separated capability list.
const EnvRequire = "TALUNOR_REQUIRE"

// Required reports whether this host has declared that it must be able to
// exercise the given capability.
func Required(capability string) bool {
	for want := range strings.SplitSeq(os.Getenv(EnvRequire), ",") {
		switch strings.ToLower(strings.TrimSpace(want)) {
		case "all":
			return true
		case strings.ToLower(capability):
			return true
		}
	}
	return false
}

// Require is the one call site for "this test needs something the host may not
// have". Pass the error that says why the capability is unusable (nil when it
// is fine): the test then either continues, skips, or — on a host that declared
// the capability required — fails with the underlying reason.
//
//	sb, err := newNamespaces()
//	testenv.Require(t, testenv.CapSandbox, err)
//	// … from here the capability is available
func Require(t *testing.T, capability string, availability error) {
	t.Helper()
	if availability == nil {
		return
	}
	if Required(capability) {
		t.Fatalf("%s=%s declares this host must be able to exercise %q, but it cannot: %v",
			EnvRequire, os.Getenv(EnvRequire), capability, availability)
	}
	t.Skipf("%s capability unavailable: %v (set %s=%s to make this a failure)",
		capability, availability, EnvRequire, capability)
}

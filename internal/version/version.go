// Package version carries Talunor's build identity. The semantic version is
// bumped once per completed build step/layer (see CHANGELOG.md); Commit and
// Date are injected at build time via -ldflags (see the Makefile).
package version

import "fmt"

// Version is Talunor's semantic version. Scheme: 0.MINOR.PATCH, where each
// completed layer of the MVP bumps MINOR. Iteration 1 (conversational agent +
// memory) completes at 0.5.0.
const Version = "0.15.0"

// Name is the application name.
const Name = "Talunor"

// Commit and Date are overridden at build time with -ldflags -X. They keep
// their defaults for `go run` / `go test` builds.
var (
	Commit = "dev"
	Date   = "unknown"
)

// String returns a human-readable, one-line build identity.
func String() string {
	return fmt.Sprintf("%s v%s (commit %s, built %s)", Name, Version, Commit, Date)
}

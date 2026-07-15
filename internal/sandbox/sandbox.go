// Package sandbox runs an untrusted shell script under resource and isolation
// limits, and returns its combined stdout+stderr as the observation Talunor's
// bash tool feeds back to the model.
//
// Two backends implement the same [Sandbox] interface:
//
//   - "nerdctl" (see runtime.go): shells out to nerdctl or docker. This is the
//     strong option — a real OCI runtime with seccomp, cgroups, and dropped
//     capabilities. Preferred when a runtime binary is present.
//   - "namespaces" (see namespaces_linux.go): a from-scratch Linux backend built
//     directly on user/mount/pid/net namespaces. It is defense-in-depth and a
//     teaching artifact, NOT a strong boundary (no seccomp; see the file header
//     for the full caveat list). Use it when no runtime is available.
//
// [FromEnv] selects a backend from TALUNOR_SANDBOX (or auto-detects). Whatever
// the backend, the contract is the same: a non-zero exit from the script is not
// a Go error — it is normal output the model should see. Only infrastructure
// failures (backend unavailable, timeout, setup error) return an error.
package sandbox

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// Limits bounds a single sandboxed run. The zero value is not useful; start
// from [DefaultLimits].
type Limits struct {
	Timeout  time.Duration // hard wall-clock kill (0 = backend default)
	MaxProcs int           // process/pids cap (fork-bomb guard)
	MemBytes int64         // memory cap in bytes
	FSBytes  int64         // writable (tmpfs) size cap in bytes
	Network  bool          // false (default) = no network at all
}

// DefaultLimits are conservative bounds suited to a model running one-off
// commands: a short timeout, a small process/memory/disk budget, no network.
func DefaultLimits() Limits {
	return Limits{
		Timeout:  10 * time.Second,
		MaxProcs: 64,
		MemBytes: 128 << 20, // 128 MiB
		FSBytes:  64 << 20,  // 64 MiB
		Network:  false,
	}
}

// Sandbox runs a shell script under a set of limits.
type Sandbox interface {
	// Run executes script (interpreted by /bin/sh) and returns its combined
	// stdout+stderr. A non-zero exit is reported inside output (with a note),
	// not as err; err is reserved for infrastructure failures — the backend
	// being unavailable, a setup error, or the timeout firing.
	Run(ctx context.Context, script string, lim Limits) (output string, err error)
	// Name identifies the backend ("nerdctl", "docker", "namespaces").
	Name() string
}

// FromEnv selects a backend. TALUNOR_SANDBOX may be "nerdctl" (or its alias
// "docker" / "runtime") or "namespaces". When unset it auto-detects: a container
// runtime if one is on PATH, otherwise the namespaces backend on Linux. It
// returns a clear error when the requested (or only available) backend cannot be
// used on this host.
func FromEnv() (Sandbox, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("TALUNOR_SANDBOX"))) {
	case "nerdctl", "docker", "runtime", "container":
		return newRuntime()
	case "namespaces", "ns", "userns":
		return newNamespaces()
	case "":
		// Auto-detect: prefer a real runtime; fall back to namespaces.
		if sb, err := newRuntime(); err == nil {
			return sb, nil
		}
		sb, err := newNamespaces()
		if err != nil {
			return nil, fmt.Errorf("no container runtime (nerdctl/docker) found and the "+
				"namespaces backend is unavailable: %w; install nerdctl or set TALUNOR_SANDBOX", err)
		}
		return sb, nil
	default:
		return nil, fmt.Errorf("unknown TALUNOR_SANDBOX %q (want \"nerdctl\" or \"namespaces\")",
			os.Getenv("TALUNOR_SANDBOX"))
	}
}

// exitNote formats the trailing note appended to output when a script exits
// non-zero. Kept here so both backends word it identically.
func exitNote(code int) string {
	return fmt.Sprintf("\n[exit status %d]", code)
}

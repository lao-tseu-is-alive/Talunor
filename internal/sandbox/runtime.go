package sandbox

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// defaultImage is the container image the runtime backend runs scripts in. It is
// small and pinned; override with TALUNOR_SANDBOX_IMAGE. On first use the runtime
// pulls it, which may take a few seconds.
const defaultImage = "alpine:3.20"

// ociRuntime is the strong backend: it delegates isolation to an OCI runtime
// (nerdctl or docker), which brings a default seccomp profile and cgroups. We do
// NOT rely on the runtime's default capability set, though: the flags below drop
// ALL capabilities, forbid privilege escalation (no-new-privileges) and run as a
// non-root uid, so the container matches the "capability dropping" the docs
// promise rather than trusting the runtime's (root-with-default-caps) defaults.
type ociRuntime struct {
	bin   string // absolute path to nerdctl or docker
	name  string // "nerdctl" or "docker"
	image string
}

// runtimeProbeTimeout bounds the daemon probe. `info` talks to the daemon, so it
// can hang (a VM still booting) rather than fail — short enough not to stall
// startup, long enough for a cold client.
const runtimeProbeTimeout = 5 * time.Second

// findRuntimeBinary returns the first container runtime CLI on PATH, preferring
// nerdctl (the user runs Rancher Desktop) and falling back to docker.
//
// Finding the binary is NOT the same as being able to run a container: with
// Rancher Desktop, `nerdctl` is a shim that exists whether or not its VM is
// up. See runtimeAvailable.
func findRuntimeBinary() (bin, name string, err error) {
	for _, n := range []string{"nerdctl", "docker"} {
		if b, err := exec.LookPath(n); err == nil {
			return b, n, nil
		}
	}
	return "", "", errors.New("neither nerdctl nor docker is on PATH")
}

// runtimeAvailable reports whether this host can actually RUN a container: a CLI
// on PATH *and* a daemon that answers. nil means usable; the error says which
// half is missing.
//
// The distinction is the point. A capability check that stops at exec.LookPath
// reports a runtime that cannot execute anything, which turns a test that should
// SKIP into a test that FAILS (docs/lessons/22-the-silent-suite).
func runtimeAvailable(ctx context.Context) error {
	bin, name, err := findRuntimeBinary()
	if err != nil {
		return err
	}
	return probeRuntime(ctx, bin, name)
}

// probeRuntime asks the runtime for its daemon-side state and classifies the
// answer. `info` is the cheapest command that fails when the daemon is down and
// succeeds when a container could be started.
func probeRuntime(ctx context.Context, bin, name string) error {
	probeCtx, cancel := context.WithTimeout(ctx, runtimeProbeTimeout)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, bin, "info").CombinedOutput()
	return classifyProbe(name, out, err, probeCtx.Err())
}

// classifyProbe turns the outcome of `<runtime> info` into the capability
// answer. Kept pure — no exec, no env — so the case that actually bit us (binary
// present, daemon unreachable) is table-testable on any host, including one with
// no container runtime at all.
func classifyProbe(name string, out []byte, runErr, ctxErr error) error {
	if ctxErr != nil {
		return fmt.Errorf("%s is on PATH but did not answer within %s (daemon starting or wedged?)",
			name, runtimeProbeTimeout)
	}
	if runErr != nil {
		return fmt.Errorf("%s is on PATH but its daemon is not usable: %s",
			name, probeDiagnostic(string(out)))
	}
	return nil
}

// probeDiagnostic extracts the line explaining a failing probe. Both clients
// print their own "Client:" block first and the failure LAST (docker after a
// "Server:" header, the Rancher shim as its only line), so the last meaningful
// line is the diagnostic — no guessing at wording.
func probeDiagnostic(out string) string {
	const maxLen = 200
	diagnostic := ""
	for line := range strings.SplitSeq(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasSuffix(line, ":") {
			continue // blank, or a section header like "Server:"
		}
		diagnostic = line
	}
	if diagnostic == "" {
		return "no output"
	}
	if len(diagnostic) > maxLen {
		return diagnostic[:maxLen] + "…"
	}
	return diagnostic
}

// newRuntime builds the OCI backend around the runtime CLI found on PATH. It
// does not probe the daemon: callers that need to know whether a container can
// actually run ask runtimeAvailable.
func newRuntime() (Sandbox, error) {
	bin, name, err := findRuntimeBinary()
	if err != nil {
		return nil, err
	}
	img := strings.TrimSpace(os.Getenv("TALUNOR_SANDBOX_IMAGE"))
	if img == "" {
		img = defaultImage
	}
	return &ociRuntime{bin: bin, name: name, image: img}, nil
}

func (r *ociRuntime) Name() string { return r.name }

func (r *ociRuntime) Run(ctx context.Context, script string, lim Limits) (string, error) {
	if strings.TrimSpace(script) == "" {
		return "", errors.New("empty script")
	}
	timeout := lim.Timeout
	if timeout <= 0 {
		timeout = DefaultLimits().Timeout
	}
	// Give the client a little slack over the in-container timeout so the
	// container-side `timeout` fires first (cleaner than killing the client).
	runCtx, cancel := context.WithTimeout(ctx, timeout+5*time.Second)
	defer cancel()

	args := []string{
		"run", "--rm",
		"--read-only",                         // rootfs is immutable; only the tmpfs below is writable
		"--cpus=1",                            // one CPU's worth of time
		"--cap-drop=ALL",                      // drop every Linux capability (untrusted code)
		"--security-opt", "no-new-privileges", // a setuid binary can't regain privilege
		"--user", "65534:65534", // run as nobody, never root in the container
		"--tmpfs", "/tmp:size=" + strconv.FormatInt(lim.FSBytes, 10) + ",exec,nosuid,nodev",
	}
	if !lim.Network {
		args = append(args, "--network", "none")
	}
	if lim.MaxProcs > 0 {
		args = append(args, "--pids-limit", strconv.Itoa(lim.MaxProcs))
	}
	if lim.MemBytes > 0 {
		args = append(args, "--memory", strconv.FormatInt(lim.MemBytes, 10))
	}
	// Run the script under a container-side wall-clock guard (busybox timeout),
	// then the client-side context as a backstop.
	guarded := fmt.Sprintf("timeout %d sh -c %s", int(timeout.Seconds()), shellQuote(script))
	args = append(args, r.image, "sh", "-c", guarded)

	cmd := exec.CommandContext(runCtx, r.bin, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()

	// Distinguish a real timeout from a normal non-zero exit.
	if runCtx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
		return truncate(buf.String()), fmt.Errorf("sandbox: command timed out after %s", timeout)
	}
	if ctx.Err() != nil {
		return truncate(buf.String()), ctx.Err()
	}
	out := buf.String()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		code := exitErr.ExitCode()
		// busybox `timeout` uses 143 (128+SIGTERM) when it kills the script.
		if code == 143 {
			return truncate(out) + exitNote(code) + " (timed out)", nil
		}
		return truncate(out) + exitNote(code), nil
	}
	if err != nil {
		// Infrastructure failure (couldn't start the runtime, image pull failed…).
		return truncate(out), fmt.Errorf("sandbox: %s run failed: %w", r.name, err)
	}
	return truncate(out), nil
}

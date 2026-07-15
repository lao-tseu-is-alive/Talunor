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
// (nerdctl or docker), which brings seccomp, cgroups, and capability dropping
// for free. We only assemble flags that mirror our [Limits].
type ociRuntime struct {
	bin   string // absolute path to nerdctl or docker
	name  string // "nerdctl" or "docker"
	image string
}

// newRuntime finds a container runtime on PATH, preferring nerdctl (the user
// runs Rancher Desktop) and falling back to docker.
func newRuntime() (Sandbox, error) {
	for _, name := range []string{"nerdctl", "docker"} {
		if bin, err := exec.LookPath(name); err == nil {
			img := strings.TrimSpace(os.Getenv("TALUNOR_SANDBOX_IMAGE"))
			if img == "" {
				img = defaultImage
			}
			return &ociRuntime{bin: bin, name: name, image: img}, nil
		}
	}
	return nil, errors.New("neither nerdctl nor docker is on PATH")
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
		"--read-only", // rootfs is immutable; only the tmpfs below is writable
		"--cpus=1",    // one CPU's worth of time
		"--tmpfs", "/tmp:size=" + strconv.FormatInt(lim.FSBytes, 10) + ",exec",
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

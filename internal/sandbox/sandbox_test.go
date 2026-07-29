package sandbox

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lao-tseu-is-alive/Talunor/internal/testenv"
)

// requireNamespaces returns the rootless backend, or ends the test: a skip
// normally, a failure on a host that declared TALUNOR_REQUIRE=sandbox. The
// declaration is what stops a machine from silently losing the capability and
// still reporting `ok` (see docs/lessons/22-the-silent-suite).
func requireNamespaces(t *testing.T) Sandbox {
	t.Helper()
	if runtime.GOOS != "linux" {
		testenv.Require(t, testenv.CapSandbox, errors.New("the namespaces backend is Linux-only"))
	}
	sb, err := newNamespaces()
	testenv.Require(t, testenv.CapSandbox, err)
	return sb
}

// requireRuntime returns the OCI backend under the same contract
// (TALUNOR_REQUIRE=docker).
func requireRuntime(t *testing.T) Sandbox {
	t.Helper()
	if !hasRuntime() {
		testenv.Require(t, testenv.CapDocker, errors.New("no nerdctl/docker on PATH"))
	}
	sb, err := newRuntime()
	if err != nil {
		t.Fatalf("newRuntime: %v", err)
	}
	return sb
}

// smallLimits keeps tests fast while still exercising each cap.
func smallLimits() Limits {
	l := DefaultLimits()
	l.Timeout = 5 * time.Second
	return l
}

func hasRuntime() bool {
	for _, n := range []string{"nerdctl", "docker"} {
		if _, err := exec.LookPath(n); err == nil {
			return true
		}
	}
	return false
}

// runBackend runs one script through sb and returns the output, failing on an
// infrastructure error.
func runBackend(t *testing.T, sb Sandbox, script string, lim Limits) string {
	t.Helper()
	out, err := sb.Run(context.Background(), script, lim)
	if err != nil {
		t.Fatalf("%s Run(%q) infra error: %v (output: %q)", sb.Name(), script, err, out)
	}
	return out
}

func TestNamespacesEcho(t *testing.T) {
	sb := requireNamespaces(t)
	out := runBackend(t, sb, "echo hello-sandbox", smallLimits())
	if !strings.Contains(out, "hello-sandbox") {
		t.Errorf("echo output = %q; want it to contain hello-sandbox", out)
	}
}

func TestNamespacesNoNetwork(t *testing.T) {
	sb := requireNamespaces(t)
	// An empty net namespace: even the loopback interface is down, so a connect
	// to any address must fail. We only assert the command did NOT succeed.
	out, err := sb.Run(context.Background(), "wget -T 2 -q -O- http://127.0.0.1:11434/ ; echo done-$?", smallLimits())
	if err != nil {
		t.Fatalf("infra error: %v", err)
	}
	if strings.Contains(out, "done-0") {
		t.Errorf("expected the network probe to fail in an empty netns; output = %q", out)
	}
}

func TestNamespacesRootReadOnly(t *testing.T) {
	sb := requireNamespaces(t)
	// / is read-only; /tmp is writable.
	out := runBackend(t, sb, "touch /oops 2>&1 || echo ro-root; touch /tmp/ok && echo tmp-ok", smallLimits())
	if !strings.Contains(out, "ro-root") {
		t.Errorf("expected write to / to fail; output = %q", out)
	}
	if !strings.Contains(out, "tmp-ok") {
		t.Errorf("expected write to /tmp to succeed; output = %q", out)
	}
}

func TestNamespacesTimeout(t *testing.T) {
	sb := requireNamespaces(t)
	lim := smallLimits()
	lim.Timeout = 1 * time.Second
	start := time.Now()
	out, err := sb.Run(context.Background(), "sleep 30; echo should-not-print", lim)
	if err != nil {
		t.Fatalf("infra error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("timeout did not fire promptly: took %s", elapsed)
	}
	if strings.Contains(out, "should-not-print") {
		t.Errorf("command outlived its timeout; output = %q", out)
	}
}

func TestRuntimeEcho(t *testing.T) {
	sb := requireRuntime(t)
	// First run may pull the image; give it room.
	lim := smallLimits()
	lim.Timeout = 60 * time.Second
	out := runBackend(t, sb, "echo hello-runtime", lim)
	if !strings.Contains(out, "hello-runtime") {
		t.Errorf("echo output = %q; want it to contain hello-runtime", out)
	}
}

func TestRuntimeNoNetwork(t *testing.T) {
	sb := requireRuntime(t)
	lim := smallLimits()
	lim.Timeout = 60 * time.Second
	out, err := sb.Run(context.Background(), "wget -T 3 -q -O- http://1.1.1.1/ ; echo done-$?", lim)
	if err != nil {
		t.Fatalf("infra error: %v", err)
	}
	if strings.Contains(out, "done-0") {
		t.Errorf("expected network to be blocked (--network none); output = %q", out)
	}
}

func TestFromEnvUnknown(t *testing.T) {
	t.Setenv("TALUNOR_SANDBOX", "bogus")
	if _, err := FromEnv(); err == nil {
		t.Error("FromEnv with an unknown backend should error")
	}
}

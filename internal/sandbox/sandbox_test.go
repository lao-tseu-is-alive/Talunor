package sandbox

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
// (TALUNOR_REQUIRE=docker). The availability question is "can this host run a
// container?", not "is there a CLI on PATH?" — a Rancher Desktop shim answers
// yes to the second while its VM is down, which used to fail these tests
// instead of skipping them.
func requireRuntime(t *testing.T) Sandbox {
	t.Helper()
	testenv.Require(t, testenv.CapDocker, runtimeAvailable(context.Background()))
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

// TestClassifyProbeSeparatesPresenceFromUsability pins the decision that used to
// be implicit in exec.LookPath: a runtime is available only when its daemon
// answers. Pure inputs, so it runs on a host with no container runtime at all.
func TestClassifyProbeSeparatesPresenceFromUsability(t *testing.T) {
	cases := []struct {
		name    string
		out     string
		runErr  error
		ctxErr  error
		wantErr bool
		wantSub string
	}{
		{name: "usable", out: "Client:\n Server Version: v2.3.2\n", wantErr: false},
		{
			name: "daemon down", out: "Rancher Desktop is not running. Please start Rancher Desktop to use nerdctl\n",
			runErr: errors.New("exit status 1"), wantErr: true, wantSub: "Rancher Desktop is not running",
		},
		{
			name: "docker socket missing",
			out: "Client:\n Version: 29.6.2-rd\n\nServer:\n" +
				"failed to connect to the docker API at unix:///var/run/docker.sock: no such file\n",
			runErr: errors.New("exit status 1"), wantErr: true, wantSub: "failed to connect",
		},
		{
			name: "hung daemon", ctxErr: context.DeadlineExceeded,
			runErr: context.DeadlineExceeded, wantErr: true, wantSub: "did not answer",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := classifyProbe("nerdctl", []byte(tc.out), tc.runErr, tc.ctxErr)
			if (err != nil) != tc.wantErr {
				t.Fatalf("classifyProbe = %v; wantErr %v", err, tc.wantErr)
			}
			if err != nil && !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("classifyProbe = %q; want it to mention %q", err, tc.wantSub)
			}
		})
	}
}

// TestProbeRuntimeReportsAFailingClient drives the exec path itself against a
// stub that behaves like a runtime CLI whose daemon is down: present, exits
// non-zero, explains why. No container runtime needed.
func TestProbeRuntimeReportsAFailingClient(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub is a shell script")
	}
	stub := filepath.Join(t.TempDir(), "fakectl")
	script := "#!/bin/sh\necho 'Rancher Desktop is not running. Please start it'\nexit 1\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	err := probeRuntime(context.Background(), stub, "fakectl")
	if err == nil {
		t.Fatal("probeRuntime succeeded against a stub whose daemon is down")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Errorf("probeRuntime error = %q; want the client's own explanation", err)
	}
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

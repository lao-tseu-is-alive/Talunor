//go:build linux

package sandbox

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// pipeWith returns the read end of a pipe already loaded with data, mimicking
// what the child inherits as childTokenFD.
func pipeWith(t *testing.T, data string) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	if _, err := w.WriteString(data); err != nil {
		t.Fatalf("write token: %v", err)
	}
	w.Close()
	return r
}

// validToken is a well-formed token: the length is part of the contract.
const validToken = "0123456789abcdef0123456789abcdef"

// TestVerifyChildIdentityAcceptsTheRealChild pins the happy path: pid 1 of the
// new pid namespace, plus the same token on the pipe and in the environment.
func TestVerifyChildIdentityAcceptsTheRealChild(t *testing.T) {
	if err := verifyChildIdentity(childInitPID, pipeWith(t, validToken), validToken); err != nil {
		t.Fatalf("the legitimate child must be accepted, got: %v", err)
	}
}

// TestVerifyChildIdentityRejectsImpostors covers every way a process can look
// like the sandbox child without being it. Each case must be rejected BEFORE
// childMain touches a mount.
func TestVerifyChildIdentityRejectsImpostors(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer devNull.Close()

	closed, _, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	closed.Close() // an fd that is no longer valid, like an un-inherited one

	cases := []struct {
		name  string
		pid   int
		fd    *os.File
		env   string
		wants string // substring the diagnostic must carry
	}{
		{
			// The common accident: someone exported the trigger in their shell.
			name: "ordinary process, not pid 1",
			pid:  4242, fd: pipeWith(t, validToken), env: validToken,
			wants: "not a sandbox child",
		},
		{
			// A container entrypoint IS pid 1, so only the token saves us there.
			name: "pid 1 but no token in the environment",
			pid:  childInitPID, fd: pipeWith(t, validToken), env: "",
			wants: envToken + " is missing",
		},
		{
			name: "pid 1 but malformed token in the environment",
			pid:  childInitPID, fd: pipeWith(t, validToken), env: "short",
			wants: envToken + " is missing",
		},
		{
			// Regression guard: an fd at EOF must not "match" an empty env var —
			// two empty strings are equal, which would wave the impostor through.
			name: "pid 1, empty token, fd at EOF",
			pid:  childInitPID, fd: devNull, env: "",
			wants: envToken + " is missing",
		},
		{
			// Anything that is not our pipe, even if readable: an fstat rejects it
			// rather than blocking on, say, an inherited socket.
			name: "pid 1, token in env, fd is not a pipe",
			pid:  childInitPID, fd: devNull, env: validToken,
			wants: "not a pipe",
		},
		{
			name: "pid 1, token in env, no inherited fd",
			pid:  childInitPID, fd: closed, env: validToken,
			wants: "fd 3",
		},
		{
			name: "pid 1, token in env, nothing on the pipe",
			pid:  childInitPID, fd: pipeWith(t, ""), env: validToken,
			wants: "read token",
		},
		{
			// The env var is forgeable; the pipe is not.
			name: "pid 1, token in env, different token on the pipe",
			pid:  childInitPID, fd: pipeWith(t, strings.Repeat("f", tokenHexLen)), env: validToken,
			wants: "token mismatch",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyChildIdentity(tc.pid, tc.fd, tc.env)
			if err == nil {
				t.Fatal("impostor accepted: childMain would have started mounting")
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("diagnostic = %q; want it to mention %q", err, tc.wants)
			}
		})
	}
}

// TestChildTriggerFromAmbientEnvExitsBeforeMounting is the end-to-end version of
// the above, and the reason the guard exists at all: init() hijacks EVERY binary
// linking this package, this test binary included. It re-runs itself with
// TALUNOR_SANDBOX_CHILD=1 (and no inherited token pipe) and requires a clean
// diagnostic exit rather than an attempt to mount over the host's root.
//
// -test.run=^$ keeps the child from re-entering this test if the guard ever
// regresses: a hijacked child that fell through would run no tests, not fork.
func TestChildTriggerFromAmbientEnvExitsBeforeMounting(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(),
		envChild+"=1",
		envScript+"=echo this-must-never-run",
		envRootfs+"=/", // the worst case: a rootfs that really exists
	)
	// Deliberately no ExtraFiles: an accidental trigger has no token pipe.
	out, err := cmd.CombinedOutput()

	exitErr, ok := errors.AsType[*exec.ExitError](err)
	if !ok {
		t.Fatalf("expected the hijacked child to exit %d, got err=%v (output: %s)", childFailExit, err, out)
	}
	if code := exitErr.ExitCode(); code != childFailExit {
		t.Errorf("exit code = %d; want %d (output: %s)", code, childFailExit, out)
	}
	if !strings.Contains(string(out), "sandbox child:") {
		t.Errorf("want the guard's diagnostic, got: %s", out)
	}
	// The failure must come from the guard, not from a half-done setup.
	for _, leaked := range []string{"make / private", "pivot_root", "this-must-never-run"} {
		if strings.Contains(string(out), leaked) {
			t.Errorf("child got past the guard (%q in output): %s", leaked, out)
		}
	}
}

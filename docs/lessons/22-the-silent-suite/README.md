# Lesson 22 — The silent suite: a skipped test is not a passing test

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🔍 Historical exploration** (the hole at `v0.20.1`, the fix at `v0.20.2`) ·
Level 3 (advanced) · ~70 min

## Why this lesson exists

Talunor's sandbox has a well-documented trick: to run a script in isolation it
**re-executes its own binary** (`/proc/self/exe`) and lets an `init()` hook turn that
child into a container init. Lesson 10 taught you that trick. What Lesson 10 did not
say is that the hook is armed by an *environment variable* — and an environment
variable is inherited by every descendant of whoever exported it.

A reviewer noticed. A patch was written. The patch was well-reasoned, well-commented,
and **it disabled the sandbox completely** — while the test suite stayed green.

That last clause is the lesson. Not "AI writes bugs" (Lesson 15 already covers
verifying what a machine claims about your code), but something that applies to every
patch you will ever review, human or not: **a test suite tells you what it ran, and it
is under no obligation to tell you what it didn't.** Four tests in this very package
had been quietly skipping for weeks.

## Learning objectives

By the end you can:
- measure a defect's *actual* severity before fixing it, instead of the severity its
  report claims;
- explain what authenticates a re-executed child process — and why an env var alone
  never can;
- name three failure modes in the "obvious" implementation: an fd closed before
  `exec.Cmd` dups it, a comparison two empty strings satisfy, and an unbounded read on
  a file descriptor you did not open;
- tell a **compile-time** guard (`//go:build`) from a **runtime** one
  (`if runtime.GOOS != "linux"`) and say which one a test needs;
- audit your own suite for silent skips, and design a test so the important decision
  stays verifiable on a host that cannot run the feature.

## Prerequisites

- **Lesson 10 (the sandbox)** — the `/proc/self/exe` re-exec, the two backends.
- **Lesson 07 (test without a real LLM)** — fakes and determinism; this lesson is its
  uncomfortable sequel.
- Helpful: **Lesson 15**, for the reflex of verifying a claim before acting on it.

## Part 1 — verify the finding before you fix it

The report read: *"if a user accidentally had `TALUNOR_SANDBOX_CHILD=1` in their shell,
any binary importing `internal/sandbox` would call `childMain()` in `init()` and attempt
`pivot_root` on the host."*

Half of that is true. Find out which half — from `main`, no privileges needed:

```bash
go test -c -o /tmp/sandbox.test ./internal/sandbox/
TALUNOR_SANDBOX_CHILD=1 /tmp/sandbox.test -test.run TestFromEnvUnknown ; echo "exit=$?"
```

At `v0.20.1` this prints:

```
sandbox child: make / private: operation not permitted
exit=127
```

Read that carefully, because it settles the severity:

- **Confirmed:** the hijack is real, and it reaches *every* binary linking the package
  — `cmd/talunor` (via `internal/tools`), and each test binary. The process exits 127
  **before `main()` runs**, blaming a mount the user never asked for.
- **Overstated:** it never gets near `pivot_root`. `childMain` dies at the *first*
  syscall of `setupRoot` — `mount(MS_REC|MS_PRIVATE, "/")` — with `EPERM`, because an
  ordinary process has no `CAP_SYS_ADMIN` in its mount namespace.

So this is a **footgun / self-DoS**, not a route to trashing the host's mounts. Worth
fixing — a diagnostic that says "operation not permitted" when the real problem is a
stray variable costs somebody an afternoon — but worth fixing *at the right priority*,
with the right words in the changelog. Measuring first is not pedantry: it is what
keeps a security note honest.

## Part 2 — the proposed patch (spot three defects)

The fix's *design* was right, and worth learning in its own right. Two independent
facts should gate the child:

1. **pid == 1.** The real child is pid 1 of its own `CLONE_NEWPID` namespace.
2. **A per-run secret.** The parent draws a random token, sends it through a **pipe**
   inherited as fd 3, and repeats it in an env var. An attacker can export the env var;
   they cannot write our pipe.

Here is the parent side as proposed. **Before reading on, look for what breaks it:**

```go
tokenR, tokenW, err := os.Pipe()
cmd := exec.CommandContext(runCtx, "/proc/self/exe")
cmd.ExtraFiles = append(cmd.ExtraFiles, tokenR)
cmd.Env = append(os.Environ(), envChild+"=1", envToken+"="+tokenStr /* … */)

_, _ = tokenW.WriteString(tokenStr)
tokenW.Close()
tokenR.Close()          // "the parent never reads from the pipe"
err = cmd.Run()
```

and the child side:

```go
if os.Getpid() != 1 {
    die(...)
}
tokenFD := os.NewFile(3, "sandbox-token")
tokenBytes, err := io.ReadAll(tokenFD)
if err != nil {
    die(...)
}
if string(tokenBytes) != os.Getenv(envToken) {
    die(errors.New("token mismatch"))
}
```

Three defects. Take a minute before Part 3.

## Part 3 — defect 1: `ExtraFiles` is dup'd at `Start`, not at assignment

`cmd.ExtraFiles = append(…, tokenR)` does not hand anything to anyone. `os/exec` only
duplicates those descriptors into the child when the process is **started** — inside
`cmd.Run()`. Closing `tokenR` on the line before therefore closes the fd the child was
supposed to inherit.

Prove it in twenty lines, outside the project (no cgo, no repo, nothing to install):

```bash
mkdir /tmp/fdlab && cd /tmp/fdlab && go mod init fdlab
cat > main.go <<'EOF'
package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
)

func main() {
	if os.Getenv("CHILD") == "1" {
		got := make([]byte, 8)
		_, err := io.ReadFull(os.NewFile(3, "token"), got)
		fmt.Printf("child: got=%q err=%v\n", got, err)
		return
	}
	r, w, _ := os.Pipe()
	w.WriteString("TOKEN123")
	w.Close()
	cmd := exec.Command(os.Args[0])
	cmd.ExtraFiles = []*os.File{r}
	cmd.Env = append(os.Environ(), "CHILD=1")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	r.Close()                    // ← the bug: move this line below cmd.Run()
	fmt.Println("parent:", cmd.Run())
}
EOF
go run .
```

```
child: got="\x00\x00\x00\x00\x00\x00\x00\x00" err=read token: bad file descriptor
```

Now move `r.Close()` after `cmd.Run()` and re-run: `got="TOKEN123"`. The correct
lifecycle is asymmetric and worth memorising — the **write** end closes *early* (so the
child sees EOF after the token), the **read** end closes *late* (so `Start` can dup it):

```go
defer tokenR.Close()                  // after Run, via defer
if _, err := tokenW.WriteString(token); err != nil { /* … */ }
if err := tokenW.Close(); err != nil { /* … */ }   // before Run
```

In Talunor this defect is not cosmetic: every legitimate sandbox run fails the token
check with `bad file descriptor`, so `bash` is dead. The parent sees `cmd.Run()` return
a plain exit-127 error and reports it as the *script's* output, which is exactly the
kind of failure a hurried reader calls "the sandbox is broken on this host."

## Part 4 — defect 2: two empty strings are equal

```go
if string(tokenBytes) != os.Getenv(envToken) { die(...) }
```

Suppose fd 3 is open but yields EOF immediately — `/dev/null`, or a pipe someone already
drained. `io.ReadAll` returns `""`. Suppose `TALUNOR_SANDBOX_TOKEN` is not set:
`os.Getenv` returns `""`. `"" == ""`, and the impostor walks through the gate built to
stop it. The second guard is *vacuous* in precisely the situation it exists for.

The rule generalises well beyond this file:

> **Authenticate the shape before comparing the value.** A secret that is absent, empty,
> or the wrong length must be rejected *as malformed* — never handed to an equality test
> that an attacker can satisfy by supplying nothing at all.

Read the fixed version (`verifyChildIdentity`, `internal/sandbox/namespaces_linux.go`):
the length check comes first, and the comparison is `subtle.ConstantTimeCompare`.

```go
if len(envValue) != tokenHexLen {
    return fmt.Errorf("%s is missing or malformed: not a sandbox child", envToken)
}
```

Its regression test is the case that would otherwise have shipped:

```go
{
    name: "pid 1, empty token, fd at EOF",
    pid:  childInitPID, fd: devNull, env: "",
    wants: envToken + " is missing",
},
```

## Part 5 — defect 3: you do not own fd 3

`io.ReadAll(os.NewFile(3, …))` reads **whatever fd 3 happens to be** in a process that
never expected this trick. A supervisor, a shell, a language runtime — anything may have
left something there. Two bad outcomes:

- fd 3 is a regular file or socket: the guard *consumes* data belonging to somebody else;
- fd 3 is a pipe or socket with a live writer: `ReadAll` blocks **forever**, inside
  `init()`, where no logger is configured and no timeout exists. The binary hangs before
  `main()` with no output at all. That is strictly worse than the bug being fixed.

Is the pid check enough to make this unreachable? Almost — and "almost" is why the
detail matters: **Talunor is pid 1 inside its own Docker image.** There, guard 1 passes
and everything rests on guard 2.

The fix states what it is willing to read, then reads exactly that much:

```go
var st unix.Stat_t
if err := unix.Fstat(int(tokenFD.Fd()), &st); err != nil { /* … */ }
if st.Mode&unix.S_IFMT != unix.S_IFIFO {
    return fmt.Errorf("fd %d is not a pipe: not a sandbox child", childTokenFD)
}
got := make([]byte, tokenHexLen)
if _, err := io.ReadFull(tokenFD, got); err != nil { /* … */ }
```

### A fourth one, free: runtime `if` ≠ compile-time guard

The patch's test opened with `if runtime.GOOS != "linux" { t.Skip(...) }` and then used
`envChild` — a constant that only exists in `namespaces_linux.go`. A `t.Skip` runs *after*
compilation; the file still has to build everywhere:

```bash
GOOS=darwin go vet ./internal/sandbox/
# vet: sandbox_test.go:170:3: undefined: envChild
```

`namespaces_other.go` exists precisely so this package keeps compiling off Linux. The fix
puts the new tests in `namespaces_linux_test.go` with `//go:build linux` at the top.
**A runtime check protects execution; only a build tag protects compilation.**

## Part 6 — the real question: why did nobody notice?

Three defects, one of them fatal to the feature, and:

```bash
go test ./internal/sandbox/
ok  	github.com/lao-tseu-is-alive/Talunor/internal/sandbox
```

Ask the suite what it actually ran:

```bash
go test ./internal/sandbox/ -v -run Namespaces 2>&1 | grep -E '^(---|\s+---)'
```

On most hosts, today:

```
--- SKIP: TestNamespacesEcho (0.00s)
--- SKIP: TestNamespacesNoNetwork (0.00s)
--- SKIP: TestNamespacesRootReadOnly (0.00s)
--- SKIP: TestNamespacesTimeout (0.00s)
```

Every test that exercises the real backend was skipping, because Ubuntu re-applies

```bash
cat /proc/sys/kernel/apparmor_restrict_unprivileged_userns   # 1 = the backend cannot run
```

across updates (Lesson 10 and gotcha 14 in `AGENTS.md` warn about this sysctl; nobody
warned that its return would *silence the tests*). The skip is correct behaviour — a
contributor without user namespaces should not get a red suite. The failure is that
`ok` and `ok` look identical whether four tests ran or none did.

> **The core idea.** `t.Skip` converts "I cannot check this" into "nothing to report."
> That is a reasonable default for portability and a terrible default for trust. The
> environment your tests need is part of your test suite's *contract*, and nothing
> enforces it by default.

### The design answer: make the decision testable without the privilege

You cannot make user namespaces appear on every laptop. You *can* stop the interesting
decision from depending on them. That is why the fix does not simply add an `if` inside
`childMain` — a function that can only ever `exec` or `os.Exit`, i.e. one no test can
call. It extracts the judgement into an ordinary function:

```go
func verifyChildIdentity(pid int, tokenFD *os.File, envValue string) error
```

`pid` is a parameter, not `os.Getpid()`. The fd is a parameter, not fd 3. The env value
is a parameter, not `os.Getenv`. Now the whole decision is a table test over plain
`os.Pipe()`s, plus one subprocess test that re-runs the test binary with the ambient
trigger — **neither needs root, user namespaces, or a container**:

```bash
go test ./internal/sandbox/ -v -run 'VerifyChild|ChildTrigger'
```

Nine cases, none skipped, on any Linux host and on any CI runner. The part that still
needs privileges (mount, pivot_root, rlimits) stays where it was; the part that decides
*whether to do any of it* no longer does.

Note the subprocess test's small precaution — it launches the child with
`-test.run=^$`. If the guard ever regresses, the hijacked child runs *no* tests instead
of re-entering this one and forking indefinitely. **When a test spawns your own test
binary, assume the thing you are testing is broken.**

## Hands-on — make the sysctl decide your verdict

The point lands when the same code passes and fails depending on nothing but a kernel
setting. On a Linux host where you can use `sudo`:

```bash
# 1. See where you stand.
cat /proc/sys/kernel/apparmor_restrict_unprivileged_userns

# 2. Break the fix on purpose: in internal/sandbox/namespaces_linux.go, replace
#    `defer tokenR.Close()` with a plain `tokenR.Close()` on the line before `cmd.Run()`
#    — the exact defect from Part 3.

# 3. With the restriction ON (sysctl = 1):
go test ./internal/sandbox/          # => ok. The sandbox is dead and the suite is happy.

# 4. Lift it, and ask the same question again:
scripts/allow-unprivileged-userns.sh
go test ./internal/sandbox/ -run Namespaces -v
#  --- FAIL: TestNamespacesEcho
#      echo output = "sandbox child: cannot read token from fd 3: read sandbox-token:
#      bad file descriptor\n\n[exit status 127]"

# 5. Restore the `defer`, confirm green, and put the sysctl back if you prefer:
#    scripts/allow-unprivileged-userns.sh --restore
```

Step 3 is the one to sit with. Nothing about your code changed between step 3 and step
4. Your *evidence* changed.

## What Talunor shipped because of this lesson

Principle 7 below is a habit, and habits decay. So `v0.20.4` put it in the build
(`internal/testenv`): a host can **declare** what it must be able to exercise.

```bash
export TALUNOR_REQUIRE=all        # this machine must run ext + sandbox + docker tests
```

Every capability skip now goes through one call — `testenv.Require(t, cap, err)` — which
skips as before on a machine that lacks the capability, and **fails** on a machine that
declared it. Nothing changes for a contributor; on the maintainer's box the reverted
sysctl now stops the release instead of shrinking the suite:

```
--- FAIL: TestRuntimeEcho
    TALUNOR_REQUIRE=docker declares this host must be able to exercise "docker",
    but it cannot: no nerdctl/docker on PATH
```

And `make capabilities` (printed first by `make release-check`) states the ground truth
before any test runs:

```
==> capabilities: ext=yes sandbox=yes docker=yes | TALUNOR_REQUIRE=all
```

Note what this is *not*: it does not audit skips, count them, or diff them against a
baseline. Printing a list you already ignored changes nothing. The declaration works
because it encodes something only you know — *what this particular machine is supposed to
be able to do* — and turns the gap between that and reality into a red test.

## The principles

```text
A skipped test is not a passing test — and `ok` will never tell you which one you got.
```

1. **Verify the finding, then size it.** "Attempts `pivot_root` on the host" and "dies
   at the first `mount` with EPERM" deserve different priorities and different words.
2. **Authenticate the shape before the value.** Absent, empty, or wrong-length is a
   rejection — not an input to `==`.
3. **Read only what you own.** An inherited fd is untyped and untrusted: `fstat` it,
   bound the read, never `ReadAll` it in `init()`.
4. **Know when your fds are dup'd.** `exec.Cmd` copies `ExtraFiles` at `Start`; write
   end early, read end late.
5. **Build tags guard compilation; `t.Skip` guards execution.** They are not
   interchangeable.
6. **Push the decision out of the privileged code.** A pure function taking pid, fd and
   secret as parameters is testable everywhere; a `childMain` that only ever `exec`s is
   testable nowhere.
7. **Audit your skips.** `go test ./... -v | grep SKIP` is a five-second habit that tells
   you what your green run is actually worth.

## Completion checklist

- [ ] I reproduced the ambient-trigger hijack and can state its real severity.
- [ ] I ran the `/tmp/fdlab` experiment and saw `bad file descriptor` flip to `TOKEN123`
      by moving one line.
- [ ] I can explain why `"" == ""` defeated the token guard, and what `verifyChildIdentity`
      checks before comparing.
- [ ] I can say what goes wrong when `io.ReadAll` meets an fd 3 it does not own.
- [ ] I ran `GOOS=darwin go vet ./internal/sandbox/` and understand why a `t.Skip` did not
      save that test file.
- [ ] I listed my own suite's skips and know which ones hide a real capability gap.
- [ ] I did the sysctl hands-on and watched identical code pass, then fail.
- [ ] I returned to `main` with the `defer` restored.

---

## 🎓 About this lesson

This is the course's third post-mortem (after Lessons 11 and 14) and its first about a
**fix** rather than a feature. The patch it dissects was machine-generated, which is how
the episode started, but that framing is deliberately not the moral: a careful human
writing the same code would have hit defect 1 identically, and the green suite would have
lied to them just as convincingly. Lesson 15 taught you to verify what a review *claims*;
this one asks the harder question — **what does your evidence actually cover?** Talunor's
answer, in one commit: measure the defect before fixing it, extract the decision from the
privileged code, and never mistake `ok` for a checked box.

Back to the [course index](../).

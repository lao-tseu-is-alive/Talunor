# Lesson 10 — Understand the sandbox

**Language:** 🇬🇧 English · [🇫🇷 Français](README.fr.md)

**🔍 Historical exploration** · Level 4 · **Advanced** · ~90 min

> **The capstone.** The most advanced lesson — it touches Linux internals. The goal
> is *not* to turn you into a kernel expert in 90 minutes; it's to understand the
> **levels of isolation** and, above all, an engineering value: **never claim more
> safety than you actually have.**

## Why this lesson exists

Talunor's `bash` tool can run *any* shell command the model asks for. That's
powerful and dangerous, so it runs inside a **sandbox**. But "sandbox" is not one
thing — it's a spectrum. Talunor ships **two** backends and is scrupulously honest
about how strong each one is. That honesty is the real lesson. Read it at
**`v0.9.0`**, where the sandbox arrives.

## Learning objectives

By the end you can:
- name the isolation primitives a container is built from;
- explain the difference between the two backends and when to use each;
- explain why one is labelled "teaching artifact, not a strong boundary" — and why
  saying so is a feature, not a weakness.

## Prerequisites

- Lessons 00–06. This is the deep end; skim what's unfamiliar.

## Check out the sandbox layer

```bash
git checkout v0.9.0     # detached HEAD — read only (see Lesson 00)
```

> **Files at this tag** (the isolation layer for the `bash` tool):
>
> ```text
> internal/sandbox/sandbox.go          the Sandbox interface + Limits + FromEnv
> internal/sandbox/runtime.go          the OCI backend (nerdctl/docker) — the strong one
> internal/sandbox/namespaces_linux.go the rootless namespaces backend — the teaching one
> internal/sandbox/rootfs_linux.go     builds the busybox rootfs the container runs in
> internal/sandbox/namespaces_other.go stubs for non-Linux (so it still compiles)
> ```

Read in this order — interface first, then the two implementations:

```text
internal/sandbox/sandbox.go          # the small contract: Run(script, Limits)
internal/sandbox/runtime.go          # ociRuntime — delegates to a real runtime
internal/sandbox/namespaces_linux.go # the hand-rolled, rootless one (read the comments!)
```

## Two backends, honestly compared

| Property | `ociRuntime` (nerdctl/docker) | `namespaces` (rootless, hand-rolled) |
|----------|-------------------------------|--------------------------------------|
| External dependency | Yes (a container runtime) | No — pure Go + the kernel |
| Isolation strength | **Strong** | **Limited** |
| seccomp (syscall filter) | Yes (from the runtime) | **No** |
| Best for | genuinely untrusted code | *learning what a runtime does* |

The `namespaces` backend re-executes Talunor's own binary as a container "init" in
fresh **user / mount / pid / net** namespaces, `pivot_root`s into a read-only
busybox filesystem, drops capabilities, sets `no_new_privs`, and gives it an empty
network namespace (so: no network). It *looks* like a container — because it is
doing, by hand, what a runtime does for you.

## The heart of the lesson: honest boundaries

Find the comment in `namespaces_linux.go` that says, in effect, *"there is no
seccomp filter, so the full Linux syscall surface is reachable — this is defense in
depth and a teaching artifact, not a boundary for hostile code."*

Sit with that. The author built an impressive-looking sandbox **and then told you
not to trust it for real threats.** That is the opposite of most security theatre.

> A complex mechanism must never be presented as a stronger guarantee than it
> actually is. Naming the limit (no seccomp → not a real boundary) is what turns a
> demo into trustworthy engineering. When it matters, the code says: use the OCI
> backend.

This is the single most important idea in the whole course: **the value of a
guardrail is inseparable from an honest account of where it stops.**

## Experiment

Compare the two paths by reading, and (optionally) run the sandbox's own tests:

```bash
go test ./internal/sandbox/ -v   # some cases skip if the host can't provide a backend
```

Running the `bash` tool for real needs setup (`TALUNOR_BASH=1`, a runtime or
unprivileged user namespaces; on Ubuntu 24.04 an AppArmor toggle — see the
`scripts/` helper and `README.md` on `main`). If your host allows it:

```bash
git switch main
TALUNOR_BASH=1 go run ./cmd/talunor --plain
# ask it to run: id ; pwd ; ls /   — observe how little of the host is visible
```

Then return to the latest code:

```bash
git switch main
```

## Questions to answer

- Name three namespaces the sandbox uses and what each one hides.
- Why does an empty network namespace mean "no network"?
- Why is the `namespaces` backend *not* recommended for genuinely untrusted code,
  and what should you use instead?
- Why is documenting a weakness a sign of *good* security work, not bad?

## Common mistakes

- **Reading `namespaces_linux.go` first.** Start with the interface and the OCI
  backend; the hand-rolled one makes sense only once you know what it's imitating.
- **Believing the demo is bulletproof** because it looks like a container. The code
  itself tells you otherwise — believe the code.

## Completion checklist

- [ ] I can name the two backends and when to use each.
- [ ] I can list a few isolation primitives (user/mount/pid/net ns, pivot_root, caps).
- [ ] I found the "no seccomp / teaching artifact" comment and can explain why that
      honesty matters.
- [ ] I can state the capstone idea: a guardrail's worth includes an honest account
      of its limits.
- [ ] I returned to `main`.

---

## 🎓 You've finished the course

You walked Talunor from a bare memory store to a full agent with tools, tests, and
honest safety. You can now:

1. run it; 2. explain its architecture; 3. follow one agent turn; 4. add a tool;
5. write a deterministic test; 6. reason about its security limits; 7. justify at
least one design trade-off.

Talunor is no longer just *intended* as a teaching project — for you, it has become
a practical course in **Go, AI agents, testability, and safe-by-design code**.

Back to the [course index](../).

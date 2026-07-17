# Lesson 09 — Secure web fetching (SSRF)

**🔍 Historical exploration** · Level 3 · **Advanced** · ~75 min

> **Advanced security lesson.** It rewards Lessons 04–05. Take your time.

## Why this lesson exists

Give an agent the power to fetch a URL and you've opened a door: an attacker can
ask it to fetch a URL that points **inward** — your database, a neighbour's admin
panel, or the cloud metadata endpoint that hands out credentials. This is **SSRF**
(Server-Side Request Forgery). Talunor's `web_fetch` tool is built to refuse those
destinations, and *how* it refuses them is a genuinely good piece of security
engineering. You'll read it at **`v0.10.0`**, where it first appears.

## Learning objectives

By the end you can:
- explain what SSRF is and why a URL allowlist alone isn't enough;
- explain why checking the IP **at connect time** beats resolving-then-checking;
- read a security guard written as a *pure, table-tested function*.

## Prerequisites

- Lessons 04–05. Comfort reading Go.

## Check out the web_fetch layer

```bash
git checkout v0.10.0     # detached HEAD — read only (see Lesson 00)
```

> **Files at this tag** (the network opt-in tool):
>
> ```text
> internal/webfetch/webfetch.go       the guarded fetcher: blockedIP, guardDial, Fetch
> internal/webfetch/webfetch_test.go  the SSRF classifier table + the redirect test
> internal/tools/webfetch.go          the tool wrapper (schema, allowlist, approval)
> ```

Read, in this order:

```text
internal/webfetch/webfetch.go       # blockedIP → guardDial → checkRedirect → Fetch
internal/webfetch/webfetch_test.go  # TestBlockedIP, TestFetchRedirectToInternalBlocked
```

## The idea in three levels

**Intuition.** Your agent can fetch a web page. An attacker gives it a URL pointing
*inside* your network — a private service, or `169.254.169.254`, the cloud address
that vends credentials. You must refuse those.

**Technique.** The naïve guard is: resolve the hostname to an IP, check the IP,
then connect. But between the *check* and the *connect*, a hostile DNS server can
change its answer to an internal IP — **DNS rebinding**. The fix: check the IP **at
the exact moment of connecting**, on the IP that is actually dialled — and do it on
every redirect hop, since a public URL can `302` you to an internal one.

**In Talunor.** The check lives in the dialer's `Control` hook — read `guardDial`:
it receives the resolved `ip:port` *right before* `connect()`, so the IP vetted is
the IP dialled, always. The decision itself is a **pure function**, `blockedIP`,
which refuses loopback, private (RFC1918), link-local (including
`169.254.169.254`), CGNAT, and more — and **fails closed** on anything it can't
classify. Because it's pure, it's tested exhaustively in a table (`TestBlockedIP`)
with no network at all.

## The killer test

Open `TestFetchRedirectToInternalBlocked`. It stands up a normal (loopback) test
server that responds with a redirect to `http://169.254.169.254/…`, and asserts the
fetch is **refused**. That proves the guard re-checks *after* a redirect — the
classic SSRF bypass — not just on the first request.

## Experiment

The guard is testable with no network, which is the whole point:

```bash
go test ./internal/webfetch/ -v
```

Watch `TestBlockedIP` walk a table of addresses (public → allowed, internal →
blocked) and `TestFetchRedirectToInternalBlocked` prove the redirect case. Then
read a couple of rows of the table and predict the result before you see it.

Return to the latest code when done:

```bash
git switch main
```

## Optional 🛠️ extension (on `main`)

The guard blocks `0.0.0.0` exactly, but not all of `0.0.0.0/8` (the "this network"
range, which can behave like loopback on some systems). As a small hardening,
branch from `main` and add `0.0.0.0/8` to the blocked CIDRs, with a new row in
`TestBlockedIP`:

```bash
git switch main && git pull && git switch -c learning/harden-blockedip
# then: add "0.0.0.0/8" to the blocked ranges + a test case
go test ./internal/webfetch/ -run BlockedIP -v
```

This is defense in depth: a *pure function* is the easiest place in a codebase to
add a rule and prove it.

## Questions to answer

- Why is a domain **allowlist** not a complete SSRF defence on its own? (Hint: what
  can a redirect, or a hostile DNS record for an allowlisted domain, do?)
- Why is `blockedIP` a pure function rather than a method that opens a connection?
- What does "fail closed" mean here, and why is it the right default for a guard?

## Common mistakes

- **Confusing the allowlist with the guard.** In `tools/webfetch.go`, an allowlist
  can skip the *approval prompt* — but it never skips the IP guard. An "allowed"
  host that resolves to an internal IP is still refused.
- **Thinking IP filtering is the whole story.** The *timing* (check at connect,
  re-check per redirect) matters as much as the list.

## Completion checklist

- [ ] I can explain SSRF and give one real target (e.g. the metadata endpoint).
- [ ] I can explain why check-at-connect-time defeats DNS rebinding.
- [ ] I read `blockedIP` and `guardDial` and can say how they fit together.
- [ ] I ran the webfetch tests and understood the redirect test.
- [ ] I returned to `main`.

**Next:** [Lesson 10 — Understand the sandbox](../10-understand-the-sandbox/) — the
course's capstone.

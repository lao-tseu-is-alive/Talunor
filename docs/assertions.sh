#!/bin/sh
# assertions.sh — the executable half of the reference docs.
#
# WHY THIS EXISTS
#   `docs/architecture.md` is billed as the project's mental model, and for three
#   releases it described a trust model the project had already replaced: §3.2 said
#   authority ranked "user_stated > model_inferred, a verified tool_observed above
#   both" — a LINEAR RANK, superseded by ADR 0004 in v0.22.0, where authority became
#   a function of (provenance, subject).
#
#   Nothing caught it. `atlas-check` verifies that every tracked file is *listed*.
#   `readme-check` pins one version string. `lessons-check` guards links and tags.
#   `lessons-assert` re-derives claims — but only for docs/lessons/. The reference
#   docs, the ones a newcomer reads first, had no alarm at all.
#
#   It surfaced by accident: a generated video overview, built from those docs,
#   narrated the stale claim back. A summariser has no loyalty to what you meant.
#
# THE TWO KINDS OF CHECK HERE
#   1. CLAIM → CODE. The doc says the code does X; re-derive X from the source.
#      Same idea as lessons-assert.
#   2. CODE → DOC. The reverse direction, and the one that would actually have
#      caught the bug above: architecture.md had ZERO mention of `subject` while
#      subject.go and ADR 0004 existed. A decision the code has taken and the mental
#      model has never heard of is exactly how a doc goes quietly out of date.
#      Concretely: every accepted ADR must be referenced by architecture.md.
#
# Rules for adding one:
#   - assert what the DOC PROMISES, not everything the code does — this is not a
#     second test suite;
#   - keep every check offline and cheap (grep/test, no build, no network);
#   - name the section and the claim, so a failure says which paragraph to fix;
#   - when a check fails, fix BOTH languages: architecture.md and architecture.fr.md.
#
# Usage: docs/assertions.sh      (from the repo root; make docs-assert)

set -u
cd "$(dirname "$0")/.." || exit 1

fail=0

# assert <label> <shell expression that must succeed>
assert() {
	if sh -c "$2" >/dev/null 2>&1; then
		printf '  ok   %s\n' "$1"
	else
		printf '  FAIL %s\n         expected to succeed: %s\n' "$1" "$2"
		fail=1
	fi
}

EN=docs/architecture.md
FR=docs/architecture.fr.md

echo "==> architecture.md — every accepted decision is in the mental model"

# CODE -> DOC. This is the direction that was missing. ADR 0004 existed for three
# releases while architecture.md described the model it replaced; listing the ADRs
# and requiring each to be cited is the cheapest possible guard against a repeat.
for adr in docs/decisions/[0-9]*.md; do
	base=$(basename "$adr")
	num=$(echo "$base" | cut -c1-4)
	# A superseded/rejected ADR need not be cited; an accepted one must be.
	if grep -qi '^- \*\*Status:\*\* *Accepted' "$adr"; then
		assert "ADR $num is referenced by architecture.md" \
			"grep -q '$base' $EN"
		assert "ADR $num is referenced by architecture.fr.md" \
			"grep -q '$base' $FR"
	fi
done

echo "==> architecture.md — the load-bearing claims, re-derived from the source"

# §3.1 — "memory.Store pins the pool to a single connection".
assert "3.1 the connection really is pinned to one" \
	'grep -q "SetMaxOpenConns(1)" internal/memory/store.go'
# §3.1 says the single connection buys DATABASE serialisation only, and that
# in-process state still needs atomics/mutexes. A generated summary flattened that
# into "no application-level locking"; the doc now states the scope, so the scope
# has to remain true.
assert "3.1 in-process state still uses atomics and a mutex" \
	'grep -q "atomic.Bool" internal/agent/agent.go && grep -q "sync.RWMutex" internal/agent/agent.go'

# §3.2 — confidence is system-assigned, never self-reported.
assert "3.2 confidence is assigned from provenance, not asked of the model" \
	'grep -q "func BaseConfidence(p Provenance)" internal/memory/memory.go'

# §3.3 — authority is (provenance, subject), and the SUBJECT is checked FIRST.
# This is the exact claim that was wrong in the doc for three releases.
assert "3.3 Supersedes takes Attributions, not bare provenances" \
	'grep -q "func Supersedes(newer, older Attribution)" internal/memory/supersede.go'
assert "3.3 the subject is checked BEFORE authority" \
	'sed -n "/^func Supersedes/,/^}/p" internal/memory/supersede.go | head -3 | grep -q "SameSubject"'
assert "3.3 user_stated about the WORLD still scores 0" \
	'sed -n "/^func supersedeAuthority/,/^}/p" internal/memory/supersede.go | grep -q "SubjectWorld"'
assert "3.3 contested is DERIVED, not a stored column" \
	'grep -q "func contestedExpr" internal/memory/memory.go'
assert "3.3 no migration adds a stored contested/status column" \
	'! grep -nE "ADD COLUMN (contested|status)" internal/memory/migrate.go'

# §3.4 — the evidence trail has two sides, and tool_observed is an UNCLAIMED tier.
assert "3.4 the trail records contradictions, not only support" \
	'grep -q "PolarityContradicts" internal/memory/evidence.go'
assert "3.4 no builtin tool claims Verified (the doc says the seam is unfilled)" \
	'! grep -rn ") Verified() bool" internal/ --include=*.go | grep -v _test | grep -q .'

# §3.5 — retention is computed at read time, base-2 half-life.
assert "3.5 decay is lazy and base-2 (salience * 2^(-age/half-life))" \
	'grep -q "math.Exp2" internal/memory/salience.go'

# §3.6 — every action crosses a fail-closed gate that can re-prompt on live args.
assert "3.6 the policy gate is consulted before a tool runs" \
	'sed -n "/func (a \*Agent) runTool/,/^}/p" internal/agent/tools.go | grep -q "a.policy.Evaluate"'
assert "3.6 a high-risk step re-confirms with live arguments" \
	'grep -q "reapproveAtOrAbove" internal/agent/tools.go'

# §3.7 — the guard table. Its whole point is that the two bash backends differ, so
# both halves must stay true: the OCI one really drops capabilities, and the
# namespaces one really has no seccomp (and says so).
assert "3.7 the OCI backend drops all capabilities" \
	'grep -q "cap-drop=ALL" internal/sandbox/runtime.go'
assert "3.7 the namespaces backend still documents having no seccomp" \
	'grep -qi "seccomp" internal/sandbox/sandbox.go'
assert "3.7 the SSRF check lives in the dialer Control hook" \
	'grep -q "Control:" internal/webfetch/*.go'
assert "3.7 recalled memory is fenced as untrusted DATA" \
	'grep -q "untrusted DATA" internal/agent/turn.go'

# §3.8 — learning runs off the critical path.
assert "3.8 the turn hands learning to a background worker" \
	'grep -q "func (a \*Agent) enqueueReflect" internal/agent/learn.go'

echo "==> status lines agree with what actually shipped"

# CODE -> DOC again, and the reason this section exists: docs-assert shipped in
# v0.23.6 and a review found two status drifts the next day. The README's layer table
# stopped at Layer 23 while Layer 24 had shipped in v0.23.0, and the lessons index
# said "00–24" while listing 25. Both are the same shape as the ADR gap — a document
# that has not heard of something the repository already did — and neither was
# guarded, because a status line makes no claim a code check can re-derive.
#
# So compare the documents against the ARTEFACTS: the directory listing, and the
# roadmap's own record of what is done.
assert "the lessons index status line matches the lessons on disk" \
	'last=$(ls -d docs/lessons/[0-9]*/ | sed "s|.*/\([0-9][0-9]\)-.*|\1|" | sort -n | tail -1);
	 grep -q "Lessons 00–$last are ready" docs/lessons/README.md &&
	 grep -q "leçons 00–$last sont prêtes" docs/lessons/README.fr.md'
assert "the README iteration table covers every layer AGENTS.md calls done" \
	'last=$(grep -oE "Layer [0-9]+ \(done\)" AGENTS.md | grep -oE "[0-9]+" | sort -n | tail -1);
	 grep -qE "^\| $last \|" README.md'

echo "==> architecture.md — the two languages stay structurally in step"

# A section added to one language and not the other is the most common way this
# pair drifts; counting the numbered subsections catches it without judging prose.
assert "EN and FR have the same number of §3 subsections" \
	'test "$(grep -c "^### 3\." docs/architecture.md)" = "$(grep -c "^### 3\." docs/architecture.fr.md)"'
assert "the lesson count in both files agrees with docs/lessons/" \
	'n=$(ls -d docs/lessons/[0-9]*/ | wc -l | tr -d " ");
	 grep -q "($n lessons)" docs/architecture.md && grep -q "($n leçons)" docs/architecture.fr.md'

if [ "$fail" != 0 ]; then
	echo "docs-assert: FAILED — a reference doc no longer matches the code."
	echo "  Fix the doc (EN + FR) and this file in the same commit."
	exit 1
fi
echo "docs-assert: OK"

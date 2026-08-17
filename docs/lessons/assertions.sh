#!/bin/sh
# assertions.sh — the executable half of the course.
#
# `make lessons-check` guards the STRUCTURE of docs/lessons: pinned tags exist,
# links resolve, referenced paths are real. It cannot tell whether a lesson's
# PROSE is still true, and that gap bit us: Lesson 15 (the lesson about not
# believing confident text) spent a release telling readers to run
# `grep -rin fts5 internal/` and "find nothing" — three releases after Layer 22
# added an FTS5 index.
#
# So the claims a lesson asks the reader to reproduce are re-derived here, from
# the source, on every `make release-check`. A failure means one of two things,
# and BOTH are worth a release blocking on them:
#
#   - the code changed and a lesson now teaches something false, or
#   - the assertion itself is stale (fix it here, in the same commit).
#
# Rules for adding one:
#   - assert what a LESSON PROMISES, not what the code happens to do today —
#     this is not a second test suite;
#   - keep every check offline and cheap (grep/test, no build, no network);
#   - name the lesson and the claim, so a failure says which page to fix.
#
# Usage: docs/lessons/assertions.sh    (from the repo root; make lessons-assert)

set -u
cd "$(dirname "$0")/../.." || exit 1

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

echo "==> lesson 15 (don't trust the review) — the five claims, re-derived"

# C1 — "the project is CGO-free, using modernc.org/sqlite". False: cgo driver.
assert "15/C1 the cgo driver mattn/go-sqlite3 is the one in go.mod" \
	'grep -q "github.com/mattn/go-sqlite3" go.mod'
assert "15/C1 modernc.org/sqlite is still absent" \
	'! grep -q "modernc.org/sqlite" go.mod'
assert "15/C1 CGO_ENABLED=1 is declared in Makefile and Dockerfile" \
	'grep -qE "CGO_ENABLED *:?= *1" Makefile && grep -qE "CGO_ENABLED *:?= *1" Dockerfile'

# C2 — "hybrid FTS5 + sqlite-vec". Half-true since Layer 22: hybrid recall is
# real, the named library is still the wrong one. Both halves are asserted, so
# either drifting back blocks a release.
assert "15/C2 recall really is hybrid (FTS5 lexical arm exists)" \
	'grep -rqi "fts5" internal/memory/lexical.go && test -f internal/memory/hybrid.go'
assert "15/C2 the vector arm is still vector_full_scan (sqlite-vector)" \
	'grep -rq "vector_full_scan" internal/memory/'
assert "15/C2 asg017/sqlite-vec is still NOT a dependency" \
	'! grep -q "sqlite-vec\"" go.mod && ! grep -rq "asg017/sqlite-vec" internal/'

# C3 — "resolve DNS, check the IP, then connect". False: the guard runs in the
# dialer's Control hook, which is the whole anti-rebinding point.
assert "15/C3 the SSRF guard runs inside the dialer Control hook" \
	'grep -q "Control:" internal/webfetch/webfetch.go'
assert "15/C3 the Control hook is the dial guard, and the guard vets the IP" \
	'grep -qE "Control: *c\.guardDial" internal/webfetch/webfetch.go &&
	 sed -n "/func .*guardDial/,/^}/p" internal/webfetch/webfetch.go | grep -q "blocked"'

# C4 — "doctor checks namespaces and cgroups". False: it smoke-tests memory.
assert "15/C4 doctor's own header says it smoke-tests memory" \
	'head -8 cmd/doctor/main.go | grep -qi "memory"'
assert "15/C4 doctor still mentions neither namespaces nor cgroups" \
	'! grep -qiE "namespace|cgroup" cmd/doctor/main.go'

# C5 — "blockedIP is a pure function, table-tested". True; the one claim that
# was right, and the reason "distrust everything" is the wrong lesson.
assert "15/C5 blockedIP is still pure (net.IP in, bool out)" \
	'grep -q "func blockedIP(ip net.IP) bool" internal/webfetch/webfetch.go'
assert "15/C5 blockedIP is still table-tested" \
	'grep -q "blockedIP" internal/webfetch/webfetch_test.go'

echo "==> lesson 24 (the ADR that didn't bind) — the mechanism it teaches"

# The lesson's hands-on names these symbols and tests on current main. Historical
# reading is pinned to immutable tags and cannot rot; the exercises can.
assert "24 SameSubject is still the cross-subject rule" \
	'grep -q "func SameSubject" internal/memory/subject.go'
assert "24 Supersedes still checks the subject BEFORE authority" \
	'sed -n "/^func Supersedes/,/^}/p" internal/memory/supersede.go | head -3 | grep -q "SameSubject"'
assert "24 the policy still denies user_stated authority over the world" \
	'sed -n "/^func supersedeAuthority/,/^}/p" internal/memory/supersede.go | grep -q "SubjectWorld"'
assert "24 hands-on 1 test names still exist" \
	'grep -q "TestUserWorldClaimCannotRetireVerifiedObservation" internal/agent/agent_test.go &&
	 grep -q "TestCrossSubjectSkipsTheArbiter" internal/agent/agent_test.go &&
	 grep -q "TestSupersedesTrustModel" internal/memory/supersede_test.go'
assert "24 hands-on 3: the subject is asserted at the point of assignment" \
	'sed -n "/func TestReflectLearnsFromToolObservation/,/^}/p" internal/agent/agent_test.go | grep -q "SubjectWorld"'
assert "24 reflection still asks one question per subject" \
	'grep -q "userFactPrompt" internal/agent/reflect.go && grep -q "worldFactPrompt" internal/agent/reflect.go'

if [ "$fail" != 0 ]; then
	echo "lessons-assert: FAILED — a lesson's expected result no longer holds."
	echo "  Fix the lesson (EN + FR) and this file in the same commit."
	exit 1
fi
echo "lessons-assert: OK"

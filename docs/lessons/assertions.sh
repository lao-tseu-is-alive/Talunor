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

echo "==> lesson 06 (build your first tool) — the deliberate hole, both arms"

# The lesson hands the reader a skeleton whose `Value float64` cannot tell an
# absent argument from a legitimate 0 °C, asks them to PREDICT the result, and
# only then teaches the pointer. A well-meaning reader "fixing" the skeleton
# would delete the discovery and leave a page that teaches nothing — the same
# both-arms rule as lesson 25: the flaw must survive, and so must the fix.
assert "06 the skeleton still carries the flaw (Value float64, non-pointer)" \
	'grep -qE "^ *Value float64 .json:\"value\"." docs/lessons/06-build-your-first-tool/README.md &&
	 grep -qE "^ *Value float64 .json:\"value\"." docs/lessons/06-build-your-first-tool/README.fr.md'
assert "06 the schema still claims required (that is what json ignores)" \
	'grep -q "\"required\": \[\"value\", \"from\"\]" docs/lessons/06-build-your-first-tool/README.md &&
	 grep -q "\"required\": \[\"value\", \"from\"\]" docs/lessons/06-build-your-first-tool/README.fr.md'
assert "06 the fix is taught (Value *float64) in both languages" \
	'grep -qE "Value \*float64" docs/lessons/06-build-your-first-tool/README.md &&
	 grep -qE "Value \*float64" docs/lessons/06-build-your-first-tool/README.fr.md'

# The exercise is only an exercise while the answer is absent from the repo.
assert "06 unit_convert is still NOT implemented upstream" \
	'! test -f internal/tools/unitconvert.go && ! grep -rq "UnitConvert" internal/ cmd/'

# The solution branch the lesson sends a stuck reader to must still be reachable —
# a document-vs-artefact check (v0.23.9's shape), not a claim re-derived from code.
# A fresh clone has it as a remote ref; a maintainer's checkout may have it local.
assert "06 the worked-solution branch the lesson links to still exists" \
	'git rev-parse -q --verify solutions/06-unit-convert >/dev/null ||
	 git rev-parse -q --verify origin/solutions/06-unit-convert >/dev/null ||
	 git rev-parse -q --verify refs/remotes/origin/solutions/06-unit-convert >/dev/null'

# The fork instructions are the one path the maintainer CANNOT execute (GitHub refuses
# to fork a repo into its owner's account), so the lesson says so and points at the
# report issue. Nothing offline can prove the path works; what CAN be guarded is that
# the admission survives an edit — dropping it would restore the false confidence.
assert "06 the unverified-fork-path note survives, in both languages" \
	'grep -q "the maintainer cannot run" docs/lessons/06-build-your-first-tool/README.md &&
	 grep -q "le mainteneur ne peut pas exécuter" docs/lessons/06-build-your-first-tool/README.fr.md'
assert "06 lesson and CONTRIBUTING point at the same report issue" \
	'n=$(grep -ohE "Talunor/issues/[0-9]+" docs/lessons/06-build-your-first-tool/README.md docs/lessons/06-build-your-first-tool/README.fr.md CONTRIBUTING.md | sort -u | wc -l); [ "$n" = 1 ]'

# Claims the lesson makes about the code the reader is told to open.
assert "06 the four Tool methods the lesson quotes are still the interface" \
	'sed -n "/^type Tool interface/,/^}/p" internal/tools/tool.go |
	   grep -c -E "^[[:space:]]+(Name|Description|Schema|Execute)\(" | grep -q "^4$"'
assert "06 a returned error still becomes an \"error:\" observation, not a fatal" \
	'sed -n "/func (r \*Registry) Execute/,/^}/p" internal/tools/tool.go | grep -q "\"error: \" + err.Error()"'
assert "06 builtins are still composed at tools.NewRegistry in cmd/talunor" \
	'grep -q "tools.NewRegistry(" cmd/talunor/main.go'
assert "06 the mixed test-package claim is still true (external AND internal exist)" \
	'head -1 internal/tools/tools_test.go | grep -q "^package tools_test$" &&
	 head -1 internal/tools/webfetch_test.go | grep -q "^package tools$"'
assert "06 atlas-check really walks git ls-files (the failure the lesson promises)" \
	'sed -n "/^atlas-check:/,/^$/p" Makefile | grep -q "git ls-files"'

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

echo "==> lesson 25 (the scar that never bled) — deriving, not storing"

# The lesson's whole thesis is that "contested" is DERIVED. Both arms are needed:
# the absence check alone would also pass on a repo where the feature was deleted,
# which is the hands-on the lesson makes the reader perform.
assert "25 the derivation exists (positive arm)" \
	'grep -q "func contestedExpr" internal/memory/memory.go &&
	 grep -q "polarity = .contradicts." internal/memory/memory.go'
assert "25 evidence polarity is a real column (migration 7)" \
	'sed -n "/version: 7/,/^\t},/p" internal/memory/migrate.go | grep -q "ADD COLUMN polarity"'
assert "25 no migration stores a contested/status column (negative arm)" \
	'! grep -nE "ADD COLUMN (contested|status)" internal/memory/migrate.go'
assert "25 the three load-bearing decisions stay pinned by assertions" \
	'sed -n "/func TestRefusedSupersessionIsRecordedAsCounterEvidence/,/^}/p" internal/agent/agent_test.go |
	   grep -q "must not erode the fact" &&
	 sed -n "/func TestRefusedSupersessionIsRecordedAsCounterEvidence/,/^}/p" internal/agent/agent_test.go |
	   grep -q "must live only as evidence detail" &&
	 sed -n "/func TestRefusedSupersessionIsRecordedAsCounterEvidence/,/^}/p" internal/agent/agent_test.go |
	   grep -q "not vanish"'
assert "25 a contested fact is still marked in the prompt (not decorative)" \
	'sed -n "/func fencedMemories/,/^}/p" internal/agent/turn.go | grep -q "CONTESTED"'
assert "25 the internal-test helper the hands-on uses still exists" \
	'grep -q "func internalTestStore" internal/memory/lexical_internal_test.go'

if [ "$fail" != 0 ]; then
	echo "lessons-assert: FAILED — a lesson's expected result no longer holds."
	echo "  Fix the lesson (EN + FR) and this file in the same commit."
	exit 1
fi
echo "lessons-assert: OK"

#!/usr/bin/env bash
#
# initial_setup.sh — one-time dependency setup for Talunor.
#
# This script is now a thin delegate to `make deps`, and that is the point.
#
# WHAT IT USED TO BE, AND WHY IT CHANGED
#   It began as a readable, commented companion to `make deps`: the same downloads,
#   written out step by step so a newcomer could see what setup actually does. The
#   duplication was deliberate and its own header said it might "eventually just call
#   make deps".
#
#   It should have. A 2026-08-28 review found that the two paths had diverged into
#   something worse than duplication:
#
#     - it fetched the two native `.so` extensions and the GGUF model with `curl -sL`
#       and **verified no checksums**, while `make deps` pins the exact SHA-256 of every
#       byte and fails closed on a mismatch;
#     - it had no `curl -f`, so an HTTP error page was saved as if it were an archive,
#       producing a confusing `tar` failure instead of a clear download failure;
#     - it pulled the model from a **mutable** HuggingFace `main` URL, so the bytes
#       could change without the script changing;
#     - it ran `go get` + `go mod tidy`, silently rewriting `go.mod`/`go.sum`.
#
#   A contributor following the discoverable setup script therefore loaded UNVERIFIED
#   NATIVE CODE into the Talunor process — code that runs inside SQLite, in-process,
#   with no sandbox. The safe path existed the whole time, one target away.
#
#   The rule this leaves behind: **never maintain a second implementation of artefact
#   fetching.** Documentation that duplicates a security-relevant mechanism does not
#   stay a copy; it becomes an older, weaker version of it that nobody re-reads.
#
# WHAT SETUP ACTUALLY DOES
#   `make deps` fetches, into ext/:
#     - ai.so       — sqlite-ai: runs a GGUF embedding model inside SQLite
#     - vector.so   — sqlite-vector: FLOAT32 BLOB columns + vector_full_scan KNN
#                     (NOT asg017/sqlite-vec — a different API; see AGENTS.md gotcha 1)
#     - ext/models/all-MiniLM-L6-v2.f16.gguf — 384-dim embeddings, mean-pooled
#   Each is verified against a SHA-256 pinned in the Makefile. Versions are pinned
#   there too, so read the Makefile to see exactly what is fetched — it is the source
#   of truth, and this comment is not.
#
#   The Go SQLite driver is an ordinary module dependency already in go.mod. It needs
#   no `go get`: `make deps` and `go build` resolve it.
#
# Usage
#   scripts/initial_setup.sh      # equivalent to: make deps
#
set -euo pipefail

cd "$(dirname "$0")/.."

command -v make >/dev/null || {
	echo "error: make not found — it drives the verified dependency fetch" >&2
	exit 1
}
command -v curl >/dev/null || {
	echo "error: curl not found — 'make deps' uses it to fetch the extensions" >&2
	exit 1
}
if ! command -v gcc >/dev/null && ! command -v cc >/dev/null; then
	echo "error: no C compiler found — Talunor needs cgo (CGO_ENABLED=1)" >&2
	exit 1
fi

echo ">> Delegating to 'make deps' (checksum-verified, fails closed)..."
exec make deps

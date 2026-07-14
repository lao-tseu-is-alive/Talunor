#!/usr/bin/env bash
#
# initial_setup.sh — Talunor MVP initial dependency setup
#
# A few notes on this script
#  **It mirrors exactly what I ran** —
#   the two release tarballs (`ai.so`, `vector.so`),
#   the F16 `all-MiniLM-L6-v2` GGUF from `second-state`,
#   plus the `go get` for the cgo SQLite driver (step 5, which was the planned next command).
# **It overlaps with the `Makefile`'s `deps` target** by design:
#   the Makefile is the day-to-day reproducible path (idempotent, version-pinned),
#   and this script is the readable, commented "here's what one-time setup actually does"
#   companion you asked for. If you'd rather avoid duplication later,
#   this script can eventually just call `make deps`.

# Fetches the two loadable SQLite extensions (sqlite-ai for in-DB inference /
# embeddings, sqlite-vector for KNN vector search) and the sentence-embedding
# model, then wires up the Go SQLite driver. Targets Linux x86_64 (CPU build).
#
# Re-runnable: existing files are skipped by `make deps`; this script forces
# a clean fetch. Run from the repository root.
set -euo pipefail

# --- Pinned versions ---------------------------------------------------------
# Pin exact release tags so the setup is reproducible across machines.
AI_VERSION="1.0.4"       # github.com/sqliteai/sqlite-ai
VECTOR_VERSION="1.0.0"   # github.com/sqliteai/sqlite-vector

# --- 0. Prerequisites --------------------------------------------------------
# sqlite-ai/sqlite-vector are C shared objects loaded at runtime, so the Go
# driver (mattn/go-sqlite3) must be built with cgo. Verify the toolchain.
echo ">> Checking toolchain (go, gcc, curl, tar)..."
command -v go   >/dev/null || { echo "go not found";   exit 1; }
command -v gcc  >/dev/null || { echo "gcc not found (cgo needs a C compiler)"; exit 1; }
command -v curl >/dev/null || { echo "curl not found"; exit 1; }
command -v tar  >/dev/null || { echo "tar not found";  exit 1; }
export CGO_ENABLED=1   # required: extensions are loaded via cgo-backed driver

# --- 1. Directory layout -----------------------------------------------------
# ext/        -> the two .so extensions loaded into SQLite at startup
# ext/models/ -> the GGUF embedding model fed to sqlite-ai
echo ">> Creating ext/ and ext/models/ ..."
mkdir -p ext/models

# --- 2. sqlite-ai extension (in-DB embeddings + inference) --------------------
# Ships the loadable extension as ai.so inside a tarball. We extract just ai.so.
echo ">> Downloading sqlite-ai ${AI_VERSION} ..."
curl -sL -o ext/ai.tar.gz \
  "https://github.com/sqliteai/sqlite-ai/releases/download/${AI_VERSION}/ai-linux-cpu-x86_64-${AI_VERSION}.tar.gz"
tar xzf ext/ai.tar.gz -C ext ./ai.so
rm -f ext/ai.tar.gz

# --- 3. sqlite-vector extension (KNN vector search) --------------------------
# NOTE: this is sqliteai/sqlite-vector (BLOB columns + vector_full_scan),
# NOT asg017/sqlite-vec (vec0 virtual tables) — different API.
echo ">> Downloading sqlite-vector ${VECTOR_VERSION} ..."
curl -sL -o ext/vector.tar.gz \
  "https://github.com/sqliteai/sqlite-vector/releases/download/${VECTOR_VERSION}/vector-linux-x86_64-${VECTOR_VERSION}.tar.gz"
tar xzf ext/vector.tar.gz -C ext ./vector.so
rm -f ext/vector.tar.gz

# --- 4. Embedding model (all-MiniLM-L6-v2, 384 dims, F16 GGUF) ----------------
# sqlite-ai runs this GGUF in-process; 384-dim normalized mean-pooled vectors.
echo ">> Downloading all-MiniLM-L6-v2 (F16 GGUF) ..."
curl -sL -o ext/models/all-MiniLM-L6-v2.f16.gguf \
  "https://huggingface.co/second-state/All-MiniLM-L6-v2-Embedding-GGUF/resolve/main/all-MiniLM-L6-v2-ggml-model-f16.gguf"

# --- 5. Go SQLite driver -----------------------------------------------------
# mattn/go-sqlite3: cgo driver that supports LoadExtension() for the .so files.
echo ">> Adding Go SQLite driver ..."
go get github.com/mattn/go-sqlite3
go mod tidy

# --- 6. Verify ---------------------------------------------------------------
echo ">> Fetched artifacts:"
ls -la ext/ ext/models/
file ext/ai.so ext/vector.so   # expect: ELF 64-bit shared objects

echo ">> Done. Next: 'make doctor' to smoke-test the memory substrate."


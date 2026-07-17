# Talunor — autonomous agent MVP
# Loadable SQLite extensions + embedding model are fetched (not vendored in git).

AI_VERSION     := 1.0.4
VECTOR_VERSION := 1.0.0

# Linux x86_64 CPU builds. Override on other platforms.
AI_ASSET     := ai-linux-cpu-x86_64-$(AI_VERSION).tar.gz
VECTOR_ASSET := vector-linux-x86_64-$(VECTOR_VERSION).tar.gz
AI_URL       := https://github.com/sqliteai/sqlite-ai/releases/download/$(AI_VERSION)/$(AI_ASSET)
VECTOR_URL   := https://github.com/sqliteai/sqlite-vector/releases/download/$(VECTOR_VERSION)/$(VECTOR_ASSET)

# all-MiniLM-L6-v2, 384-dim sentence embeddings, F16 GGUF.
EMBED_MODEL := ext/models/all-MiniLM-L6-v2.f16.gguf
# NOTE: this URL tracks the *mutable* `main` ref — HuggingFace can re-upload the
# file, which would trip EMBED_SHA256 by design (fail-closed). The GitHub release
# assets above are immutable by tag; the model is not. If the pin ever fails on a
# legitimate upstream change, re-pin from a trusted copy — better, switch `main`
# to an immutable `resolve/<commit-sha>/…` revision so downloads are reproducible.
EMBED_URL   := https://huggingface.co/second-state/All-MiniLM-L6-v2-Embedding-GGUF/resolve/main/all-MiniLM-L6-v2-ggml-model-f16.gguf

# SHA256 of the artefacts we actually load into the process (the extracted .so
# files and the .gguf model), pinned to known-good downloads. `make deps` refuses
# to use a file whose hash does not match — turning "whatever the URL serves
# today" into "exactly the bytes we reviewed", which matters because these .so
# files run as native code inside Talunor with no sandbox. When bumping a
# *_VERSION above, regenerate with: sha256sum ext/ai.so ext/vector.so $(EMBED_MODEL)
AI_SHA256     := c7654ffb6bf3ae50b86b9bb67ebeece4e5ad0ae416236b09991e4f4bf2708608
VECTOR_SHA256 := 2e4d4781fb439ddafff59977fd88178afdf628f71dec84d51d3d2ce41e8ce345
EMBED_SHA256  := 797b70c4edf85907fe0a49eb85811256f65fa0f7bf52166b147fd16be2be4662

# verify_sha256(expected_hash, file): fail the build and delete the file if its
# hash does not match, so a tampered or truncated download is never used.
define verify_sha256
@echo "$(1)  $(2)" | sha256sum -c - >/dev/null 2>&1 \
	|| { echo "talunor: checksum mismatch for $(2) — refusing a tampered/corrupt artefact"; rm -f "$(2)"; exit 1; }
@echo "talunor: verified $(2)"
endef

# curl for asset downloads. -f makes an HTTP error (4xx/5xx) fail the command
# instead of silently saving the error page as if it were the artefact — without
# it, a transient 504 lands a tiny HTML file that only trips the checksum later,
# with a misleading "mismatch" message. Retries ride out flaky mirrors/CDNs.
CURL := curl -fsSL --retry 5 --retry-delay 2 --retry-all-errors

export CGO_ENABLED := 1

# Build metadata injected into internal/version at link time.
VERSION_PKG := github.com/lao-tseu-is-alive/Talunor/internal/version
GIT_COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -X $(VERSION_PKG).Commit=$(GIT_COMMIT) -X $(VERSION_PKG).Date=$(BUILD_DATE)

# Container image (local builds). IMAGE overridable; nerdctl for Rancher Desktop.
IMAGE ?= talunor:local

.PHONY: deps doctor build test release-check atlas-check readme-check lessons-check tidy clean distclean \
        docker-build docker-run nerdctl-build nerdctl-run

## deps: download the SQLite extensions and the embedding model into ext/
deps: ext/vector.so ext/ai.so $(EMBED_MODEL)

ext/ai.so:
	@mkdir -p ext
	$(CURL) -o ext/ai.tar.gz "$(AI_URL)"
	tar xzf ext/ai.tar.gz -C ext ./ai.so
	rm -f ext/ai.tar.gz
	$(call verify_sha256,$(AI_SHA256),ext/ai.so)

ext/vector.so:
	@mkdir -p ext
	$(CURL) -o ext/vector.tar.gz "$(VECTOR_URL)"
	tar xzf ext/vector.tar.gz -C ext ./vector.so
	rm -f ext/vector.tar.gz
	$(call verify_sha256,$(VECTOR_SHA256),ext/vector.so)

$(EMBED_MODEL):
	@mkdir -p ext/models
	$(CURL) -o $(EMBED_MODEL) "$(EMBED_URL)"
	$(call verify_sha256,$(EMBED_SHA256),$(EMBED_MODEL))

## doctor: smoke-test the memory substrate (extensions + embedding + KNN)
doctor: deps
	go run -ldflags "$(LDFLAGS)" ./cmd/doctor

## build: compile all binaries into bin/
build: deps
	go build -ldflags "$(LDFLAGS)" -o bin/ ./...

## test: run the test suite (skips memory tests if deps are missing)
test:
	go test ./...

## release-check: pre-release gate (run before `git tag`). Bundles the AGENTS.md
## ritual — gofmt, vet, tests — plus guards for the class of bug that only bites a
## release: a dropped fetch target (v0.9.1 lost the model this way) and a drifted
## asset checksum. Offline: it re-verifies the ext/ already on disk rather than
## re-downloading. The heavier, networked proof is a clean-room `make nerdctl-build`.
release-check: deps
	@echo "==> gofmt (no diffs allowed)"
	@bad="$$(gofmt -l .)"; [ -z "$$bad" ] || { echo "needs gofmt:"; echo "$$bad"; exit 1; }
	@echo "==> go vet"
	@go vet ./...
	@echo "==> go test"
	@go test ./... -count=1
	@echo "==> fetch targets intact (no asset silently dropped from 'deps')"
	@[ -n "$(EMBED_MODEL)" ] || { echo "EMBED_MODEL is empty — a fetch target was dropped"; exit 1; }
	@for a in ext/vector.so ext/ai.so $(EMBED_MODEL); do \
	  $(MAKE) --no-print-directory -Bn deps | grep -q "$$a" \
	    || { echo "'deps' no longer builds $$a"; exit 1; }; \
	done
	@echo "==> re-verify checksums of the fetched artefacts"
	$(call verify_sha256,$(VECTOR_SHA256),ext/vector.so)
	$(call verify_sha256,$(AI_SHA256),ext/ai.so)
	$(call verify_sha256,$(EMBED_SHA256),$(EMBED_MODEL))
	@echo "==> atlas coverage (docs/atlas.md references every tracked file)"
	@$(MAKE) --no-print-directory atlas-check
	@echo "==> README version banner matches internal/version"
	@$(MAKE) --no-print-directory readme-check
	@echo "==> lessons reference valid tags / links / files"
	@$(MAKE) --no-print-directory lessons-check
	@echo "release-check: OK"

## atlas-check: fail if docs/atlas.md doesn't reference every tracked file, so a
## file added/removed without refreshing the map blocks a release (structural
## drift). It cannot tell whether a *comment* is still accurate — that stays a
## human/model judgement — only that nothing is missing. Regenerate the map with
## the `repo-atlas` skill when this fails.
atlas-check:
	@test -f docs/atlas.md || { echo "docs/atlas.md is missing"; exit 1; }
	@missing=0; \
	for f in $$(git ls-files | grep -v '^docs/atlas\.md$$'); do \
	  grep -q "$$(basename "$$f")" docs/atlas.md \
	    || { echo "atlas: not referenced: $$f"; missing=1; }; \
	done; \
	[ "$$missing" = 0 ] || { echo "docs/atlas.md is stale — regenerate it (repo-atlas skill)"; exit 1; }
	@echo "atlas-check: OK"

## readme-check: fail if the README "Current version" banner drifts from the
## Version constant in internal/version (the source of truth, bumped before this
## gate runs — so it checks the constant, not a git tag that doesn't exist yet).
## Update the banner line whenever you bump the version.
readme-check:
	@ver=$$(grep -oE 'Version = "[0-9]+\.[0-9]+\.[0-9]+"' internal/version/version.go | grep -oE '[0-9]+\.[0-9]+\.[0-9]+'); \
	  grep -q "Current version: \*\*v$$ver\*\*" README.md \
	    || { echo "README 'Current version' banner != v$$ver (internal/version) — update it"; exit 1; }; \
	  echo "readme-check: OK (v$$ver)"

## lessons-check: keep docs/lessons references valid — every pinned git tag exists,
## every sibling-lesson link resolves, and every file named in a `git diff vA vB --
## path` exists at both tags. Historical lessons pin to immutable tags, so these
## refs should never rot; this catches an author's typo (a wrong tag or path). It
## does NOT judge whether inline snippets are still accurate — that stays the
## author's job, like the other drift alarms. No-op when docs/lessons is absent.
lessons-check:
	@test -d docs/lessons || { echo "lessons-check: no docs/lessons/ (skipped)"; exit 0; }
	@cur=$$(grep -oE 'Version = "[0-9]+\.[0-9]+\.[0-9]+"' internal/version/version.go | grep -oE '[0-9]+\.[0-9]+\.[0-9]+'); \
	fail=0; \
	for v in $$(grep -rhoE 'v[0-9]+\.[0-9]+\.[0-9]+' docs/lessons | sort -u); do \
	  [ "$$v" = "v$$cur" ] && continue; \
	  git rev-parse -q --verify "$$v^{commit}" >/dev/null 2>&1 || { echo "lessons: references unknown tag $$v"; fail=1; }; \
	done; \
	for d in $$(grep -rhoE '\((\.\./)?[0-9][0-9]-[a-z0-9-]+/\)' docs/lessons | grep -oE '[0-9][0-9]-[a-z0-9-]+' | sort -u); do \
	  test -d "docs/lessons/$$d" || { echo "lessons: broken link to $$d/"; fail=1; }; \
	done; \
	tmp=$$(mktemp); grep -rhoE 'git diff v[0-9.]+ v[0-9.]+ -- [A-Za-z0-9_./-]+' docs/lessons | sort -u > "$$tmp"; \
	while read -r _ _ ta tb _ p; do \
	  git cat-file -e "$$ta:$$p" 2>/dev/null || { echo "lessons: $$p missing at $$ta"; fail=1; }; \
	  git cat-file -e "$$tb:$$p" 2>/dev/null || { echo "lessons: $$p missing at $$tb"; fail=1; }; \
	done < "$$tmp"; rm -f "$$tmp"; \
	[ "$$fail" = 0 ] || { echo "lessons-check: FAILED"; exit 1; }; \
	echo "lessons-check: OK"

## chat: stream one prompt to a local Ollama model (LLM provider smoke test)
##   usage: make chat PROMPT="explain vector search in one sentence"
chat:
	go run -ldflags "$(LDFLAGS)" ./cmd/chat "$(PROMPT)"

## run: start the interactive agent REPL (persistent memory across sessions)
run: deps
	go run -ldflags "$(LDFLAGS)" ./cmd/talunor

## docker-build: build the self-contained image (binary + extensions + model)
docker-build:
	docker build --build-arg COMMIT=$(GIT_COMMIT) --build-arg BUILD_DATE=$(BUILD_DATE) -t $(IMAGE) .

# Reach the host's Ollama from inside the container. host.docker.internal works
# on Rancher Desktop / Docker Desktop and, via --add-host, native Docker on Linux.
# Default port 11435 = the secure bridge (see docs/ollama-networking.md); override
# with e.g. OLLAMA_URL=http://host.docker.internal:11434/v1 for the quick option.
OLLAMA_URL ?= http://host.docker.internal:11435/v1
RUN_NET := --add-host=host.docker.internal:host-gateway \
           -e TALUNOR_OLLAMA_URL=$(OLLAMA_URL)

## docker-run: run the TUI from the image (needs a TTY + a local Ollama)
docker-run:
	docker run --rm -it $(RUN_NET) -v talunor-data:/data $(IMAGE)

## nerdctl-build: same as docker-build, via nerdctl (Rancher Desktop / containerd)
nerdctl-build:
	nerdctl build --build-arg COMMIT=$(GIT_COMMIT) --build-arg BUILD_DATE=$(BUILD_DATE) -t $(IMAGE) .

## nerdctl-run: run the TUI from the image via nerdctl
nerdctl-run:
	nerdctl run --rm -it $(RUN_NET) -v talunor-data:/data $(IMAGE)

## tidy: sync go.mod/go.sum
tidy:
	go mod tidy

## clean: remove build output and the local database
clean:
	rm -rf bin talunor.db talunor.db-shm talunor.db-wal

## distclean: also remove fetched extensions and models
distclean: clean
	rm -f ext/*.so ext/models/*.gguf

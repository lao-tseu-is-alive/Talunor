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
EMBED_URL   := https://huggingface.co/second-state/All-MiniLM-L6-v2-Embedding-GGUF/resolve/main/all-MiniLM-L6-v2-ggml-model-f16.gguf

export CGO_ENABLED := 1

# Build metadata injected into internal/version at link time.
VERSION_PKG := github.com/lao-tseu-is-alive/Talunor/internal/version
GIT_COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -X $(VERSION_PKG).Commit=$(GIT_COMMIT) -X $(VERSION_PKG).Date=$(BUILD_DATE)

# Container image (local builds). IMAGE overridable; nerdctl for Rancher Desktop.
IMAGE ?= talunor:local

.PHONY: deps doctor build tidy clean distclean \
        docker-build docker-run nerdctl-build nerdctl-run

## deps: download the SQLite extensions and the embedding model into ext/
deps: ext/vector.so ext/ai.so $(EMBED_MODEL)

ext/ai.so:
	@mkdir -p ext
	curl -sL -o ext/ai.tar.gz "$(AI_URL)"
	tar xzf ext/ai.tar.gz -C ext ./ai.so
	rm -f ext/ai.tar.gz

ext/vector.so:
	@mkdir -p ext
	curl -sL -o ext/vector.tar.gz "$(VECTOR_URL)"
	tar xzf ext/vector.tar.gz -C ext ./vector.so
	rm -f ext/vector.tar.gz

$(EMBED_MODEL):
	@mkdir -p ext/models
	curl -sL -o $(EMBED_MODEL) "$(EMBED_URL)"

## doctor: smoke-test the memory substrate (extensions + embedding + KNN)
doctor: deps
	go run -ldflags "$(LDFLAGS)" ./cmd/doctor

## build: compile all binaries into bin/
build: deps
	go build -ldflags "$(LDFLAGS)" -o bin/ ./...

## test: run the test suite (skips memory tests if deps are missing)
test:
	go test ./...

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
# NB: Ollama must listen on 0.0.0.0 (OLLAMA_HOST=0.0.0.0:11434), not just 127.0.0.1.
RUN_NET := --add-host=host.docker.internal:host-gateway \
           -e TALUNOR_OLLAMA_URL=http://host.docker.internal:11434/v1

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

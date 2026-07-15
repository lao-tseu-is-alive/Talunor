# syntax=docker/dockerfile:1
#
# Talunor — self-contained image.
#
# Unlike a static Go service, Talunor is cgo: it links glibc and, at runtime,
# dlopens two SQLite extensions (sqlite-vector, sqlite-ai) and loads a GGUF
# embedding model. This image bakes all three in, so `run` works with no
# first-boot download and embeddings run fully offline. The only external
# dependency is the chat LLM (a local Ollama), reached over the network.
#
# Build args (optional): COMMIT and BUILD_DATE feed internal/version via
# -ldflags. The semantic Version is a const in the source, so tagged builds carry
# the right version automatically.
#
# Debian trixie (glibc 2.41) is used for both stages: the prebuilt sqliteai
# extensions were linked against an older glibc, and newer glibc is backward
# compatible, so trixie safely satisfies both them and the Go binary.

# ---- builder ---------------------------------------------------------------
FROM golang:1.26-trixie AS builder

# make + curl drive `make deps` (fetches the extensions + model); the golang
# image already provides gcc/git for the cgo build.
RUN apt-get update \
 && apt-get install -y --no-install-recommends make curl ca-certificates \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Cache module downloads separately from the source for faster rebuilds.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Fetch vector.so, ai.so and the GGUF model into ext/ (ext/ is .dockerignored,
# so this always pulls fresh assets rather than copying a local checkout).
RUN make deps

ARG COMMIT=docker
ARG BUILD_DATE=unknown
ENV CGO_ENABLED=1
RUN go build \
      -ldflags "-s -w \
        -X github.com/lao-tseu-is-alive/Talunor/internal/version.Commit=${COMMIT} \
        -X github.com/lao-tseu-is-alive/Talunor/internal/version.Date=${BUILD_DATE}" \
      -o /out/talunor ./cmd/talunor

# ---- runtime ---------------------------------------------------------------
FROM debian:trixie-slim

# ai.so (llama.cpp embedding runtime) needs libstdc++ and libgcc_s; libm/libc
# come from the base. ca-certificates lets the agent reach an HTTPS LLM endpoint.
RUN apt-get update \
 && apt-get install -y --no-install-recommends libstdc++6 ca-certificates \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /out/talunor /usr/local/bin/talunor
COPY --from=builder /src/ext /app/ext

# Point the agent at the baked-in extensions/model and a persistent DB location.
# TALUNOR_OLLAMA_URL is intentionally unset: the code defaults to
# http://localhost:11434/v1, which works with `--network host`. Users on a
# bridge network override it (see README "Run without building").
ENV TALUNOR_VECTOR_EXT=/app/ext/vector \
    TALUNOR_AI_EXT=/app/ext/ai \
    TALUNOR_EMBED_MODEL=/app/ext/models/all-MiniLM-L6-v2.f16.gguf \
    TALUNOR_DB=/data/talunor.db

# Long-term memory persists here; mount a volume to keep it across runs.
VOLUME /data

# Default to the TUI (needs `run -it`). Pass --plain, --list N, etc. as args.
ENTRYPOINT ["talunor"]

# syntax=docker/dockerfile:1
#
# Talunor — self-contained, minimal-surface image.
#
# Unlike a static Go service, Talunor is cgo: it links glibc and, at runtime,
# dlopens two SQLite extensions (sqlite-vector, sqlite-ai) and loads a GGUF
# embedding model. This image bakes all three in, so `run` works with no
# first-boot download and embeddings run fully offline. The only external
# dependency is the chat LLM (a local Ollama), reached over the network.
#
# Base choice — why distroless:
#   The runtime is gcr.io/distroless/cc, which contains only glibc, libstdc++,
#   libgcc and ca-certificates — exactly what the Go binary and ai.so need
#   (ai.so's NEEDED = libstdc++, libgcc_s, libm, libc). It ships no shell, no
#   apt, no perl/util-linux, so almost all of a general debian image's CVEs (most
#   of them unfixable distro triage) simply do not exist here.
#   distroless/cc is debian12 (glibc 2.36); the prebuilt extensions require at
#   most GLIBC_2.34 / GLIBCXX_3.4.29 (checked with `objdump -T`), so bookworm is
#   comfortably new enough. The builder therefore also targets bookworm, so the
#   Go binary never demands a newer glibc than the runtime provides.
#
# Build args (optional): COMMIT and BUILD_DATE feed internal/version via
# -ldflags. The semantic Version is a const in the source.

# ---- builder ---------------------------------------------------------------
FROM golang:1.26-bookworm AS builder

# make + curl drive `make deps`; the golang image already provides gcc/git.
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

# Stage an empty /data so the runtime stage can COPY it in with nonroot
# ownership — distroless has no shell/mkdir to create it there.
RUN mkdir -p /stage-data

ARG COMMIT=docker
ARG BUILD_DATE=unknown
ENV CGO_ENABLED=1
RUN go build \
      -ldflags "-s -w \
        -X github.com/lao-tseu-is-alive/Talunor/internal/version.Commit=${COMMIT} \
        -X github.com/lao-tseu-is-alive/Talunor/internal/version.Date=${BUILD_DATE}" \
      -o /out/talunor ./cmd/talunor

# ---- runtime ---------------------------------------------------------------
# distroless/cc = glibc + libstdc++ + libgcc + ca-certificates (+ /tmp), nothing
# else. The :nonroot tag runs as the unprivileged user 65532 by default (no
# shell, no root) — a tampered model or a bug in a loaded extension cannot touch
# the host filesystem as root. /data is seeded with that ownership so long-term
# memory stays writable without privilege.
FROM gcr.io/distroless/cc-debian12:nonroot

WORKDIR /app
COPY --from=builder /out/talunor /usr/local/bin/talunor
COPY --from=builder /src/ext /app/ext

# Writable state dir owned by the nonroot user (65532). A fresh named volume
# mounted at /data inherits this ownership from the image, so `-v talunor-data:/data`
# just works. A host bind-mount must itself be writable by uid 65532.
COPY --from=builder --chown=65532:65532 /stage-data /data

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

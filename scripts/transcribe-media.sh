#!/usr/bin/env bash
#
# transcribe-media.sh — turn a video/audio file into a plain-text transcript,
# locally, so a generated artefact about this project can be fact-checked.
#
# Why this exists
#   `v0.23.2` was written because a generated video overview of Talunor narrated
#   two claims that were wrong — and tracing them back showed both came from a
#   stale section of `docs/architecture.md`, not from the model. That made the
#   video a *drift detector*: a summariser has no loyalty to what you meant, it
#   propagates what you wrote.
#
#   Repeating that check needs a transcript, and judging a 7-minute narration
#   from memory is not a check. This script produces the transcript so the
#   verification can be done against the code, claim by claim.
#
#   It stays local on purpose: uploading an unreleased description of your own
#   architecture to a transcription service is a decision, not a convenience.
#
# What to do with the output (the method, briefly)
#   1. Check each claim against the CODE, not against the docs the artefact was
#      built from — verifying fidelity to the sources only proves the tool ran.
#   2. Hunt OMISSIONS, not just errors. This project's honesty lives in its
#      caveats ("model_inferred by default", "no seccomp — not a boundary for
#      hostile code", "a mitigation, not a boundary"). A summary drops those by
#      construction, and dropping them turns an honest project into an
#      overclaiming one. Omission is the finding.
#   3. When something is wrong, ask where it came FROM before fixing the
#      artefact. It is often the doc.
#
# Requirements
#   - ffmpeg               (audio extraction; any container ffmpeg can read)
#   - whisper.cpp's CLI    https://github.com/ggml-org/whisper.cpp
#   - a real ggml model    e.g. models/ggml-large-v3-turbo.bin
#     NOTE: whisper.cpp ships tiny `for-tests-ggml-*.bin` dummies (~600 KB).
#     Those are NOT models; download a real one with models/download-ggml-model.sh.
#
# Configuration (env vars, all optional)
#   WHISPER_CLI    path to whisper-cli      (default: auto-detect, see below)
#   WHISPER_MODEL  path to a .bin ggml model (default: auto-detect)
#   WHISPER_LANG   language hint, or "auto" (default: auto)
#   WHISPER_THREADS number of threads       (default: nproc)
#
# Usage
#   scripts/transcribe-media.sh VIDEO_OR_AUDIO [OUT_BASENAME]
#   scripts/transcribe-media.sh talk.mp4                 # -> talk.txt
#   scripts/transcribe-media.sh talk.mp4 /tmp/talk       # -> /tmp/talk.txt
#   WHISPER_FORMAT=srt scripts/transcribe-media.sh talk.mp4
#
#   WHISPER_FORMAT accepts txt (default), srt, vtt, or csv.
#
set -euo pipefail

die() { printf '\033[31merror:\033[0m %s\n' "$*" >&2; exit 1; }
info() { printf '\033[2m==>\033[0m %s\n' "$*" >&2; }

# ---- arguments --------------------------------------------------------------

[ $# -ge 1 ] || die "usage: $(basename "$0") VIDEO_OR_AUDIO [OUT_BASENAME]"
src="$1"
[ -f "$src" ] || die "no such file: $src"
# Default output sits next to the input, minus its extension.
out="${2:-${src%.*}}"
fmt="${WHISPER_FORMAT:-txt}"

case "$fmt" in
	txt|srt|vtt|csv) ;;
	*) die "WHISPER_FORMAT must be one of: txt srt vtt csv (got '$fmt')" ;;
esac

# ---- dependencies -----------------------------------------------------------

command -v ffmpeg >/dev/null || die "ffmpeg not found — install it (it does the audio extraction)"

# whisper-cli: honour the env var, else look in the usual places.
cli="${WHISPER_CLI:-}"
if [ -z "$cli" ]; then
	for c in \
		"$HOME/tools/whisper.cpp/build/bin/whisper-cli" \
		"$HOME/whisper.cpp/build/bin/whisper-cli" \
		"$(command -v whisper-cli 2>/dev/null || true)"
	do
		[ -n "$c" ] && [ -x "$c" ] && { cli="$c"; break; }
	done
fi
[ -n "$cli" ] && [ -x "$cli" ] || die \
	"whisper-cli not found. Build https://github.com/ggml-org/whisper.cpp and either
       put it on PATH, or set WHISPER_CLI=/path/to/whisper-cli"

# Model: honour the env var, else pick the largest real .bin near the CLI.
# The size filter matters — whisper.cpp ships ~600 KB `for-tests-*` dummies that
# load happily and transcribe nothing useful.
model="${WHISPER_MODEL:-}"
if [ -z "$model" ]; then
	model_dir="$(dirname "$(dirname "$(dirname "$cli")")")/models"
	if [ -d "$model_dir" ]; then
		model="$(find "$model_dir" -maxdepth 1 -name 'ggml-*.bin' -size +20M \
			-printf '%s\t%p\n' 2>/dev/null | sort -rn | head -1 | cut -f2)"
	fi
fi
[ -n "$model" ] && [ -f "$model" ] || die \
	"no ggml model found. Fetch one, e.g.:
       (cd \"\$(dirname \"\$(dirname \"\$(dirname \"$cli\")\")\")\" && ./models/download-ggml-model.sh large-v3-turbo)
     or set WHISPER_MODEL=/path/to/ggml-*.bin"

# ---- extract audio ----------------------------------------------------------

# whisper.cpp wants 16 kHz mono signed 16-bit PCM; give it exactly that rather
# than letting it guess. The temp file is removed on any exit path.
wav="$(mktemp -t transcribe-XXXXXX.wav)"
trap 'rm -f "$wav"' EXIT

info "extracting audio: $(basename "$src")"
ffmpeg -v error -i "$src" -vn -ar 16000 -ac 1 -c:a pcm_s16le "$wav" -y \
	|| die "ffmpeg could not extract audio from $src"
[ -s "$wav" ] || die "extracted audio is empty — does $src have an audio track?"

# ---- transcribe -------------------------------------------------------------

lang="${WHISPER_LANG:-auto}"
threads="${WHISPER_THREADS:-$(nproc 2>/dev/null || echo 4)}"

info "model:   $(basename "$model")"
info "language: $lang · threads: $threads · format: $fmt"
info "transcribing… (roughly real-time ÷ 30 on CPU with large-v3-turbo)"

"$cli" \
	--model "$model" \
	--file "$wav" \
	--language "$lang" \
	--threads "$threads" \
	"--output-$fmt" \
	--output-file "$out" \
	--print-progress >/dev/null

result="$out.$fmt"
[ -f "$result" ] || die "whisper produced no $result — check the output above"

words="$(wc -w <"$result" | tr -d ' ')"
info "done: $result ($words words)"
printf '%s\n' "$result"

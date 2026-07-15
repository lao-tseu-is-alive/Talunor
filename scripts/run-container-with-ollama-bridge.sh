#!/usr/bin/env bash
#
# run-container-with-ollama-bridge.sh — start the loopback→VM Ollama bridge,
# then run the Talunor container wired to it.
#
# Why this exists
#   Ollama listens on 127.0.0.1:11434 only (good — keep it that way). Under
#   Rancher/Docker Desktop the container runs in a VM, so it cannot reach the
#   host's loopback directly. This script does the two steps that make it work,
#   in order:
#     1. PRE-nerdctl: start a tiny socat forwarder from a VM-reachable port
#        (11435) to loopback Ollama —
#          socat TCP-LISTEN:11435,reuseaddr,fork,bind=0.0.0.0 TCP:127.0.0.1:11434
#     2. Run the container with the correct nerdctl call (host.docker.internal +
#        TALUNOR_OLLAMA_URL pointing at the bridge port).
#   See docs/ollama-networking.md for the durable (systemd) version and the
#   firewall rule that is the actual security control.
#
# SECURITY
#   `bind=0.0.0.0` on the bridge is only safe behind a default-drop firewall that
#   allows dport 11435 from the VM subnet ONLY (Rancher Desktop default:
#   192.168.5.0/24). Without that rule this exposes Ollama to your LAN. This
#   script refuses to bind 0.0.0.0 unless you pass --i-have-a-firewall (or set
#   BRIDGE_BIND to a specific VM-facing address). The persistent systemd bridge
#   in docs/ollama-networking.md is the recommended setup.
#
# Usage
#   scripts/run-container-with-ollama-bridge.sh [--i-have-a-firewall] [-- <extra nerdctl args>]
#
# Env overrides (with defaults):
#   OLLAMA_PORT=11434     # loopback port Ollama actually listens on
#   BRIDGE_PORT=11435     # VM-reachable port the container connects to
#   BRIDGE_BIND=          # address socat binds; empty => 0.0.0.0 (needs firewall)
#   IMAGE=ghcr.io/lao-tseu-is-alive/talunor:latest
#   RUNTIME=nerdctl       # or docker
#   DATA_VOLUME=talunor-data
#
set -euo pipefail

OLLAMA_PORT="${OLLAMA_PORT:-11434}"
BRIDGE_PORT="${BRIDGE_PORT:-11435}"
BRIDGE_BIND="${BRIDGE_BIND:-}"
IMAGE="${IMAGE:-ghcr.io/lao-tseu-is-alive/talunor:latest}"
RUNTIME="${RUNTIME:-nerdctl}"
DATA_VOLUME="${DATA_VOLUME:-talunor-data}"

HAVE_FIREWALL=0
EXTRA_ARGS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --i-have-a-firewall) HAVE_FIREWALL=1; shift ;;
    --) shift; EXTRA_ARGS=("$@"); break ;;
    -h|--help) sed -n '2,35p' "$0"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

# --- 0. Prerequisites --------------------------------------------------------
command -v socat >/dev/null || { echo "socat not found (install it, e.g. 'sudo apt install socat')"; exit 1; }
command -v "$RUNTIME" >/dev/null || { echo "$RUNTIME not found on PATH"; exit 1; }

# Ollama should already be up on loopback; warn (don't fail) if we can't tell.
if command -v ss >/dev/null && ! ss -tlnH "sport = :$OLLAMA_PORT" | grep -q .; then
  echo "!! Nothing seems to be listening on 127.0.0.1:$OLLAMA_PORT — is Ollama running?"
  echo "   (continuing anyway; the bridge will just have nothing to forward to)"
fi

# --- 1. Decide the bind address ---------------------------------------------
# Binding 0.0.0.0 relies on your firewall; a specific VM-facing bind is safer.
if [[ -z "$BRIDGE_BIND" ]]; then
  if [[ "$HAVE_FIREWALL" -ne 1 ]]; then
    cat >&2 <<EOF
Refusing to bind the bridge to 0.0.0.0 without a firewall.
  socat TCP-LISTEN:$BRIDGE_PORT,...,bind=0.0.0.0 exposes Ollama to anything that
  can reach this host on port $BRIDGE_PORT. Choose one:
    • add the default-drop nftables rule (see docs/ollama-networking.md) and
      re-run with  --i-have-a-firewall
    • bind to a specific VM-facing address:  BRIDGE_BIND=<host-vm-ip> $0
EOF
    exit 1
  fi
  BRIDGE_BIND="0.0.0.0"
fi

# --- 2. PRE-nerdctl: start the socat bridge ---------------------------------
BRIDGE_PID=""
already_bridged() {
  command -v ss >/dev/null && ss -tlnH "sport = :$BRIDGE_PORT" | grep -q .
}
if already_bridged; then
  echo ">> A listener is already on :$BRIDGE_PORT — reusing it (not starting socat)."
else
  echo ">> Starting socat bridge: $BRIDGE_BIND:$BRIDGE_PORT -> 127.0.0.1:$OLLAMA_PORT"
  socat "TCP-LISTEN:$BRIDGE_PORT,reuseaddr,fork,bind=$BRIDGE_BIND" "TCP:127.0.0.1:$OLLAMA_PORT" &
  BRIDGE_PID=$!
  # Tear down only the bridge we started, when the container exits.
  trap '[[ -n "$BRIDGE_PID" ]] && kill "$BRIDGE_PID" 2>/dev/null && echo ">> Stopped socat bridge (pid $BRIDGE_PID)."' EXIT
  sleep 0.3
fi

# --- 3. Run the container with the correct call ------------------------------
# --add-host=...:host-gateway is the portable way to resolve host.docker.internal
# (Rancher Desktop, Docker Desktop, native Docker). TALUNOR_OLLAMA_URL points the
# agent's chat provider at the bridge port; the embedding model is baked into the
# image, so only chat needs Ollama.
echo ">> Running Talunor container ($RUNTIME, image $IMAGE)..."
set -x
"$RUNTIME" run --rm -it \
  --add-host=host.docker.internal:host-gateway \
  -e "TALUNOR_OLLAMA_URL=http://host.docker.internal:$BRIDGE_PORT/v1" \
  -v "$DATA_VOLUME:/data" \
  "${EXTRA_ARGS[@]}" \
  "$IMAGE"

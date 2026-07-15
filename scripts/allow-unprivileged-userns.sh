#!/usr/bin/env bash
#
# allow-unprivileged-userns.sh — toggle the AppArmor gate on unprivileged
# user namespaces, so Talunor's `namespaces` sandbox backend can run.
#
# Why this exists
#   The rootless `namespaces` bash-sandbox backend (TALUNOR_SANDBOX=namespaces)
#   creates a user namespace and writes a uid_map. On Ubuntu 24.04+ that is
#   blocked by default via the sysctl
#       kernel.apparmor_restrict_unprivileged_userns = 1
#   so the sandbox fails with a bare "operation not permitted" (EPERM) on the
#   uid_map write. Setting it to 0 lifts the restriction machine-wide.
#
#   This ONLY affects the home-made `namespaces` backend. The `nerdctl`/`docker`
#   backend (the stronger, recommended one) does not need this — if you use it,
#   you can ignore this script entirely.
#
# Security note
#   Allowing unprivileged user namespaces widens the kernel attack surface a
#   little (it is why some distros gate it). It is a common, reasonable dev
#   setting, but on a shared or hardened host prefer the nerdctl backend and
#   leave the restriction on. This script also offers --restore to put it back.
#
# Usage
#   scripts/allow-unprivileged-userns.sh            # allow (set sysctl -> 0)
#   scripts/allow-unprivileged-userns.sh --persist  # allow + persist a reboot
#   scripts/allow-unprivileged-userns.sh --restore  # re-restrict (set -> 1)
#   scripts/allow-unprivileged-userns.sh --status    # just print current state
#
set -euo pipefail

SYSCTL_KEY="kernel.apparmor_restrict_unprivileged_userns"
PERSIST_FILE="/etc/sysctl.d/99-talunor-userns.conf"

# The sysctl only exists on kernels/distros that ship the AppArmor restriction
# (Ubuntu 24.04+). Elsewhere unprivileged userns is usually already allowed.
current() {
  if [[ -r "/proc/sys/kernel/apparmor_restrict_unprivileged_userns" ]]; then
    cat /proc/sys/kernel/apparmor_restrict_unprivileged_userns
  else
    echo "absent"
  fi
}

explain() {
  local v; v="$(current)"
  case "$v" in
    0) echo ">> $SYSCTL_KEY = 0  → unprivileged user namespaces ALLOWED (namespaces backend can run)";;
    1) echo ">> $SYSCTL_KEY = 1  → unprivileged user namespaces RESTRICTED (namespaces backend will fail)";;
    absent) echo ">> $SYSCTL_KEY is not present on this host — the AppArmor gate does not apply here.";;
  esac
}

set_value() {
  local value="$1"
  if [[ "$(current)" == "absent" ]]; then
    echo "This host has no '$SYSCTL_KEY' sysctl — nothing to toggle."
    echo "If the namespaces backend still fails, check user.max_user_namespaces and"
    echo "kernel.unprivileged_userns_clone, or just use TALUNOR_SANDBOX=nerdctl."
    exit 0
  fi
  # sysctl -w needs root; re-exec under sudo if we aren't.
  if [[ "$(id -u)" -ne 0 ]]; then
    echo ">> Need root to set the sysctl; escalating with sudo..."
    exec sudo -- "$0" "${ORIG_ARGS[@]}"
  fi
  sysctl -w "$SYSCTL_KEY=$value"
}

persist() {
  local value="$1"
  echo "$SYSCTL_KEY = $value" > "$PERSIST_FILE"
  echo ">> Persisted to $PERSIST_FILE (survives reboot). Remove that file to undo."
}

main() {
  local action="allow" do_persist=0
  for arg in "${ORIG_ARGS[@]:-}"; do
    case "$arg" in
      ""|--allow)   action="allow" ;;
      --restore|--restrict) action="restore" ;;
      --status)     action="status" ;;
      --persist)    do_persist=1 ;;
      -h|--help)
        sed -n '2,33p' "$0"; exit 0 ;;
      *) echo "unknown option: $arg" >&2; exit 2 ;;
    esac
  done

  case "$action" in
    status)
      explain ;;
    allow)
      echo ">> Allowing unprivileged user namespaces (for TALUNOR_SANDBOX=namespaces)..."
      set_value 0
      [[ "$do_persist" -eq 1 ]] && persist 0
      explain
      echo ">> Verify:  TALUNOR_BASH=1 TALUNOR_SANDBOX=namespaces ./bin/talunor --plain"
      ;;
    restore)
      echo ">> Re-restricting unprivileged user namespaces (back to the distro default)..."
      set_value 1
      if [[ "$do_persist" -eq 1 ]]; then
        persist 1
      else
        [[ -f "$PERSIST_FILE" ]] && { rm -f "$PERSIST_FILE"; echo ">> Removed $PERSIST_FILE"; }
      fi
      explain
      ;;
  esac
}

# Preserve original args across the possible sudo re-exec.
ORIG_ARGS=("$@")
main

#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/install_tailscale.sh
# Compatibility entrypoint for the unified Tailscale setup flow.
# =============================================================================

set -eu

LOG_FILE="${POCKETCLI_DEBUG_LOG:-/tmp/pocketcli-debug.log}"
log_debug() {
    TS=$(date '+%Y-%m-%d %H:%M:%S' 2>/dev/null || printf 'unknown-time')
    printf '%s [install_tailscale] %s\n' "${TS}" "$*" >> "${LOG_FILE}" 2>/dev/null || true
}

OS="${1:-auto}"
POCKETCLI_DIR="${POCKETCLI_DIR:-${HOME}/.pocketcli}"
DAEMON_SCRIPT="${POCKETCLI_DIR}/scripts/tailscale_daemon.sh"

log_debug "delegating install flow os=${OS} to tailscale_daemon.sh"
[ -r "${DAEMON_SCRIPT}" ] || {
    printf '[PocketCli] tailscale setup script not found: %s\n' "${DAEMON_SCRIPT}" >&2
    exit 1
}

exec sh "${DAEMON_SCRIPT}" setup

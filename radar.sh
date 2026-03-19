#!/usr/bin/env sh
# =============================================================================
# PocketCli — radar.sh
# Lists machines available on the Tailscale network.
# =============================================================================

set -eu

LOG_FILE="${POCKETCLI_DEBUG_LOG:-/tmp/pocketcli-debug.log}"
log_debug() {
    TS=$(date '+%Y-%m-%d %H:%M:%S' 2>/dev/null || printf 'unknown-time')
    printf '%s [radar] %s\n' "${TS}" "$*" >> "${LOG_FILE}" 2>/dev/null || true
}

command -v tailscale >/dev/null 2>&1 || {
    log_debug "tailscale binary not found"
    printf '[PocketCli] tailscale not found. Is it installed?\n' >&2
    exit 1
}

# ---------------------------------------------------------------------------
# Fetch machine list from Tailscale
# ---------------------------------------------------------------------------
TS_STATUS=$(tailscale status --json 2>/dev/null) || {
    log_debug "tailscale status --json failed"
    printf '[PocketCli] Could not reach Tailscale daemon. Is it running?\n' >&2
    exit 1
}

log_debug "tailscale status collected successfully"

echo ""
echo "  ╔══════════════════════════════╗"
echo "  ║       PocketCli  Radar       ║"
echo "  ╚══════════════════════════════╝"
echo ""
printf "  %-20s %-15s %-10s\n" "Hostname" "IP" "Status"
printf "  %-20s %-15s %-10s\n" "────────────────────" "───────────────" "──────────"

# Parse with jq — safe, no eval
echo "${TS_STATUS}" \
  | jq -r '.Peer | to_entries[] | .value | "\(.HostName) \(.TailscaleIPs[0] // "n/a") \(if .Online then "online" else "offline" end)"' \
  | while IFS=' ' read -r host ip status; do
        # Sanitise fields before printing
        host=$(printf '%s' "${host}" | tr -cd 'a-zA-Z0-9._-')
        ip=$(printf '%s' "${ip}" | tr -cd '0-9.:')
        log_debug "rendering peer host=${host} ip=${ip} status=${status}"
        printf "  %-20s %-15s %-10s\n" "${host}" "${ip}" "${status}"
    done

echo ""

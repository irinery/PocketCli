#!/usr/bin/env bash
# =============================================================================
# PocketCli — radar.sh
# Lists machines available on the Tailscale network.
# =============================================================================

set -euo pipefail

command -v tailscale >/dev/null 2>&1 || {
    printf '[PocketCli] tailscale not found. Is it installed?\n' >&2
    exit 1
}

# ---------------------------------------------------------------------------
# Fetch machine list from Tailscale
# ---------------------------------------------------------------------------
TS_STATUS=$(tailscale status --json 2>/dev/null) || {
    printf '[PocketCli] Could not reach Tailscale daemon. Is it running?\n' >&2
    exit 1
}

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
        host="${host//[^a-zA-Z0-9._-]/}"
        ip="${ip//[^0-9.:]/}"
        printf "  %-20s %-15s %-10s\n" "${host}" "${ip}" "${status}"
    done

echo ""

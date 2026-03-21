#!/usr/bin/env sh
# =============================================================================
# PocketCli — radar.sh
# Lists machines available on the Tailscale network.
# =============================================================================

set -eu

POCKETCLI_DIR="${HOME}/.pocketcli"
. "${POCKETCLI_DIR}/lib/common.sh"

LOG_FILE="${POCKETCLI_DEBUG_LOG:-/tmp/pocketcli-debug.log}"
log_debug() {
    TS=$(date '+%Y-%m-%d %H:%M:%S' 2>/dev/null || printf 'unknown-time')
    printf '%s [radar] %s\n' "${TS}" "$*" >> "${LOG_FILE}" 2>/dev/null || true
}

echo ""
echo "  ╔══════════════════════════════╗"
echo "  ║       PocketCli  Radar       ║"
echo "  ╚══════════════════════════════╝"
echo ""
printf "  %-20s %-15s %-10s\n" "Hostname" "IP" "Status"
printf "  %-20s %-15s %-10s\n" "────────────────────" "───────────────" "──────────"

TS_STATUS=""
if command -v tailscale >/dev/null 2>&1 && command -v jq >/dev/null 2>&1; then
    TS_STATUS=$(with_timeout 5 tailscale status --json 2>/dev/null || true)
fi

if [ -n "${TS_STATUS}" ]; then
    log_debug "tailscale status collected successfully"
    printf '%s\n' "${TS_STATUS}" \
      | jq -r '.Peer | to_entries[] | .value | "\(.HostName) \(.TailscaleIPs[0] // "n/a") \(if .Online then "online" else "offline" end)"' \
      | while IFS=' ' read -r host ip status; do
            host=$(safe_host "${host}")
            ip=$(printf '%s' "${ip}" | tr -cd '0-9.:')
            log_debug "rendering peer host=${host} ip=${ip} status=${status}"
            printf "  %-20s %-15s %-10s\n" "${host}" "${ip}" "${status}"
        done
else
    log_debug "tailscale status unavailable, falling back to saved hosts and seeds"
    printf "  %-20s %-15s %-10s\n" "(fallback)" "-" "saved+seed"
    list_fallback_targets | while IFS= read -r host; do
        [ -z "${host}" ] && continue
        if ping_host "${host}" 2; then
            status="reachable"
        else
            status="saved"
        fi
        if is_ip_target "${host}"; then
            ip="${host}"
            if [ "${status}" = "reachable" ]; then
                status="seed-ok"
            else
                status="seed"
            fi
        else
            ip=$(resolve_tailscale_ip_for_host "${host}" || true)
            [ -z "${ip}" ] && ip="n/a"
        fi
        log_debug "rendering fallback host=${host} ip=${ip} status=${status}"
        printf "  %-20s %-15s %-10s\n" "${host}" "${ip}" "${status}"
    done
fi

echo ""

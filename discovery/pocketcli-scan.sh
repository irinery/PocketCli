#!/usr/bin/env sh
# =============================================================================
# PocketCli — discovery/pocketcli-scan.sh
# Scans Tailscale peers for docker containers and running services.
# Called by: pocket scan
# =============================================================================

set -eu

POCKETCLI_DIR="${HOME}/.pocketcli"
. "${POCKETCLI_DIR}/lib/common.sh"

_scan_host() {
    HOST="$1"
    HOST=$(safe_host "${HOST}")
    [ -z "${HOST}" ] && return 1

    printf "\n  ${C_BOLD}── %s${C_NC}\n" "${HOST}"

    # Test reachability first — -n prevents stdin swallow in loops
    if ! ssh -n -o ConnectTimeout=3 -o BatchMode=yes \
            "${HOST}" "command -v docker" >/dev/null 2>&1; then
        printf "    ${C_DIM}no docker / unreachable${C_NC}\n"
        return 0
    fi

    # Fetch running containers — -n required: called from while-read loop
    CONTAINERS=$(ssh -n -o ConnectTimeout=5 -o BatchMode=yes \
        "${HOST}" "docker ps --format '{{.Names}}'" 2>/dev/null || true)

    if [ -z "${CONTAINERS}" ]; then
        printf "    ${C_DIM}docker running — no containers${C_NC}\n"
    else
        printf '%s\n' "${CONTAINERS}" | while IFS= read -r c; do
            printf "    ${C_GREEN}▸${C_NC} %s\n" "$(printf '%s' "${c}" | tr -cd 'a-zA-Z0-9./_-')"
        done
    fi
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
echo ""
printf "  ${C_BOLD}PocketCli — Infrastructure Scan${C_NC}\n"
echo ""

HOSTS=$(list_known_hosts)

[ -z "${HOSTS}" ] && { warn "No online Tailscale machines found."; exit 0; }

COUNT=$(printf '%s\n' "${HOSTS}" | wc -l | tr -d ' ')
if [ -n "$(list_online_tailscale_hosts)" ]; then
    info "Scanning ${COUNT} machine(s) from Tailscale discovery..."
else
    warn "Tailscale discovery unavailable — scanning ${COUNT} saved host(s)."
fi

printf '%s\n' "${HOSTS}" | while IFS= read -r host; do
    [ -z "${host}" ] && continue
    _scan_host "${host}" &
done
wait

echo ""
ok "Scan complete."
echo ""

#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/fleet.sh
# Run commands across multiple Tailscale machines in parallel.
#
# Usage (called via 'pocket fleet'):
#   pocket fleet run    <cmd>         Run on ALL online machines
#   pocket fleet run    <cmd> --pick  Pick machines interactively with fzf
#   pocket fleet status               Show status of all machines
#   pocket fleet list                 List all reachable machines
# =============================================================================

set -eu

LOG_FILE="${POCKETCLI_DEBUG_LOG:-/tmp/pocketcli-debug.log}"
log_debug() {
    TS=$(date '+%Y-%m-%d %H:%M:%S' 2>/dev/null || printf 'unknown-time')
    printf '%s [fleet] %s\n' "${TS}" "$*" >> "${LOG_FILE}" 2>/dev/null || true
}

# ---------------------------------------------------------------------------
# Colours
# ---------------------------------------------------------------------------
BOLD='\033[1m'
CYAN='\033[0;36m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
DIM='\033[2m'
NC='\033[0m'

_info() { printf "${CYAN}▸${NC} %s\n" "$*"; }
_ok()   { printf "${GREEN}✔${NC} %s\n" "$*"; }
_warn() { printf "${YELLOW}!${NC} %s\n" "$*"; }
_die()  { printf "${RED}✘${NC} %s\n" "$*" >&2; exit 1; }

_require() { command -v "$1" >/dev/null 2>&1 || _die "$1 is required."; }

# ---------------------------------------------------------------------------
# Get online Tailscale hosts as newline-separated list
# ---------------------------------------------------------------------------
_online_hosts() {
    _require tailscale
    _require jq
    log_debug "collecting online hosts from tailscale"
    tailscale status --json 2>/dev/null \
        | jq -r '.Peer | to_entries[] | .value | select(.Online) | .HostName' \
        | sort \
        | while IFS= read -r h; do
            log_debug "found online host raw=${h}"
            printf '%s\n' "$(printf '%s' "${h}" | tr -cd 'a-zA-Z0-9._-')"
          done
}

# ---------------------------------------------------------------------------
# Pick hosts interactively with fzf (multi-select with TAB)
# ---------------------------------------------------------------------------
_pick_hosts() {
    _require fzf
    all=""
    all=$(_online_hosts)
    [ -z "${all}" ] && _die "No online machines found on Tailscale."
    log_debug "opening interactive picker"
    echo "${all}" | fzf \
        --multi \
        --prompt="  Select machines (TAB to multi-select) » " \
        --height=60% \
        --no-info \
        --header="TAB = toggle  |  Enter = confirm"
}

# ---------------------------------------------------------------------------
# Run a command on a single host, prefixing all output with the hostname
# ---------------------------------------------------------------------------
_run_on_host() {
    host="${1}"
    cmd="${2}"
    tmp_output=""

    # Sanitise host one more time at execution boundary
    host=$(printf '%s' "${host}" | tr -cd 'a-zA-Z0-9._-')
    [ -z "${host}" ] && return 1

    {
        tmp_output=$(mktemp)
        log_debug "running remote command host=${host} cmd=${cmd}"
        ssh -n \
            -o StrictHostKeyChecking=accept-new \
            -o ConnectTimeout=10 \
            -o BatchMode=yes \
            "${host}" -- "${cmd}" >"${tmp_output}" 2>&1
        exit_code=$?
        while IFS= read -r line; do
                printf "${BOLD}[${CYAN}%s${NC}${BOLD}]${NC} %s\n" "${host}" "${line}"
            done < "${tmp_output}"
        rm -f "${tmp_output}"
        log_debug "remote command finished host=${host} exit=${exit_code}"
        if [ "${exit_code}" -eq 0 ]; then
            printf "${GREEN}[✔ %s]${NC}\n" "${host}"
        else
            printf "${RED}[✘ %s — exit %s]${NC}\n" "${host}" "${exit_code}"
        fi
    } &   # run in background for parallelism
}

# ---------------------------------------------------------------------------
# Subcommand: fleet run
# ---------------------------------------------------------------------------
_fleet_run() {
    [ $# -lt 1 ] && _die "Usage: pocket fleet run <command> [--pick]"

    pick=0
    cmd=""

    # Parse args
    for arg in "$@"; do
        case "${arg}" in
            --pick) pick=1 ;;
            *)      cmd="${cmd} ${arg}" ;;
        esac
    done
    cmd="${cmd# }"   # strip leading space
    [ -z "${cmd}" ] && _die "No command specified."

    # Get target hosts
    hosts=""
    if [ "${pick}" -eq 1 ]; then
        log_debug "fleet run using interactive selection"
        hosts=$(_pick_hosts) || exit 0
    else
        log_debug "fleet run using all online hosts"
        hosts=$(_online_hosts)
    fi

    [ -z "${hosts}" ] && _die "No hosts to run on."

    count=""
    count=$(echo "${hosts}" | wc -l | tr -d ' ')
    log_debug "fleet run target_count=${count}"

    echo ""
    _info "Running on ${count} machine(s): ${BOLD}${cmd}${NC}"
    echo ""

    # Dispatch in parallel
    while IFS= read -r host; do
        [ -z "${host}" ] && continue
        _run_on_host "${host}" "${cmd}"
    done <<EOF
${hosts}
EOF

    # Wait for all background jobs
    wait
    echo ""
    _ok "Fleet run complete."
    echo ""
}

# ---------------------------------------------------------------------------
# Subcommand: fleet status
# ---------------------------------------------------------------------------
_fleet_status() {
    hosts=""
    hosts=$(_online_hosts)
    [ -z "${hosts}" ] && _die "No online machines found."

    count=""
    count=$(echo "${hosts}" | wc -l | tr -d ' ')
    log_debug "fleet status target_count=${count}"

    echo ""
    printf "  ${BOLD}╔════════════════════════════════════════╗${NC}\n"
    printf "  ${BOLD}║         PocketCli  Fleet Status        ║${NC}\n"
    printf "  ${BOLD}╚════════════════════════════════════════╝${NC}\n"
    echo ""
    _info "Collecting status from ${count} machine(s)..."
    echo ""

    # Run pocket-status remotely on each machine in parallel
    while IFS= read -r host; do
        [ -z "${host}" ] && continue
        {
            echo "  ${BOLD}── ${CYAN}${host}${NC} ──"
            ssh -n \
                -o StrictHostKeyChecking=accept-new \
                -o ConnectTimeout=10 \
                -o BatchMode=yes \
                "${host}" -- \
                "${HOME}/.pocketcli/scripts/pocket-status.sh 2>/dev/null || echo '  (pocket-status not available on this host)'" \
                2>&1
            echo ""
        } &
    done <<EOF
${hosts}
EOF

    wait
}

# ---------------------------------------------------------------------------
# Subcommand: fleet list
# ---------------------------------------------------------------------------
_fleet_list() {
    # Delegates to `pocket list`
    exec "${HOME}/.pocketcli/pocket" list
}

# ---------------------------------------------------------------------------
# Subcommand: fleet help
# ---------------------------------------------------------------------------
_fleet_help() {
    echo ""
    printf "  ${BOLD}pocket fleet${NC}  ${DIM}— run commands across your infrastructure${NC}\n"
    echo ""
    printf "  ${BOLD}Usage:${NC}  pocket fleet <subcommand> [args]\n"
    echo ""
    printf "  ${CYAN}run${NC}    <cmd>          Run command on ALL online machines\n"
    printf "  ${CYAN}run${NC}    <cmd> --pick   Pick machines interactively with fzf\n"
    printf "  ${CYAN}status${NC}                Collect pocket-status from all machines\n"
    printf "  ${CYAN}list${NC}                  List all online Tailscale machines\n"
    echo ""
    printf "  ${DIM}Examples:${NC}\n"
    printf "  ${DIM}  pocket fleet run \"uptime\"${NC}\n"
    printf "  ${DIM}  pocket fleet run \"docker pull myapp:latest\" --pick${NC}\n"
    printf "  ${DIM}  pocket fleet run \"git -C /app pull && docker compose up -d\"${NC}\n"
    printf "  ${DIM}  pocket fleet status${NC}\n"
    echo ""
}

# ---------------------------------------------------------------------------
# Dispatch
# ---------------------------------------------------------------------------
SUB="${1:-help}"
shift 2>/dev/null || true

case "${SUB}" in
    run)    _fleet_run    "$@" ;;
    status) _fleet_status      ;;
    list)   _fleet_list        ;;
    help|-h|--help|"") _fleet_help ;;
    *)
        _warn "Unknown fleet subcommand: '${SUB}'"
        _fleet_help
        exit 1
    ;;
esac

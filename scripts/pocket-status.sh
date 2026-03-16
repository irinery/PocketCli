#!/usr/bin/env bash
# =============================================================================
# PocketCli — scripts/pocket-status.sh
# Shows the local node's status in a clean TUI panel.
# Safe: uses jq for JSON, no eval, all variables quoted.
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Colours
# ---------------------------------------------------------------------------
BOLD='\033[1m'
CYAN='\033[0;36m'
DIM='\033[2m'
NC='\033[0m'

_row() { printf "  ${CYAN}%-18s${NC} %s\n" "${1}" "${2}"; }

# ---------------------------------------------------------------------------
# Collect metrics — all sanitised before display
# ---------------------------------------------------------------------------

# Hostname — strip everything except safe chars
HOST=$(hostname | tr -cd '[:alnum:]-.')

# OS
if [ -f /etc/os-release ]; then
    OS_NAME=$(. /etc/os-release && printf '%s %s' "${NAME:-Linux}" "${VERSION_ID:-}")
elif [ "$(uname)" = "Darwin" ]; then
    OS_NAME="macOS $(sw_vers -productVersion 2>/dev/null || echo '')"
else
    OS_NAME="$(uname -s) $(uname -r)"
fi
# Sanitise to printable ASCII
OS_NAME=$(printf '%s' "${OS_NAME}" | tr -cd '[:print:]')

# Uptime
UPTIME=$(uptime -p 2>/dev/null || uptime | sed 's/.*up /up /' | cut -d',' -f1-2)
UPTIME=$(printf '%s' "${UPTIME}" | tr -cd '[:print:]')

# CPU load (1 min average)
CPU_LOAD=$(uptime | awk -F'load average[s]*:' '{print $2}' | cut -d',' -f1 | tr -d ' ')
CPU_LOAD=$(printf '%s' "${CPU_LOAD}" | tr -cd '0-9.')

# Memory (Linux only; gracefully skipped on macOS)
if command -v free >/dev/null 2>&1; then
    read -r MEM_USED MEM_TOTAL < <(
        free -m | awk '/^Mem:/ {printf "%s %s", $3, $2}'
    )
    MEM_INFO="${MEM_USED}MB / ${MEM_TOTAL}MB"
else
    # macOS: use vm_stat
    PAGES_FREE=$(vm_stat 2>/dev/null | awk '/Pages free/ {gsub(/\./,"",$3); print $3}')
    PAGES_ACTIVE=$(vm_stat 2>/dev/null | awk '/Pages active/ {gsub(/\./,"",$3); print $3}')
    if [ -n "${PAGES_FREE}" ] && [ -n "${PAGES_ACTIVE}" ]; then
        MEM_FREE_MB=$(( PAGES_FREE * 4096 / 1024 / 1024 ))
        MEM_ACTIVE_MB=$(( PAGES_ACTIVE * 4096 / 1024 / 1024 ))
        MEM_INFO="${MEM_ACTIVE_MB}MB active | ${MEM_FREE_MB}MB free"
    else
        MEM_INFO="n/a"
    fi
fi

# Disk usage on /
DISK=$(df -h / 2>/dev/null | awk 'NR==2 {print $3 " used / " $2 " total (" $5 ")"}')
DISK=$(printf '%s' "${DISK}" | tr -cd '[:print:]')

# Docker containers (optional)
if command -v docker >/dev/null 2>&1; then
    CONTAINERS=$(docker ps -q 2>/dev/null | wc -l | tr -d ' ')
    CONTAINERS_INFO="${CONTAINERS} running"
else
    CONTAINERS_INFO="docker not installed"
fi

# Tailscale IP (optional)
if command -v tailscale >/dev/null 2>&1; then
    TS_IP=$(tailscale ip -4 2>/dev/null | head -1 | tr -cd '0-9.')
    TS_INFO="${TS_IP:-not connected}"
else
    TS_INFO="tailscale not installed"
fi

# PocketCli version
POCKET_VER=$(git -C "${HOME}/.pocketcli" describe --tags --always 2>/dev/null || echo "dev")

# Timestamp
TIMESTAMP=$(date '+%Y-%m-%d %H:%M:%S')

# ---------------------------------------------------------------------------
# Render
# ---------------------------------------------------------------------------
echo ""
printf "  ${BOLD}╔══════════════════════════════════════╗${NC}\n"
printf "  ${BOLD}║         PocketCli  Status            ║${NC}\n"
printf "  ${BOLD}╚══════════════════════════════════════╝${NC}\n"
echo ""

_row "Hostname"    "${HOST}"
_row "OS"          "${OS_NAME}"
_row "Uptime"      "${UPTIME}"
echo ""
_row "CPU load"    "${CPU_LOAD}"
_row "Memory"      "${MEM_INFO}"
_row "Disk (/)"    "${DISK}"
echo ""
_row "Containers"  "${CONTAINERS_INFO}"
_row "Tailscale"   "${TS_INFO}"
echo ""
_row "PocketCli"   "${POCKET_VER}"
printf "  ${DIM}%-18s %s${NC}\n" "Checked at" "${TIMESTAMP}"
echo ""

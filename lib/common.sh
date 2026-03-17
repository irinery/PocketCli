#!/usr/bin/env sh
# =============================================================================
# PocketCli — lib/common.sh
# Shared helpers. Source with: . "$POCKETCLI_DIR/lib/common.sh"
# POSIX sh only.
# =============================================================================

# ---------------------------------------------------------------------------
# Colours
# ---------------------------------------------------------------------------
if [ -t 1 ]; then
    C_BOLD='\033[1m'; C_CYAN='\033[0;36m'; C_GREEN='\033[0;32m'
    C_YELLOW='\033[1;33m'; C_RED='\033[0;31m'; C_NC='\033[0m'
    # shellcheck disable=SC2034
    C_DIM='\033[2m'
else
    C_BOLD=''; C_CYAN=''; C_GREEN=''; C_YELLOW=''
    # shellcheck disable=SC2034
    C_RED=''; C_DIM=''; C_NC=''
fi

info()  { printf "${C_CYAN}[*]${C_NC} %s\n"   "$*"; }
ok()    { printf "${C_GREEN}[✔]${C_NC} %s\n"  "$*"; }
warn()  { printf "${C_YELLOW}[!]${C_NC} %s\n" "$*"; }
die()   { printf "${C_RED}[✘]${C_NC} %s\n"    "$*" >&2; exit 1; }
step()  { printf "\n${C_BOLD}──  %s${C_NC}\n"  "$*"; }

run_or_warn() { DESC="$1"; shift; "$@" >/dev/null 2>&1 && ok "${DESC}" || warn "${DESC} — skipped"; }
run_or_die()  { DESC="$1"; shift; "$@" >/dev/null 2>&1 && ok "${DESC}" || die "${DESC} — FAILED"; }

with_timeout() {
    SECS="$1"; shift
    if command -v timeout >/dev/null 2>&1; then
        timeout "${SECS}" "$@"; return $?
    fi
    "$@" & PID=$!
    ( sleep "${SECS}"; kill "${PID}" 2>/dev/null ) &
    GUARD=$!
    wait "${PID}" 2>/dev/null; RC=$?
    kill "${GUARD}" 2>/dev/null; wait "${GUARD}" 2>/dev/null || true
    return ${RC}
}

require()   { command -v "$1" >/dev/null 2>&1 || die "'$1' is required but not installed."; }
safe_host() { printf '%s' "$1" | tr -cd 'a-zA-Z0-9._-'; }

confirm() {
    printf '%s [y/N] ' "$1"
    read -r _CONF < /dev/tty
    case "${_CONF}" in y|Y|yes|YES) return 0 ;; *) return 1 ;; esac
}

# ---------------------------------------------------------------------------
# is_ish — true if running inside iSH on iPad
# ---------------------------------------------------------------------------
is_ish() {
    [ -f /proc/ish ] && return 0
    uname -r 2>/dev/null | grep -qi 'ish' && return 0
    # iSH Alpine reports kernel 4.x on x86
    [ -f /etc/alpine-release ] && uname -r 2>/dev/null | grep -q '^4\.' && return 0
    return 1
}

# ---------------------------------------------------------------------------
# is_tailscale_daemon_running
# Always false on iSH — kernel doesn't support netlink, daemon can't run.
# ---------------------------------------------------------------------------
is_tailscale_daemon_running() {
    is_ish && return 1
    pgrep tailscaled >/dev/null 2>&1 && return 0
    return 1
}

# ---------------------------------------------------------------------------
# is_on_tailscale_network
# True if this device has a Tailscale IP (100.x.x.x) — even without daemon.
# On iSH, the iOS Tailscale app provides the VPN; no CLI needed.
# ---------------------------------------------------------------------------
is_on_tailscale_network() {
    # Method 1: tailscale CLI available and daemon responding
    if command -v tailscale >/dev/null 2>&1; then
        TS_IP=$(with_timeout 3 tailscale ip -4 2>/dev/null | head -1 | tr -cd '0-9.' || true)
        [ -n "${TS_IP}" ] && return 0
    fi
    # Method 2: look for a 100.x.x.x address on any interface (iOS app VPN)
    if ip addr 2>/dev/null | grep -q '100\.[0-9]\+\.[0-9]\+\.[0-9]\+'; then
        return 0
    fi
    if ifconfig 2>/dev/null | grep -q '100\.[0-9]\+\.[0-9]\+\.[0-9]\+'; then
        return 0
    fi
    return 1
}

# ---------------------------------------------------------------------------
# get_tailscale_ip — prints 100.x.x.x or empty
# ---------------------------------------------------------------------------
get_tailscale_ip() {
    if command -v tailscale >/dev/null 2>&1; then
        with_timeout 3 tailscale ip -4 2>/dev/null | head -1 | tr -cd '0-9.' && return 0
    fi
    # Fallback: parse network interfaces
    if command -v ip >/dev/null 2>&1; then
        ip addr 2>/dev/null \
            | grep -o '100\.[0-9]\+\.[0-9]\+\.[0-9]\+' | head -1
    else
        ifconfig 2>/dev/null \
            | grep -o '100\.[0-9]\+\.[0-9]\+\.[0-9]\+' | head -1
    fi
}

# ---------------------------------------------------------------------------
# ping_host <host> [timeout_sec]
# Quick reachability check — no tailscale CLI needed.
# ---------------------------------------------------------------------------
ping_host() {
    HOST="$1"; WAIT="${2:-3}"
    with_timeout "${WAIT}" ping -c 1 -W "${WAIT}" "${HOST}" >/dev/null 2>&1
}

# ---------------------------------------------------------------------------
# wait_for_tailscale <max_seconds>  (only useful when daemon is running)
# ---------------------------------------------------------------------------
wait_for_tailscale() {
    MAX="${1:-15}"; I=0
    while [ "${I}" -lt "${MAX}" ]; do
        tailscale status >/dev/null 2>&1 && return 0
        printf '.'; sleep 1; I=$((I + 1))
    done
    printf '\n'; return 1
}

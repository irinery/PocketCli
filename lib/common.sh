#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/lib/common.sh
# Shared helpers. Source with: . "$POCKETCLI_DIR/scripts/lib/common.sh"
# POSIX sh only. No bash. No pipefail (not POSIX).
# =============================================================================

# ---------------------------------------------------------------------------
# Colours — degrade gracefully if terminal doesn't support them
# ---------------------------------------------------------------------------
if [ -t 1 ]; then
    C_BOLD='\033[1m'
    C_CYAN='\033[0;36m'
    C_GREEN='\033[0;32m'
    C_YELLOW='\033[1;33m'
    C_RED='\033[0;31m'
    C_DIM='\033[2m'
    C_NC='\033[0m'
else
    C_BOLD=''; C_CYAN=''; C_GREEN=''; C_YELLOW=''
    C_RED='';  C_DIM='';  C_NC=''
fi

info()    { printf "${C_CYAN}[*]${C_NC} %s\n"   "$*"; }
ok()      { printf "${C_GREEN}[✔]${C_NC} %s\n"  "$*"; }
warn()    { printf "${C_YELLOW}[!]${C_NC} %s\n" "$*"; }
die()     { printf "${C_RED}[✘]${C_NC} %s\n"    "$*" >&2; exit 1; }
step()    { printf "\n${C_BOLD}──  %s${C_NC}\n"  "$*"; }

# ---------------------------------------------------------------------------
# run_or_warn <description> <command> [args...]
# Runs command; on failure prints warning but does NOT exit.
# ---------------------------------------------------------------------------
run_or_warn() {
    DESC="$1"; shift
    if "$@" >/dev/null 2>&1; then
        ok "${DESC}"
        return 0
    else
        warn "${DESC} — skipped (not critical)"
        return 1
    fi
}

# ---------------------------------------------------------------------------
# run_or_die <description> <command> [args...]
# Runs command; on failure prints error and exits.
# ---------------------------------------------------------------------------
run_or_die() {
    DESC="$1"; shift
    if "$@" >/dev/null 2>&1; then
        ok "${DESC}"
        return 0
    else
        die "${DESC} — FAILED. Cannot continue."
    fi
}

# ---------------------------------------------------------------------------
# with_timeout <seconds> <command> [args...]
# Runs command with a timeout. Kills it if it hangs.
# Uses 'timeout' if available, otherwise kills after N seconds via subshell.
# ---------------------------------------------------------------------------
with_timeout() {
    SECS="$1"; shift
    if command -v timeout >/dev/null 2>&1; then
        timeout "${SECS}" "$@"
        return $?
    fi
    # Fallback: background + sleep kill
    "$@" &
    PID=$!
    (sleep "${SECS}"; kill "${PID}" 2>/dev/null) &
    GUARD=$!
    wait "${PID}" 2>/dev/null
    RC=$?
    kill "${GUARD}" 2>/dev/null
    wait "${GUARD}" 2>/dev/null || true
    return ${RC}
}

# ---------------------------------------------------------------------------
# require <cmd>
# Dies with helpful message if command not found.
# ---------------------------------------------------------------------------
require() {
    command -v "$1" >/dev/null 2>&1 && return 0
    die "'$1' is required but not installed. Install it and re-run."
}

# ---------------------------------------------------------------------------
# safe_host <string>
# Strips all non-hostname characters.
# ---------------------------------------------------------------------------
safe_host() { printf '%s' "$1" | tr -cd 'a-zA-Z0-9._-'; }

# ---------------------------------------------------------------------------
# confirm <prompt>
# Returns 0 on y/Y, 1 otherwise. Always reads from /dev/tty.
# ---------------------------------------------------------------------------
confirm() {
    printf '%s [y/N] ' "$1"
    read -r _CONF < /dev/tty
    case "${_CONF}" in
        y|Y|yes|YES) return 0 ;;
        *)            return 1 ;;
    esac
}

# ---------------------------------------------------------------------------
# is_ish
# Returns 0 if running inside iSH (iPad).
# ---------------------------------------------------------------------------
is_ish() {
    [ -f /proc/ish ] && return 0
    uname -r 2>/dev/null | grep -qi 'ish' && return 0
    # iSH reports a very old kernel version on Alpine
    [ -f /etc/alpine-release ] && uname -r 2>/dev/null | grep -q '^4\.' && return 0
    return 1
}

# ---------------------------------------------------------------------------
# is_tailscale_daemon_running
# ---------------------------------------------------------------------------
is_tailscale_daemon_running() {
    pgrep -x tailscaled >/dev/null 2>&1 && return 0
    # Some builds name it differently
    pgrep tailscaled >/dev/null 2>&1 && return 0
    return 1
}

# ---------------------------------------------------------------------------
# wait_for_tailscale <max_seconds>
# Waits for tailscaled to become reachable.
# ---------------------------------------------------------------------------
wait_for_tailscale() {
    MAX="${1:-15}"
    I=0
    while [ "${I}" -lt "${MAX}" ]; do
        if tailscale status >/dev/null 2>&1; then
            return 0
        fi
        printf '.'
        sleep 1
        I=$((I + 1))
    done
    printf '\n'
    return 1
}

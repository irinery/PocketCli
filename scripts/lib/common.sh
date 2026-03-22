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
    C_RED=''; C_NC=''
    # shellcheck disable=SC2034
    C_DIM=''
fi

info()  { printf "${C_CYAN}[*]${C_NC} %s\n"   "$*"; }
ok()    { printf "${C_GREEN}[✔]${C_NC} %s\n"  "$*"; }
warn()  { printf "${C_YELLOW}[!]${C_NC} %s\n" "$*"; }
die()   { printf "${C_RED}[✘]${C_NC} %s\n"    "$*" >&2; exit 1; }
step()  { printf "\n${C_BOLD}──  %s${C_NC}\n"  "$*"; }

explain_enabled() { [ "${POCKETCLI_EXPLAIN:-0}" = "1" ]; }
explain() { explain_enabled && printf "${C_DIM}[explain]${C_NC} %s\n" "$*"; }
explain_step() { explain_enabled && printf "${C_DIM}[explain]${C_NC} ---- %s ----\n" "$*"; }
explain_kv() { explain_enabled && printf "${C_DIM}[explain]${C_NC} %s=%s\n" "$1" "${2:-}"; }
explain_block() {
    if ! explain_enabled; then
        return 0
    fi
    LABEL="$1"
    VALUE="${2:-}"
    printf "${C_DIM}[explain]${C_NC} %s:\n" "${LABEL}"
    printf '%s\n' "${VALUE}" | while IFS= read -r _line; do
        printf "${C_DIM}[explain]${C_NC}   %s\n" "${_line}"
    done
}

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

_saved_hosts_file() {
    printf '%s/.pocketcli/hosts' "${HOME}"
}

_fallback_seeds_file() {
    printf '%s/.pocketcli/fallback_seeds' "${HOME}"
}

_read_targets_file() {
    TARGETS_FILE="$1"
    if [ ! -f "${TARGETS_FILE}" ]; then
        return 0
    fi

    grep -v '^[[:space:]]*#' "${TARGETS_FILE}" 2>/dev/null \
        | grep -v '^[[:space:]]*$' \
        | while IFS= read -r h; do
            h=$(safe_host "${h}")
            [ -n "${h}" ] && printf '%s\n' "${h}"
        done
}

save_known_target() {
    TARGET=$(safe_host "${1:-}")
    TARGET_FILE="${2:-$(_saved_hosts_file)}"
    [ -z "${TARGET}" ] && return 1

    mkdir -p "$(dirname "${TARGET_FILE}")"
    if [ -f "${TARGET_FILE}" ] && grep -Fx "${TARGET}" "${TARGET_FILE}" >/dev/null 2>&1; then
        return 0
    fi
    printf '%s\n' "${TARGET}" >> "${TARGET_FILE}"
}

list_saved_hosts() {
    _read_targets_file "$(_saved_hosts_file)"
}

list_fallback_seeds() {
    _read_targets_file "$(_fallback_seeds_file)"
}

list_fallback_targets() {
    {
        list_saved_hosts
        list_fallback_seeds
    } | awk 'NF && !seen[$0]++'
}

list_online_tailscale_hosts() {
    if ! command -v tailscale >/dev/null 2>&1 || ! command -v jq >/dev/null 2>&1; then
        return 0
    fi

    TS_STATUS=$(with_timeout 5 tailscale status --json 2>/dev/null || true)
    [ -z "${TS_STATUS}" ] && return 0

    printf '%s\n' "${TS_STATUS}" \
        | jq -r '.Peer | to_entries[] | .value | select(.Online) | .HostName' 2>/dev/null \
        | sort \
        | while IFS= read -r h; do
            h=$(safe_host "${h}")
            [ -n "${h}" ] && printf '%s\n' "${h}"
        done
}

list_known_hosts() {
    HOSTS=$(list_online_tailscale_hosts)
    if [ -n "${HOSTS}" ]; then
        printf '%s\n' "${HOSTS}"
        return 0
    fi

    list_fallback_targets
}

is_ip_target() {
    printf '%s' "${1:-}" | grep -Eq '^([0-9]{1,3}\.){3}[0-9]{1,3}$'
}

resolve_tailscale_ip_for_host() {
    HOST=$(safe_host "${1:-}")
    [ -z "${HOST}" ] && return 0
    if ! command -v tailscale >/dev/null 2>&1 || ! command -v jq >/dev/null 2>&1; then
        return 0
    fi

    TS_STATUS=$(with_timeout 5 tailscale status --json 2>/dev/null || true)
    [ -z "${TS_STATUS}" ] && return 0

    printf '%s\n' "${TS_STATUS}" \
        | jq -r --arg host "${HOST}" '.Peer | to_entries[] | .value | select(.HostName == $host) | .TailscaleIPs[0] // empty' 2>/dev/null \
        | head -1 \
        | tr -cd '0-9.:'
}

# ---------------------------------------------------------------------------
# ping_host <host> [timeout_sec]
# Quick reachability check — no tailscale CLI needed.
# ---------------------------------------------------------------------------
ping_host() {
    HOST="$1"; WAIT="${2:-3}"
    IP=$(resolve_tailscale_ip_for_host "${HOST}" || true)

    if with_timeout "${WAIT}" ping -c 1 -W "${WAIT}" "${HOST}" >/dev/null 2>&1; then
        return 0
    fi
    if with_timeout "${WAIT}" ping -c 1 -w "${WAIT}" "${HOST}" >/dev/null 2>&1; then
        return 0
    fi
    if with_timeout "${WAIT}" ping -c 1 "${HOST}" >/dev/null 2>&1; then
        return 0
    fi
    if [ -n "${IP}" ] && with_timeout "${WAIT}" ping -c 1 -W "${WAIT}" "${IP}" >/dev/null 2>&1; then
        return 0
    fi
    if [ -n "${IP}" ] && with_timeout "${WAIT}" ping -c 1 -w "${WAIT}" "${IP}" >/dev/null 2>&1; then
        return 0
    fi
    if [ -n "${IP}" ] && with_timeout "${WAIT}" ping -c 1 "${IP}" >/dev/null 2>&1; then
        return 0
    fi
    if command -v ssh >/dev/null 2>&1; then
        if with_timeout "${WAIT}" ssh -n -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout="${WAIT}" "${HOST}" true >/dev/null 2>&1; then
            return 0
        fi
        if [ -n "${IP}" ] && with_timeout "${WAIT}" ssh -n -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o ConnectTimeout="${WAIT}" "${IP}" true >/dev/null 2>&1; then
            return 0
        fi
    fi
    return 1
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

# ---------------------------------------------------------------------------
# PocketCli session persistence helpers
# Stores the last interactive invocation so `pocket` can recreate it after
# iSH/iPad resets or low-memory evictions.
# ---------------------------------------------------------------------------
pocket_state_dir() {
    printf '%s/.pocketcli/state' "${HOME}"
}

pocket_last_command_file() {
    printf '%s/last-command' "$(pocket_state_dir)"
}

pocket_tmux_session() {
    printf '%s' "${POCKETCLI_TMUX_SESSION:-pocketcli}"
}

pocket_save_command() {
    mkdir -p "$(pocket_state_dir)"
    : > "$(pocket_last_command_file)"
    for _arg in "$@"; do
        printf '%s\n' "${_arg}" >> "$(pocket_last_command_file)"
    done
}

pocket_load_command() {
    FILE=$(pocket_last_command_file)
    if [ ! -f "${FILE}" ] || [ ! -s "${FILE}" ]; then
        printf 'menu\n'
        return 0
    fi
    cat "${FILE}"
}

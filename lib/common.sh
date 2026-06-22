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
explain() {
    explain_enabled || return 0
    printf "${C_DIM}[explain]${C_NC} %s\n" "$*"
}
explain_step() {
    explain_enabled || return 0
    printf "${C_DIM}[explain]${C_NC} ---- %s ----\n" "$*"
}
explain_kv() {
    explain_enabled || return 0
    printf "${C_DIM}[explain]${C_NC} %s=%s\n" "$1" "${2:-}"
}
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

run_or_warn() {
    DESC="$1"
    shift

    if "$@" >/dev/null 2>&1; then
        ok "${DESC}"
    else
        warn "${DESC} — skipped"
    fi
}

run_or_die() {
    DESC="$1"
    shift

    if "$@" >/dev/null 2>&1; then
        ok "${DESC}"
    else
        die "${DESC} — FAILED"
    fi
}

with_timeout() {
    SECS="$1"; shift
    if command -v timeout >/dev/null 2>&1; then
        timeout "${SECS}" "$@"; return $?
    fi
    if command -v python3 >/dev/null 2>&1; then
        python3 - "$SECS" "$@" <<'PY'
import subprocess
import sys

timeout = int(sys.argv[1])
command = sys.argv[2:]
proc = subprocess.Popen(command)
try:
    raise SystemExit(proc.wait(timeout=timeout))
except subprocess.TimeoutExpired:
    proc.kill()
    proc.wait()
    raise SystemExit(124)
PY
        return $?
    fi
    "$@" & PID=$!
    ( sleep "${SECS}"; kill "${PID}" 2>/dev/null ) &
    GUARD=$!
    wait "${PID}" 2>/dev/null; RC=$?
    kill "${GUARD}" 2>/dev/null || true
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
    if [ -f /proc/ish ]; then
        return 0
    fi
    if uname -r 2>/dev/null | grep -qi 'ish'; then
        return 0
    fi
    # iSH Alpine reports kernel 4.x on x86
    if [ -f /etc/alpine-release ]; then
        if uname -r 2>/dev/null | grep -q '^4\.'; then
            return 0
        fi
    fi
    return 1
}

# ---------------------------------------------------------------------------
# Tailscale runtime detection
#
# macOS GUI builds do not expose a process named tailscaled, and Windows runs
# it as a service. The CLI can also live outside PATH. Treat the backend/mesh
# as the source of truth and keep process inspection only as a last fallback.
# ---------------------------------------------------------------------------
tailscale_cli_path() {
    if [ -n "${POCKETCLI_TAILSCALE_CLI:-}" ]; then
        if command -v "${POCKETCLI_TAILSCALE_CLI}" >/dev/null 2>&1; then
            command -v "${POCKETCLI_TAILSCALE_CLI}"
            return 0
        fi
        if [ -x "${POCKETCLI_TAILSCALE_CLI}" ]; then
            printf '%s\n' "${POCKETCLI_TAILSCALE_CLI}"
            return 0
        fi
        return 1
    fi

    if command -v tailscale >/dev/null 2>&1; then
        command -v tailscale
        return 0
    fi
    if command -v tailscale.exe >/dev/null 2>&1; then
        command -v tailscale.exe
        return 0
    fi

    for TS_CLI_CANDIDATE in \
        '/Applications/Tailscale.app/Contents/MacOS/Tailscale' \
        '/mnt/c/Program Files/Tailscale/tailscale.exe' \
        '/c/Program Files/Tailscale/tailscale.exe' \
        '/cygdrive/c/Program Files/Tailscale/tailscale.exe'
    do
        if [ -x "${TS_CLI_CANDIDATE}" ]; then
            printf '%s\n' "${TS_CLI_CANDIDATE}"
            return 0
        fi
    done

    if [ -n "${LOCALAPPDATA:-}" ] && [ -x "${LOCALAPPDATA}/Tailscale/tailscale.exe" ]; then
        printf '%s\n' "${LOCALAPPDATA}/Tailscale/tailscale.exe"
        return 0
    fi
    return 1
}

has_tailscale_cli() {
    tailscale_cli_path >/dev/null 2>&1
}

tailscale_cli() {
    TS_CLI=$(tailscale_cli_path) || return 127
    TAILSCALE_BE_CLI=1 "${TS_CLI}" "$@"
}

tailscale_status_json() {
    TS_CLI=$(tailscale_cli_path) || return 1
    with_timeout 5 env TAILSCALE_BE_CLI=1 "${TS_CLI}" status --json
}

is_tailscale_ipv4() {
    TS_IPV4=${1:-}
    printf '%s\n' "${TS_IPV4}" | awk -F. '
        $0 ~ /^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$/ &&
        NF == 4 && $1 == 100 && $2 >= 64 && $2 <= 127 &&
        $3 >= 0 && $3 <= 255 && $4 >= 0 && $4 <= 255 { found=1 }
        END { exit(found ? 0 : 1) }
    '
}

_tailscale_cli_ip() {
    TS_CLI=$(tailscale_cli_path) || return 1
    TS_IP_OUTPUT=$(with_timeout 3 env TAILSCALE_BE_CLI=1 "${TS_CLI}" ip -4 2>/dev/null || true)
    for TS_IP_CANDIDATE in ${TS_IP_OUTPUT}; do
        TS_IP_CANDIDATE=$(printf '%s' "${TS_IP_CANDIDATE}" | tr -cd '0-9.')
        if is_tailscale_ipv4 "${TS_IP_CANDIDATE}"; then
            printf '%s\n' "${TS_IP_CANDIDATE}"
            return 0
        fi
    done
    return 1
}

_tailscale_interface_ip() {
    TS_INTERFACE_OUTPUT=""
    if command -v ip >/dev/null 2>&1; then
        TS_INTERFACE_OUTPUT=$(ip addr 2>/dev/null || true)
    fi
    if command -v ifconfig >/dev/null 2>&1; then
        TS_INTERFACE_OUTPUT="${TS_INTERFACE_OUTPUT}
$(ifconfig 2>/dev/null || true)"
    fi
    if command -v ipconfig.exe >/dev/null 2>&1; then
        TS_INTERFACE_OUTPUT="${TS_INTERFACE_OUTPUT}
$(with_timeout 3 ipconfig.exe 2>/dev/null || true)"
    fi

    TS_INTERFACE_IPS=$(printf '%s\n' "${TS_INTERFACE_OUTPUT}" \
        | grep -Eo '100\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}' 2>/dev/null || true)
    for TS_IP_CANDIDATE in ${TS_INTERFACE_IPS}; do
        if is_tailscale_ipv4 "${TS_IP_CANDIDATE}"; then
            printf '%s\n' "${TS_IP_CANDIDATE}"
            return 0
        fi
    done
    return 1
}

# Prints the local Tailscale IPv4 from the CLI or the OS-managed interface.
get_tailscale_ip() {
    TS_IP=$(_tailscale_cli_ip 2>/dev/null || true)
    if [ -n "${TS_IP}" ]; then
        printf '%s\n' "${TS_IP}"
        return 0
    fi

    _tailscale_interface_ip
}

is_tailscale_backend_responding() {
    TS_STATUS=$(tailscale_status_json 2>/dev/null || true)
    [ -n "${TS_STATUS}" ]
}

# True when an authenticated CLI backend or an OS-managed Tailscale interface
# is available. The interface fallback covers macOS GUI, Windows/WSL and iSH.
is_tailscale_mesh_operational() {
    if is_tailscale_backend_responding && _tailscale_cli_ip >/dev/null 2>&1; then
        TS_BACKEND_STATE=$(printf '%s\n' "${TS_STATUS}" \
            | sed -n 's/.*"BackendState"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
            | head -1)
        if [ -z "${TS_BACKEND_STATE}" ] || [ "${TS_BACKEND_STATE}" = "Running" ]; then
            return 0
        fi
    fi
    _tailscale_interface_ip >/dev/null 2>&1
}

is_on_tailscale_network() {
    is_tailscale_mesh_operational
}

is_tailscale_daemon_running() {
    if is_tailscale_backend_responding; then
        return 0
    fi
    if command -v pgrep >/dev/null 2>&1 && pgrep tailscaled >/dev/null 2>&1; then
        return 0
    fi
    if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet tailscaled 2>/dev/null; then
        return 0
    fi
    if command -v rc-service >/dev/null 2>&1 && rc-service tailscale status >/dev/null 2>&1; then
        return 0
    fi
    if command -v sc.exe >/dev/null 2>&1 \
        && with_timeout 3 sc.exe query Tailscale 2>/dev/null | grep -q 'RUNNING'; then
        return 0
    fi
    return 1
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
    if [ -f "${TARGET_FILE}" ]; then
        if grep -Fx "${TARGET}" "${TARGET_FILE}" >/dev/null 2>&1; then
            return 0
        fi
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
    if ! has_tailscale_cli || ! command -v jq >/dev/null 2>&1; then
        return 0
    fi

    TS_STATUS=$(tailscale_status_json 2>/dev/null || true)
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
    if ! has_tailscale_cli || ! command -v jq >/dev/null 2>&1; then
        return 0
    fi

    TS_STATUS=$(tailscale_status_json 2>/dev/null || true)
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
        tailscale_cli status >/dev/null 2>&1 && return 0
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

#!/usr/bin/env sh
# =============================================================================
# PocketCli — detect_os.sh
# Exports $OS variable. Meant to be sourced, not executed directly.
# =============================================================================

_pocketcli_log_detect_os() {
    LOG_FILE="${POCKETCLI_DEBUG_LOG:-/tmp/pocketcli-debug.log}"
    TS=$(date '+%Y-%m-%d %H:%M:%S' 2>/dev/null || printf 'unknown-time')
    printf '%s [detect_os] %s\n' "${TS}" "$*" >> "${LOG_FILE}" 2>/dev/null || true
}

detect_os() {
    OS=""
    _pocketcli_log_detect_os "starting OS detection"

    # WSL — check first, before generic Linux
    if [ -n "${WSL_DISTRO_NAME:-}" ]; then
        OS="wsl"

    elif grep -qi microsoft /proc/version 2>/dev/null; then
        OS="wsl"

    # macOS
    elif [ "$(uname)" = "Darwin" ]; then
        OS="mac"

    # Alpine
    elif [ -f /etc/alpine-release ]; then
        OS="alpine"

    # Debian / Ubuntu
    elif [ -f /etc/debian_version ]; then
        OS="debian"

    # Generic Linux fallback
    elif [ "$(uname -s)" = "Linux" ]; then
        OS="linux"

    else
        OS="unknown"
    fi

    [ "${OS}" = "unknown" ] && {
        printf '[PocketCli] Warning: OS not recognised — defaulting to linux.\n'
        _pocketcli_log_detect_os "OS unknown, defaulting to linux"
        OS="linux"
    }

    _pocketcli_log_detect_os "detected OS=${OS}"
    export OS
}

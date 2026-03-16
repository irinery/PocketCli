#!/usr/bin/env bash
# =============================================================================
# PocketCli — detect_os.sh
# Exports $OS variable. Meant to be sourced, not executed directly.
# =============================================================================

detect_os() {
    OS=""

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
        OS="linux"
    }

    export OS
}

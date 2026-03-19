#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/install_tailscale.sh
# Installs and authenticates Tailscale.
# =============================================================================

set -eu

LOG_FILE="${POCKETCLI_DEBUG_LOG:-/tmp/pocketcli-debug.log}"
log_debug() {
    TS=$(date '+%Y-%m-%d %H:%M:%S' 2>/dev/null || printf 'unknown-time')
    printf '%s [install_tailscale] %s\n' "${TS}" "$*" >> "${LOG_FILE}" 2>/dev/null || true
}

OS="${1:-}"
[ -z "${OS}" ] && { printf '[PocketCli] OS not provided\n' >&2; exit 1; }
log_debug "starting install flow os=${OS}"

# ---------------------------------------------------------------------------
# Helper — must be defined before any call site
# ---------------------------------------------------------------------------
_tailscale_login() {
    log_debug "starting tailscale login"
    printf '\n[PocketCli] Starting Tailscale login...\n'
    printf '  (A browser window or URL will appear — authenticate to continue)\n\n'

    sudo tailscale up --ssh || {
        printf '[PocketCli] tailscale up failed. Run "sudo tailscale up --ssh" manually.\n'
    }
}

# ---------------------------------------------------------------------------
# Skip if already installed
# ---------------------------------------------------------------------------
if command -v tailscale >/dev/null 2>&1; then
    log_debug "tailscale already installed"
    printf '[PocketCli] Tailscale already installed — skipping.\n'
    _tailscale_login
    exit 0
fi

printf '[PocketCli] Installing Tailscale...\n'

case "${OS}" in

    debian|wsl|linux)
        log_debug "installing tailscale via upstream script"
        curl -fsSL https://tailscale.com/install.sh | sh
    ;;

    alpine)
        log_debug "installing tailscale via apk"
        apk add --no-cache tailscale
        rc-update add tailscale 2>/dev/null || true
        rc-service tailscale start 2>/dev/null || true
    ;;

    mac)
        log_debug "installing tailscale via brew"
        brew install tailscale 2>/dev/null || \
            printf '[PocketCli] Install Tailscale from https://tailscale.com/download\n'
    ;;

    *)
        log_debug "unsupported os=${OS}; requesting manual install"
        printf '[PocketCli] Install Tailscale manually: https://tailscale.com/download\n'
        exit 0
    ;;
esac

log_debug "tailscale install flow finished"
printf '[PocketCli] Tailscale installed.\n'
_tailscale_login

#!/usr/bin/env bash
# =============================================================================
# PocketCli — scripts/install_tailscale.sh
# Installs and authenticates Tailscale.
# =============================================================================

set -euo pipefail

OS="${1:-}"
[ -z "${OS}" ] && { printf '[PocketCli] OS not provided\n' >&2; exit 1; }

# Skip if already installed
if command -v tailscale >/dev/null 2>&1; then
    printf '[PocketCli] Tailscale already installed — skipping.\n'
    _tailscale_login
    exit 0
fi

printf '[PocketCli] Installing Tailscale...\n'

case "${OS}" in

    debian|wsl|linux)
        curl -fsSL https://tailscale.com/install.sh | sh
    ;;

    alpine)
        apk add --no-cache tailscale
        rc-update add tailscale 2>/dev/null || true
        rc-service tailscale start 2>/dev/null || true
    ;;

    mac)
        brew install tailscale 2>/dev/null || \
            printf '[PocketCli] Install Tailscale from https://tailscale.com/download\n'
    ;;

    *)
        printf '[PocketCli] Install Tailscale manually: https://tailscale.com/download\n'
        return 0
    ;;
esac

printf '[PocketCli] Tailscale installed.\n'
_tailscale_login

# ---------------------------------------------------------------------------
_tailscale_login() {
    printf '\n[PocketCli] Starting Tailscale login...\n'
    printf '  (A browser window or URL will appear — authenticate to continue)\n\n'

    sudo tailscale up --ssh || {
        printf '[PocketCli] tailscale up failed. Run "sudo tailscale up --ssh" manually.\n'
    }
}

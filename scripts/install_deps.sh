#!/usr/bin/env bash
# =============================================================================
# PocketCli — scripts/install_deps.sh
# Usage: install_deps.sh <os> <mode>
# =============================================================================

set -euo pipefail

OS="${1:-}"
MODE="${2:-}"

[ -z "${OS}" ]   && { printf '[PocketCli] OS not provided to install_deps.sh\n' >&2; exit 1; }
[ -z "${MODE}" ] && { printf '[PocketCli] MODE not provided to install_deps.sh\n' >&2; exit 1; }

# ---------------------------------------------------------------------------
# Package lists
# ---------------------------------------------------------------------------
# Viewer: minimal SSH client environment
VIEWER_PKGS="git curl jq tmux zsh fzf"

# Agent: full toolkit
AGENT_PKGS="git curl jq tmux zsh fzf ripgrep htop lazygit starship"

case "${MODE}" in
    viewer) PKGS="${VIEWER_PKGS}" ;;
    agent)  PKGS="${AGENT_PKGS}"  ;;
    *)      printf '[PocketCli] Unknown mode: %s\n' "${MODE}" >&2; exit 1 ;;
esac

printf '[PocketCli] Installing packages: %s\n' "${PKGS}"

# ---------------------------------------------------------------------------
# Install by OS
# ---------------------------------------------------------------------------
case "${OS}" in

    alpine)
        apk update
        # shellcheck disable=SC2086
        apk add --no-cache ${PKGS}
    ;;

    debian|wsl|linux)
        sudo apt-get update -qq
        # shellcheck disable=SC2086
        sudo apt-get install -y --no-install-recommends ${PKGS} || {
            printf '[PocketCli] Some packages may not exist in this distro. Continuing.\n'
        }

        # lazygit not in apt — install from GitHub release
        if ! command -v lazygit >/dev/null 2>&1 && [ "${MODE}" = "agent" ]; then
            _install_lazygit_linux
        fi

        # starship not in apt
        if ! command -v starship >/dev/null 2>&1 && [ "${MODE}" = "agent" ]; then
            curl -sS https://starship.rs/install.sh | sh -s -- --yes
        fi
    ;;

    mac)
        if ! command -v brew >/dev/null 2>&1; then
            printf '[PocketCli] Homebrew is required on macOS.\n'
            printf '  Install it at: https://brew.sh\n'
            exit 1
        fi
        # shellcheck disable=SC2086
        brew install ${PKGS} || true
    ;;

    *)
        printf '[PocketCli] Unsupported OS: %s\n' "${OS}" >&2
        exit 1
    ;;
esac

printf '[PocketCli] Dependencies installed.\n'

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
_install_lazygit_linux() {
    printf '[PocketCli] Installing lazygit...\n'
    LG_VERSION=$(curl -s "https://api.github.com/repos/jesseduffield/lazygit/releases/latest" \
        | jq -r '.tag_name' | tr -d 'v')
    LG_URL="https://github.com/jesseduffield/lazygit/releases/download/v${LG_VERSION}/lazygit_${LG_VERSION}_Linux_x86_64.tar.gz"
    TMP=$(mktemp -d)
    curl -fsSL "${LG_URL}" -o "${TMP}/lazygit.tar.gz"
    tar -xzf "${TMP}/lazygit.tar.gz" -C "${TMP}"
    sudo install "${TMP}/lazygit" /usr/local/bin/lazygit
    rm -rf "${TMP}"
}

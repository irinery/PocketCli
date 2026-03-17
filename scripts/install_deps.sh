#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/install_deps.sh
# Usage: sh install_deps.sh <os> <mode>
# =============================================================================

set -eu

OS="${1:-}"
MODE="${2:-}"

[ -z "${OS}" ]   && { printf '[PocketCli] OS not provided\n' >&2; exit 1; }
[ -z "${MODE}" ] && { printf '[PocketCli] MODE not provided\n' >&2; exit 1; }

# Viewer: minimal — just what's needed for SSH + menu
VIEWER_PKGS="git curl jq tmux zsh qrencode"
# Agent: full toolkit (fzf useful for pocket connect picker on non-iSH)
AGENT_PKGS="git curl jq tmux zsh fzf ripgrep htop"

case "${MODE}" in
    viewer) PKGS="${VIEWER_PKGS}" ;;
    agent)  PKGS="${AGENT_PKGS}"  ;;
    *)      printf '[PocketCli] Unknown mode: %s\n' "${MODE}" >&2; exit 1 ;;
esac

printf '[PocketCli] Installing: %s\n' "${PKGS}"

case "${OS}" in

    alpine|ish)
        apk update
        # shellcheck disable=SC2086
        apk add --no-cache ${PKGS}

        # starship — not in Alpine repos, install via official script
        if [ "${MODE}" = "agent" ] && ! command -v starship >/dev/null 2>&1; then
            _install_starship
        fi
    ;;

    debian|wsl|linux)
        sudo apt-get update -qq
        # shellcheck disable=SC2086
        sudo apt-get install -y --no-install-recommends ${PKGS} || true

        if [ "${MODE}" = "agent" ]; then
            command -v lazygit  >/dev/null 2>&1 || _install_lazygit_linux
            command -v starship >/dev/null 2>&1 || _install_starship
        fi
    ;;

    mac)
        command -v brew >/dev/null 2>&1 || {
            printf '[PocketCli] Homebrew required: https://brew.sh\n'; exit 1
        }
        # shellcheck disable=SC2086
        brew install ${PKGS} || true
        [ "${MODE}" = "agent" ] && brew install lazygit || true
    ;;

    *)
        printf '[PocketCli] Unsupported OS: %s\n' "${OS}" >&2; exit 1
    ;;
esac

printf '[PocketCli] Dependencies installed.\n'

# ---------------------------------------------------------------------------
_install_lazygit_linux() {
    printf '[PocketCli] Installing lazygit...\n'
    LG_VER=$(curl -fsSL "https://api.github.com/repos/jesseduffield/lazygit/releases/latest" \
        | jq -r '.tag_name' | tr -d 'v')
    TMP=$(mktemp -d)
    curl -fsSL \
        "https://github.com/jesseduffield/lazygit/releases/download/v${LG_VER}/lazygit_${LG_VER}_Linux_x86_64.tar.gz" \
        -o "${TMP}/lazygit.tar.gz"
    tar -xzf "${TMP}/lazygit.tar.gz" -C "${TMP}"
    sudo install "${TMP}/lazygit" /usr/local/bin/lazygit
    rm -rf "${TMP}"
}

_install_starship() {
    printf '[PocketCli] Installing starship...\n'
    curl -fsSL https://starship.rs/install.sh | sh -s -- --yes
}

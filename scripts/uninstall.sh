#!/usr/bin/env bash
# =============================================================================
# PocketCli — scripts/uninstall.sh
# Removes PocketCli from the current user's environment.
# Does NOT remove Tailscale or system packages installed by PocketCli.
# =============================================================================

set -euo pipefail

INSTALL_DIR="${HOME}/.pocketcli"
SHELL_RC_FILES=("${HOME}/.zshrc" "${HOME}/.bashrc" "${HOME}/.profile")
TMUX_CONF="${HOME}/.config/tmux/tmux.conf"
STARSHIP_CONF="${HOME}/.config/starship.toml"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

_info() { printf "${CYAN}▸${NC} %s\n" "$*"; }
_ok()   { printf "${GREEN}✔${NC} %s\n" "$*"; }
_warn() { printf "${YELLOW}!${NC} %s\n" "$*"; }
_die()  { printf "${RED}✘${NC} %s\n" "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Guard
# ---------------------------------------------------------------------------
[ -z "${HOME}" ] && _die "\$HOME is not set."

echo ""
printf "${RED}  ╔════════════════════════════════════╗${NC}\n"
printf "${RED}  ║       PocketCli  Uninstaller       ║${NC}\n"
printf "${RED}  ╚════════════════════════════════════╝${NC}\n"
echo ""
_warn "This will remove:"
echo "   • ${INSTALL_DIR}"
echo "   • pocket-* aliases and PATH entries from shell RC files"
echo "   • PocketCli tmux config (${TMUX_CONF})"
echo "   • PocketCli starship config (${STARSHIP_CONF})"
echo ""
_warn "It will NOT remove: tailscale, git, tmux, zsh or any other system package."
echo ""
printf "  Continue? [y/N] "
read -r CONFIRM

case "${CONFIRM}" in
    y|Y|yes|YES) : ;;
    *) _info "Aborted."; exit 0 ;;
esac

echo ""

# ---------------------------------------------------------------------------
# Kill running tmux session if it belongs to PocketCli
# ---------------------------------------------------------------------------
if tmux has-session -t pocketcli 2>/dev/null; then
    _info "Stopping pocketcli tmux session..."
    tmux kill-session -t pocketcli
    _ok "tmux session stopped."
fi

# ---------------------------------------------------------------------------
# Remove install directory
# ---------------------------------------------------------------------------
if [ -d "${INSTALL_DIR}" ]; then
    # Extra safety: ensure INSTALL_DIR is under $HOME
    case "${INSTALL_DIR}" in
        "${HOME}/"*) : ;;
        *) _die "INSTALL_DIR '${INSTALL_DIR}' is outside \$HOME — refusing to delete." ;;
    esac
    _info "Removing ${INSTALL_DIR}..."
    rm -rf "${INSTALL_DIR}"
    _ok "Removed ${INSTALL_DIR}"
else
    _warn "${INSTALL_DIR} not found — skipping."
fi

# ---------------------------------------------------------------------------
# Clean shell RC files
# ---------------------------------------------------------------------------
for rc in "${SHELL_RC_FILES[@]}"; do
    if [ -f "${rc}" ] && grep -qF "pocketcli" "${rc}" 2>/dev/null; then
        _info "Cleaning ${rc}..."
        # Remove the pocketcli block (comment + source line)
        sed -i.bak '/# ── PocketCli/,/# ──────/d' "${rc}"
        sed -i.bak '/pocketcli/Id' "${rc}"
        rm -f "${rc}.bak"
        _ok "Cleaned ${rc}"
    fi
done

# ---------------------------------------------------------------------------
# Remove config files (only if they were created by PocketCli)
# ---------------------------------------------------------------------------
if [ -f "${TMUX_CONF}" ] && grep -qF "PocketCli" "${TMUX_CONF}" 2>/dev/null; then
    rm -f "${TMUX_CONF}"
    _ok "Removed tmux config."
fi

if [ -f "${STARSHIP_CONF}" ] && grep -qF "PocketCli" "${STARSHIP_CONF}" 2>/dev/null; then
    rm -f "${STARSHIP_CONF}"
    _ok "Removed starship config."
fi

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------
echo ""
_ok "PocketCli has been removed."
echo ""
printf "  Restart your shell or run:  source ~/.zshrc\n"
echo ""

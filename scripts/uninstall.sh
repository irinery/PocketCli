#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/uninstall.sh
# =============================================================================

set -eu

INSTALL_DIR="${HOME}/.pocketcli"
TMUX_CONF="${HOME}/.config/tmux/tmux.conf"
STARSHIP_CONF="${HOME}/.config/starship.toml"

_info() { printf '[PocketCli] %s\n' "$*"; }
_ok()   { printf '[PocketCli] OK: %s\n' "$*"; }
_die()  { printf '[PocketCli] ERROR: %s\n' "$*" >&2; exit 1; }

[ -z "${HOME:-}" ] && _die "\$HOME is not set."

echo ""
echo "  ================================="
echo "    PocketCli  Uninstaller"
echo "  ================================="
echo ""
printf '  This will remove:\n'
printf '   * %s\n' "${INSTALL_DIR}"
printf '   * pocket aliases from shell RC files\n'
printf '   * PocketCli tmux and starship configs\n'
echo ""
printf '  Continue? [y/N] '
read -r CONFIRM < /dev/tty

case "${CONFIRM}" in
    y|Y|yes|YES) : ;;
    *) _info "Aborted."; exit 0 ;;
esac

echo ""

# Stop tmux session if running
if command -v tmux >/dev/null 2>&1; then
    if tmux has-session -t pocketcli 2>/dev/null; then
        _info "Stopping pocketcli tmux session..."
        tmux kill-session -t pocketcli
        _ok "tmux session stopped."
    fi
fi

# Remove install directory — guard: must be under $HOME
if [ -d "${INSTALL_DIR}" ]; then
    case "${INSTALL_DIR}" in
        "${HOME}/"*) : ;;
        *) _die "INSTALL_DIR '${INSTALL_DIR}' is outside \$HOME — refusing." ;;
    esac
    _info "Removing ${INSTALL_DIR}..."
    rm -rf "${INSTALL_DIR}"
    _ok "Removed ${INSTALL_DIR}"
fi

# Clean shell RC files — iterate without arrays
for rc in "${HOME}/.zshrc" "${HOME}/.bashrc" "${HOME}/.profile"; do
    if [ -f "${rc}" ] && grep -qF "pocketcli" "${rc}" 2>/dev/null; then
        _info "Cleaning ${rc}..."
        sed -i.bak '/pocketcli/Id;/PocketCli/d' "${rc}" 2>/dev/null || \
            sed -i.bak '/pocketcli/d;/PocketCli/d' "${rc}"
        rm -f "${rc}.bak"
        _ok "Cleaned ${rc}"
    fi
done

# Remove config files only if created by PocketCli
if [ -f "${TMUX_CONF}" ] && grep -qF "PocketCli" "${TMUX_CONF}" 2>/dev/null; then
    rm -f "${TMUX_CONF}"
    _ok "Removed tmux config."
fi

if [ -f "${STARSHIP_CONF}" ] && grep -qF "PocketCli" "${STARSHIP_CONF}" 2>/dev/null; then
    rm -f "${STARSHIP_CONF}"
    _ok "Removed starship config."
fi

echo ""
_ok "PocketCli removed."
echo ""
printf '  Restart your shell or run:  . ~/.profile\n'
echo ""

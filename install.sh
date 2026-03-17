#!/usr/bin/env sh
# =============================================================================
# PocketCli — install.sh
# Orchestrates the full installation flow.
# =============================================================================

set -eu

INSTALL_DIR="${HOME}/.pocketcli"
SCRIPTS_DIR="${INSTALL_DIR}/scripts"
CONFIG_DIR="${INSTALL_DIR}/config"

info()    { printf '[PocketCli] %s\n' "$*"; }
success() { printf '[PocketCli] OK: %s\n' "$*"; }
die()     { printf '[PocketCli] ERROR: %s\n' "$*" >&2; exit 1; }

[ -z "${HOME:-}" ] && die "\$HOME is not set."

# ---------------------------------------------------------------------------
# Detect OS — source the script (POSIX . instead of bash source)
# ---------------------------------------------------------------------------
. "${INSTALL_DIR}/detect_os.sh"
detect_os
info "Detected OS: ${OS}"

# ---------------------------------------------------------------------------
# Choose install mode
# ---------------------------------------------------------------------------
echo ""
echo "  Select install mode:"
echo ""
echo "    1) Viewer  ->  iPad or lightweight terminal (SSH client only)"
echo "    2) Agent   ->  server or remote machine (full environment)"
echo ""
printf "  Choice [1/2]: "
# Read from /dev/tty explicitly — required when stdin is a pipe (curl | sh)
read -r MODE_CHOICE < /dev/tty

case "${MODE_CHOICE}" in
    1) MODE="viewer" ;;
    2) MODE="agent"  ;;
    *) die "Invalid choice '${MODE_CHOICE}'. Re-run the installer." ;;
esac

info "Mode: ${MODE}"

# ---------------------------------------------------------------------------
# Install dependencies
# ---------------------------------------------------------------------------
sh "${SCRIPTS_DIR}/install_deps.sh" "${OS}" "${MODE}"

# ---------------------------------------------------------------------------
# Install Tailscale
# ---------------------------------------------------------------------------
sh "${SCRIPTS_DIR}/tailscale_daemon.sh" setup

# ---------------------------------------------------------------------------
# Apply config files
# ---------------------------------------------------------------------------
info "Applying configuration files..."

# Always write to .profile (iSH uses sh, not zsh/bash by default)
# Also write to .zshrc if zsh exists
_inject_path() {
    RC="$1"
    if ! grep -qF "pocketcli" "${RC}" 2>/dev/null; then
        printf '\n# -- PocketCli ------------------------------------------------\n' >> "${RC}"
        printf 'export POCKETCLI_DIR="%s"\n' "${INSTALL_DIR}"         >> "${RC}"
        # Use single quotes around PATH so $PATH expands at runtime, not now
        printf 'export PATH="%s:$PATH"\n'   "${INSTALL_DIR}"          >> "${RC}"
        printf '. "%s/config/zshrc"\n'      "${INSTALL_DIR}"          >> "${RC}"
        printf '# --------------------------------------------------------------\n' >> "${RC}"
        info "PATH added to ${RC}"
    fi
}

_inject_path "${HOME}/.profile"
command -v zsh >/dev/null 2>&1 && _inject_path "${HOME}/.zshrc" || true

# Apply right now for this session (don't require shell restart)
export PATH="${INSTALL_DIR}:${PATH}"

# tmux config
mkdir -p "${HOME}/.config/tmux"
cp "${CONFIG_DIR}/tmux.conf" "${HOME}/.config/tmux/tmux.conf"

# starship config
mkdir -p "${HOME}/.config"
cp "${CONFIG_DIR}/starship.toml" "${HOME}/.config/starship.toml"

success "Config files applied."

# Make pocket binary executable
chmod 700 "${INSTALL_DIR}/pocket"

# ---------------------------------------------------------------------------
# Harden permissions
# ---------------------------------------------------------------------------
info "Hardening permissions..."
chmod -R o-rwx "${INSTALL_DIR}"
find "${INSTALL_DIR}" -name "*.sh" -exec chmod 700 {} \;
success "Permissions hardened."

# ---------------------------------------------------------------------------
# Post-install tip
# ---------------------------------------------------------------------------
echo ""
echo "  ================================="
success "PocketCli installed!"
echo "  ================================="
echo ""
echo "  PATH already active in this session."
echo "  For new shells, run:"
echo ""
printf '    . ~/.profile\n'
echo ""
echo "  Then use:  pocket help"
echo ""

# ---------------------------------------------------------------------------
# Start environment
# ---------------------------------------------------------------------------

# .profile must be sourced in the new shell — iSH ash doesn't auto-source it
# We pass PATH explicitly via env so the child script has it immediately
case "${MODE}" in
    viewer) exec env PATH="${INSTALL_DIR}:${PATH}" sh "${SCRIPTS_DIR}/start_viewer.sh" ;;
    agent)  exec env PATH="${INSTALL_DIR}:${PATH}" sh "${SCRIPTS_DIR}/start_agent.sh"  ;;
esac
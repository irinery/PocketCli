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
sh "${SCRIPTS_DIR}/install_tailscale.sh" "${OS}"

# ---------------------------------------------------------------------------
# Apply config files
# ---------------------------------------------------------------------------
info "Applying configuration files..."

# Detect shell RC file
if command -v zsh >/dev/null 2>&1; then
    SHELL_RC="${HOME}/.zshrc"
else
    SHELL_RC="${HOME}/.profile"
fi

# Inject PocketCli block only once
if ! grep -qF "pocketcli" "${SHELL_RC}" 2>/dev/null; then
    printf '\n# -- PocketCli ------------------------------------------------\n' >> "${SHELL_RC}"
    printf 'export POCKETCLI_DIR="%s"\n' "${INSTALL_DIR}" >> "${SHELL_RC}"
    printf 'export PATH="%s:${PATH}"\n' "${INSTALL_DIR}" >> "${SHELL_RC}"
    printf '. "%s/config/zshrc"\n' "${INSTALL_DIR}" >> "${SHELL_RC}"
    printf '# --------------------------------------------------------------\n' >> "${SHELL_RC}"
fi

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
# Start environment
# ---------------------------------------------------------------------------
case "${MODE}" in
    viewer) exec sh "${SCRIPTS_DIR}/start_viewer.sh" ;;
    agent)  exec sh "${SCRIPTS_DIR}/start_agent.sh"  ;;
esac

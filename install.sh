#!/usr/bin/env bash
# =============================================================================
# PocketCli — install.sh
# Orchestrates the full installation flow.
# =============================================================================

set -euo pipefail

INSTALL_DIR="${HOME}/.pocketcli"
SCRIPTS_DIR="${INSTALL_DIR}/scripts"

# ---------------------------------------------------------------------------
# Helpers (duplicated for standalone safety)
# ---------------------------------------------------------------------------
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info()    { printf "${CYAN}[PocketCli]${NC} %s\n" "$*"; }
success() { printf "${GREEN}[✔]${NC} %s\n" "$*"; }
warn()    { printf "${YELLOW}[!]${NC} %s\n" "$*"; }
die()     { printf "${RED}[✘]${NC} %s\n" "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Detect OS
# ---------------------------------------------------------------------------
source "${INSTALL_DIR}/detect_os.sh"
detect_os
info "Detected OS: ${OS}"

# ---------------------------------------------------------------------------
# Choose install mode
# ---------------------------------------------------------------------------
echo ""
echo "  Select install mode:"
echo ""
echo "    1) Viewer  →  iPad or lightweight terminal (SSH client only)"
echo "    2) Agent   →  server or remote machine (full environment)"
echo ""
printf "  Choice [1/2]: "
read -r MODE_CHOICE

case "${MODE_CHOICE}" in
    1) MODE="viewer" ;;
    2) MODE="agent"  ;;
    *) die "Invalid choice: '${MODE_CHOICE}'. Run the installer again." ;;
esac

info "Mode selected: ${MODE}"

# ---------------------------------------------------------------------------
# Install dependencies
# ---------------------------------------------------------------------------
"${SCRIPTS_DIR}/install_deps.sh" "${OS}" "${MODE}"

# ---------------------------------------------------------------------------
# Install & configure Tailscale
# ---------------------------------------------------------------------------
"${SCRIPTS_DIR}/install_tailscale.sh" "${OS}"

# ---------------------------------------------------------------------------
# Apply config files
# ---------------------------------------------------------------------------
info "Applying configuration files..."

CONFIG_DIR="${INSTALL_DIR}/config"
SHELL_RC="${HOME}/.zshrc"

# Use .bashrc if zsh not available
command -v zsh >/dev/null 2>&1 || SHELL_RC="${HOME}/.bashrc"

# zshrc
if ! grep -qF "pocketcli" "${SHELL_RC}" 2>/dev/null; then
    cat >> "${SHELL_RC}" <<SHELLEOF

# ── PocketCli ──────────────────────────────────────────────────────────────
export POCKETCLI_DIR="\${HOME}/.pocketcli"
source "\${POCKETCLI_DIR}/config/zshrc"
# ───────────────────────────────────────────────────────────────────────────
SHELLEOF
fi

# tmux config
mkdir -p "${HOME}/.config/tmux"
cp "${CONFIG_DIR}/tmux.conf" "${HOME}/.config/tmux/tmux.conf"

# starship config
mkdir -p "${HOME}/.config"
cp "${CONFIG_DIR}/starship.toml" "${HOME}/.config/starship.toml"

success "Config files applied."

# ---------------------------------------------------------------------------
# Harden permissions
# ---------------------------------------------------------------------------
info "Applying permission hardening..."
chmod -R o-rwx,g-w "${INSTALL_DIR}"
find "${INSTALL_DIR}" -type f -name "*.sh" -exec chmod 700 {} \;
success "Permissions hardened."

# ---------------------------------------------------------------------------
# Start the appropriate environment
# ---------------------------------------------------------------------------
case "${MODE}" in
    viewer) exec "${SCRIPTS_DIR}/start_viewer.sh" ;;
    agent)  exec "${SCRIPTS_DIR}/start_agent.sh"  ;;
esac

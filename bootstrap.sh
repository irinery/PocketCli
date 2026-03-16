#!/usr/bin/env bash
# =============================================================================
# PocketCli — Bootstrap
# https://github.com/irinery/PocketCli
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/irinery/PocketCli/main/bootstrap.sh | bash
#
# Verify checksum before running (recommended):
#   curl -fsSL https://raw.githubusercontent.com/irinery/PocketCli/main/bootstrap.sh -o bootstrap.sh
#   sha256sum bootstrap.sh   # compare with published hash at github.com/irinery/PocketCli
#   bash bootstrap.sh
# =============================================================================

set -euo pipefail

# ---------------------------------------------------------------------------
# Constants
# ---------------------------------------------------------------------------
REPO_URL="https://github.com/irinery/PocketCli.git"
INSTALL_DIR="${HOME}/.pocketcli"
POCKETCLI_VERSION="main"          # pin to a tag/commit in production, e.g. "v1.0.0"

# ---------------------------------------------------------------------------
# Helpers
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
# Guards
# ---------------------------------------------------------------------------
[ -z "${HOME:-}" ]    && die "\$HOME is not set. Aborting."
[ -z "${SHELL:-}" ]   && warn "\$SHELL is not set — defaulting to bash."

command -v git  >/dev/null 2>&1 || die "git is required but not installed."
command -v curl >/dev/null 2>&1 || die "curl is required but not installed."

# ---------------------------------------------------------------------------
# Banner
# ---------------------------------------------------------------------------
echo ""
echo "  ╔═══════════════════════════════╗"
echo "  ║        P o c k e t C l i     ║"
echo "  ║   portable terminal toolkit  ║"
echo "  ╚═══════════════════════════════╝"
echo ""

# ---------------------------------------------------------------------------
# Clone / update
# ---------------------------------------------------------------------------
if [ -d "${INSTALL_DIR}/.git" ]; then
    info "PocketCli already installed at ${INSTALL_DIR} — updating..."
    git -C "${INSTALL_DIR}" fetch --quiet origin
    git -C "${INSTALL_DIR}" checkout --quiet "${POCKETCLI_VERSION}"
    git -C "${INSTALL_DIR}" pull --quiet --ff-only
else
    info "Cloning PocketCli into ${INSTALL_DIR}..."
    git clone --quiet --branch "${POCKETCLI_VERSION}" "${REPO_URL}" "${INSTALL_DIR}"
fi

success "Repository ready."

# ---------------------------------------------------------------------------
# Hand off to install.sh
# ---------------------------------------------------------------------------
INSTALL_SCRIPT="${INSTALL_DIR}/install.sh"

[ -f "${INSTALL_SCRIPT}" ] || die "install.sh not found in cloned repository."
chmod 700 "${INSTALL_SCRIPT}"

exec "${INSTALL_SCRIPT}"

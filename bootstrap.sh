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
ARCHIVE_URL_BASE="https://codeload.github.com/irinery/PocketCli/tar.gz/refs/heads"
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

have_git() {
    command -v git >/dev/null 2>&1
}

restore_update_stash() {
    local stash_ref="$1"
    [ -n "${stash_ref}" ] || return 0
    if git -C "${INSTALL_DIR}" stash pop --quiet >/dev/null 2>&1; then
        info "Restored local changes after update."
    else
        warn "Update completed; local changes remain in git stash for manual conflict resolution."
    fi
}

update_git_install() {
    local stash_ref="" before_stash="" after_stash=""
    if [ -n "$(git -C "${INSTALL_DIR}" status --porcelain 2>/dev/null || true)" ]; then
        before_stash=$(git -C "${INSTALL_DIR}" rev-parse -q --verify refs/stash 2>/dev/null || true)
        git -C "${INSTALL_DIR}" stash push --include-untracked --quiet -m "pocketcli-bootstrap-update"
        after_stash=$(git -C "${INSTALL_DIR}" rev-parse -q --verify refs/stash 2>/dev/null || true)
        if [ -n "${after_stash}" ] && [ "${after_stash}" != "${before_stash}" ]; then
            stash_ref="${after_stash}"
        fi
    fi

    if ! git -C "${INSTALL_DIR}" fetch --quiet origin || \
        ! git -C "${INSTALL_DIR}" checkout --quiet "${POCKETCLI_VERSION}" || \
        ! git -C "${INSTALL_DIR}" pull --quiet --ff-only; then
        restore_update_stash "${stash_ref}"
        die "Could not update the existing installation."
    fi
    restore_update_stash "${stash_ref}"
}

download_archive_install() {
    local archive_url tmp_dir tmp_tgz extracted_dir backup_dir
    archive_url="${ARCHIVE_URL_BASE}/${POCKETCLI_VERSION}"
    tmp_dir=$(mktemp -d)
    tmp_tgz="${tmp_dir}/pocketcli.tar.gz"

    info "Downloading PocketCli archive (${POCKETCLI_VERSION})..."
    curl -fsSL "${archive_url}" -o "${tmp_tgz}"
    tar -xzf "${tmp_tgz}" -C "${tmp_dir}"

    extracted_dir=$(find "${tmp_dir}" -mindepth 1 -maxdepth 1 -type d -name 'PocketCli-*' | head -n 1 || true)
    [ -n "${extracted_dir}" ] || die "Could not locate extracted PocketCli directory from archive."

    backup_dir=""
    if [ -d "${INSTALL_DIR}/profile" ]; then
        backup_dir="${tmp_dir}/profile-backup"
        mkdir -p "${backup_dir}"
        cp -R "${INSTALL_DIR}/profile/." "${backup_dir}/"
    fi

    rm -rf "${INSTALL_DIR}"
    mkdir -p "${INSTALL_DIR}"
    cp -R "${extracted_dir}/." "${INSTALL_DIR}/"
    chmod 700 "${INSTALL_DIR}"

    if [ -n "${backup_dir}" ]; then
        mkdir -p "${INSTALL_DIR}/profile"
        cp -R "${backup_dir}/." "${INSTALL_DIR}/profile/"
        info "Restored local profile customizations into ${INSTALL_DIR}/profile."
    fi

    rm -rf "${tmp_dir}"
}

# ---------------------------------------------------------------------------
# Guards
# ---------------------------------------------------------------------------
[ -z "${HOME:-}" ]    && die "\$HOME is not set. Aborting."
[ -z "${SHELL:-}" ]   && warn "\$SHELL is not set — defaulting to bash."

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
if [ -d "${INSTALL_DIR}/.git" ] && have_git; then
    info "PocketCli already installed at ${INSTALL_DIR} — updating via git..."
    update_git_install
elif have_git; then
    info "Cloning PocketCli into ${INSTALL_DIR}..."
    if ! git clone --quiet --branch "${POCKETCLI_VERSION}" "${REPO_URL}" "${INSTALL_DIR}"; then
        warn "git clone failed; retrying with archive download."
        download_archive_install
    fi
else
    warn "git not found — using archive download fallback."
    download_archive_install
fi

success "Repository ready."
chmod 700 "${INSTALL_DIR}"

# ---------------------------------------------------------------------------
# Hand off to install.sh
# ---------------------------------------------------------------------------
INSTALL_SCRIPT="${INSTALL_DIR}/install.sh"

[ -f "${INSTALL_SCRIPT}" ] || die "install.sh not found in cloned repository."
chmod 700 "${INSTALL_SCRIPT}"

exec "${INSTALL_SCRIPT}"

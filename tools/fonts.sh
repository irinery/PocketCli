#!/usr/bin/env bash
# =============================================================================
# PocketCli — tools/fonts.sh
# Installs a Nerd Font for Starship icons.
# Optional — only needed on the machine running the terminal emulator.
# =============================================================================

set -euo pipefail

FONT_NAME="JetBrainsMono"
FONT_VERSION="3.2.1"
FONT_URL="https://github.com/ryanoasis/nerd-fonts/releases/download/v${FONT_VERSION}/${FONT_NAME}.zip"
FONT_DIR="${HOME}/.local/share/fonts"

printf '[PocketCli] Installing %s Nerd Font v%s...\n' "${FONT_NAME}" "${FONT_VERSION}"

command -v curl >/dev/null 2>&1 || { printf 'curl is required.\n' >&2; exit 1; }
command -v unzip >/dev/null 2>&1 || { printf 'unzip is required.\n' >&2; exit 1; }

mkdir -p "${FONT_DIR}"

TMP=$(mktemp -d)
curl -fsSL "${FONT_URL}" -o "${TMP}/font.zip"
unzip -q "${TMP}/font.zip" -d "${TMP}/font"

find "${TMP}/font" -name "*.ttf" -exec cp {} "${FONT_DIR}/" \;

rm -rf "${TMP}"

# Refresh font cache on Linux
if command -v fc-cache >/dev/null 2>&1; then
    fc-cache -fv "${FONT_DIR}" >/dev/null 2>&1
    printf '[PocketCli] Font cache refreshed.\n'
fi

printf '[PocketCli] Font installed. Set "%s Nerd Font" in your terminal emulator.\n' "${FONT_NAME}"

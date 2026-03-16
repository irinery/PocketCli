#!/usr/bin/env bash
# =============================================================================
# PocketCli — scripts/start_viewer.sh
# Starts the Viewer mode (iPad / lightweight terminal).
# =============================================================================

set -euo pipefail

printf '\n[PocketCli] Starting Viewer environment...\n'

# SSH keys setup
mkdir -p "${HOME}/.ssh"
chmod 700 "${HOME}/.ssh"

# Ensure agent forwarding is enabled in SSH config
SSH_CONFIG="${HOME}/.ssh/config"
if ! grep -qF "ForwardAgent yes" "${SSH_CONFIG}" 2>/dev/null; then
    cat >> "${SSH_CONFIG}" <<SSHEOF

# PocketCli — added by installer
Host *
    ForwardAgent yes
    ServerAliveInterval 60
    ServerAliveCountMax 3
SSHEOF
    chmod 600 "${SSH_CONFIG}"
fi

# ---------------------------------------------------------------------------
# Display the interactive menu to select a machine
# ---------------------------------------------------------------------------
exec "${HOME}/.pocketcli/scripts/pocketcli_menu.sh"

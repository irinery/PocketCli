#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/start_viewer.sh
# Starts Viewer mode. Auto-starts tailscaled if not running.
# =============================================================================

set -eu

POCKETCLI_DIR="${HOME}/.pocketcli"

# Load helpers
. "${POCKETCLI_DIR}/lib/common.sh"

# Ensure pocket is reachable in this session
export PATH="${POCKETCLI_DIR}:${PATH}"

printf '\n[PocketCli] Starting Viewer environment...\n'

# ---------------------------------------------------------------------------
# SSH setup
# ---------------------------------------------------------------------------
mkdir -p "${HOME}/.ssh"
chmod 700 "${HOME}/.ssh"

SSH_CONFIG="${HOME}/.ssh/config"
if ! grep -qF "ForwardAgent" "${SSH_CONFIG}" 2>/dev/null; then
    cat >> "${SSH_CONFIG}" << 'SSHEOF'

# PocketCli
Host *
    ForwardAgent yes
    ServerAliveInterval 60
    ServerAliveCountMax 3
SSHEOF
    chmod 600 "${SSH_CONFIG}"
fi

# ---------------------------------------------------------------------------
# Auto-start tailscaled if installed but not running
# ---------------------------------------------------------------------------
if command -v tailscaled >/dev/null 2>&1; then
    if ! is_tailscale_daemon_running; then
        printf '[PocketCli] tailscaled not running — starting...\n'
        sh "${POCKETCLI_DIR}/scripts/tailscale_daemon.sh" start \
            && printf '[PocketCli] tailscaled started.\n' \
            || printf '[PocketCli] Could not start tailscaled. Run: pocket tailscale-start\n'
    else
        printf '[PocketCli] tailscaled already running.\n'
    fi
else
    printf '[PocketCli] Tailscale not installed. Run: pocket tailscale-setup\n'
fi

# ---------------------------------------------------------------------------
# Show quick status
# ---------------------------------------------------------------------------
if command -v tailscale >/dev/null 2>&1 && is_tailscale_daemon_running; then
    TS_IP=$(tailscale ip -4 2>/dev/null | head -1 | tr -cd '0-9.' || true)
    if [ -n "${TS_IP}" ]; then
        printf '[PocketCli] Tailscale IP: %s\n' "${TS_IP}"
    else
        printf '[PocketCli] Tailscale not authenticated. Run: pocket tailscale-setup\n'
    fi
fi

echo ""

# ---------------------------------------------------------------------------
# Launch menu
# ---------------------------------------------------------------------------
exec sh "${POCKETCLI_DIR}/scripts/pocketcli_menu.sh"
#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/start_viewer.sh
# Viewer mode startup. Ensures interactive installs land in the main menu by default. Handles both:
#   - iSH (iPad): iOS Tailscale app provides VPN, no daemon possible
#   - Normal: starts tailscaled if needed
# =============================================================================

set -eu

POCKETCLI_DIR="${HOME}/.pocketcli"
. "${POCKETCLI_DIR}/lib/common.sh"
export PATH="${POCKETCLI_DIR}:${PATH}"

# ---------------------------------------------------------------------------
# SSH config
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
# Tailscale connectivity check
# ---------------------------------------------------------------------------
echo ""

if is_ish; then
    # iSH: daemon will never work — check iOS VPN instead
    TS_IP=$(get_tailscale_ip)
    if [ -n "${TS_IP}" ]; then
        ok "Tailscale active via iOS app (IP: ${TS_IP})"

        # Probe saved hosts to confirm routing works
        HOSTS_FILE="${POCKETCLI_DIR}/hosts"
        if [ -f "${HOSTS_FILE}" ]; then
            FIRST=$(grep -v '^\s*#' "${HOSTS_FILE}" | grep -v '^\s*$' | head -1)
            if [ -n "${FIRST}" ]; then
                printf "  Testing route to %s... " "${FIRST}"
                if ping_host "${FIRST}" 3; then
                    printf "${C_GREEN}OK${C_NC}\n"
                else
                    printf "${C_YELLOW}no response${C_NC} (may still be reachable via SSH)\n"
                fi
            fi
        fi
    else
        warn "Not on Tailscale network."
        echo ""
        echo "  To connect:"
        echo "  1. Open the Tailscale app on your iPad"
        echo "  2. Enable the VPN"
        echo "  3. Return here and press Enter to continue"
        printf "\n  Press Enter when connected... "
        read -r _D < /dev/tty

        TS_IP=$(get_tailscale_ip)
        if [ -n "${TS_IP}" ]; then
            ok "Connected! IP: ${TS_IP}"
        else
            warn "Still not connected — continuing anyway (SSH may still work on LAN)"
        fi
    fi
else
    # Non-iSH: start daemon if needed
    if command -v tailscaled >/dev/null 2>&1; then
        if ! is_tailscale_daemon_running; then
            info "Starting tailscaled..."
            sh "${POCKETCLI_DIR}/scripts/tailscale_daemon.sh" start \
                || warn "Could not start tailscaled. Run: pocket tailscale-setup"
        fi
        TS_IP=$(get_tailscale_ip)
        if [ -n "${TS_IP}" ]; then
            ok "Tailscale IP: ${TS_IP}"
        else
            warn "Tailscale not authenticated. Run: pocket tailscale-setup"
        fi
    else
        warn "Tailscale not installed. Run: pocket tailscale-setup"
    fi
fi

echo ""
exec sh "${POCKETCLI_DIR}/scripts/pocketcli_menu.sh"

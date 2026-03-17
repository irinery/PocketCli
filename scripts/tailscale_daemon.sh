#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/tailscale_daemon.sh
# Manages the tailscaled daemon lifecycle on iSH / Alpine without init system.
#
# Usage:
#   pocket tailscale-setup     → full setup (install + start + auth)
#   pocket tailscale-start     → start daemon only
#   pocket tailscale-status    → show status + IP
#   pocket tailscale-restart   → kill + restart daemon
# =============================================================================

set -eu

POCKETCLI_DIR="${HOME}/.pocketcli"
. "${POCKETCLI_DIR}/lib/common.sh"

PID_FILE="/tmp/tailscaled.pid"
LOG_FILE="/tmp/tailscaled.log"

# =============================================================================
# Daemon lifecycle
# =============================================================================

_daemon_start() {
    if is_tailscale_daemon_running; then
        ok "tailscaled already running (PID $(pgrep tailscaled | head -1))"
        return 0
    fi

    command -v tailscaled >/dev/null 2>&1 || die "tailscaled not installed. Run: pocket tailscale-setup"

    info "Starting tailscaled (userspace networking)..."

    # Rotate log
    : > "${LOG_FILE}"

    tailscaled \
        --tun=userspace-networking \
        --socks5-server=localhost:1055 \
        --outbound-http-proxy-listen=localhost:1055 \
        >> "${LOG_FILE}" 2>&1 &

    DAEMON_PID=$!
    printf '%d\n' "${DAEMON_PID}" > "${PID_FILE}"

    info "Waiting for daemon (PID ${DAEMON_PID})..."
    I=0
    while [ "${I}" -lt 20 ]; do
        sleep 1
        printf '.'
        # Check if process is still alive
        if ! kill -0 "${DAEMON_PID}" 2>/dev/null; then
            printf '\n'
            warn "tailscaled exited early. Log:"
            cat "${LOG_FILE}" | head -20
            return 1
        fi
        # Check if socket is responding
        if tailscale status >/dev/null 2>&1; then
            printf '\n'
            ok "tailscaled is ready."
            return 0
        fi
        I=$((I + 1))
    done

    printf '\n'
    warn "tailscaled started but not responding after 20s."
    warn "Check log: ${LOG_FILE}"
    return 1
}

_daemon_stop() {
    info "Stopping tailscaled..."

    # Try PID file first
    if [ -f "${PID_FILE}" ]; then
        PID=$(cat "${PID_FILE}" 2>/dev/null || echo "")
        if [ -n "${PID}" ]; then
            kill "${PID}" 2>/dev/null || true
            sleep 1
        fi
        rm -f "${PID_FILE}"
    fi

    # Kill any remaining tailscaled processes
    pkill -x tailscaled 2>/dev/null || true
    sleep 1

    if is_tailscale_daemon_running; then
        warn "tailscaled still running — sending SIGKILL"
        pkill -9 -x tailscaled 2>/dev/null || true
    fi

    ok "tailscaled stopped."
}

_daemon_status() {
    step "Tailscale Status"

    if ! command -v tailscale >/dev/null 2>&1; then
        warn "tailscale not installed."; return 1
    fi

    if is_tailscale_daemon_running; then
        PID=$(pgrep tailscaled | head -1 || echo "?")
        ok "tailscaled running (PID ${PID})"
    else
        warn "tailscaled NOT running"
        return 1
    fi

    if ! with_timeout 5 tailscale status >/dev/null 2>&1; then
        warn "Daemon running but socket not responding"
        return 1
    fi

    # Show IP and peers
    TS_IP=$(tailscale ip -4 2>/dev/null | head -1 || echo "not assigned")
    printf "\n  ${C_BOLD}%-18s${C_NC} %s\n" "This device IP:" "${TS_IP}"

    echo ""
    printf "  ${C_BOLD}%-22s %-16s %-8s${C_NC}\n" "Hostname" "IP" "Online"
    printf "  %-22s %-16s %-8s\n" "----------------------" "----------------" "--------"

    tailscale status 2>/dev/null \
        | grep -v '^#\|^$\|offers exit' \
        | while IFS= read -r line; do
            HOST=$(printf '%s' "${line}" | awk '{print $2}' | tr -cd 'a-zA-Z0-9._-')
            IP=$(printf '%s' "${line}"   | awk '{print $1}' | tr -cd '0-9.:')
            [ -z "${HOST}" ] && continue
            ACTIVE=$(printf '%s' "${line}" | grep -c 'active' || true)
            STATUS=$([ "${ACTIVE}" -gt 0 ] && echo "yes" || echo "-")
            printf "  %-22s %-16s %-8s\n" "${HOST}" "${IP}" "${STATUS}"
          done

    echo ""
}

# =============================================================================
# Full setup — called by installer and by 'pocket tailscale-setup'
# =============================================================================

_full_setup() {
    step "Tailscale Full Setup"

    # 1. Install if needed
    if ! command -v tailscale >/dev/null 2>&1; then
        info "Installing tailscale..."
        if command -v apk >/dev/null 2>&1; then
            run_or_die "apk add tailscale" apk add --no-cache tailscale qrencode
        elif command -v apt-get >/dev/null 2>&1; then
            curl -fsSL https://tailscale.com/install.sh | sh
        else
            die "Cannot install tailscale. Install manually: https://tailscale.com/download"
        fi
    else
        ok "tailscale already installed."
    fi

    # 2. Start daemon
    _daemon_start || die "Could not start tailscaled. Check: ${LOG_FILE}"

    # 3. Authenticate
    _authenticate

    # 4. Final status
    _daemon_status
}

# =============================================================================
# Authentication with QR code
# =============================================================================

_authenticate() {
    step "Authentication"

    # Already connected?
    if with_timeout 5 tailscale status >/dev/null 2>&1; then
        TS_IP=$(tailscale ip -4 2>/dev/null | head -1 || true)
        if [ -n "${TS_IP}" ]; then
            ok "Already authenticated (IP: ${TS_IP})"
            return 0
        fi
    fi

    info "Running tailscale up..."
    AUTH_LOG="/tmp/ts_auth_$$.log"
    : > "${AUTH_LOG}"

    # Start tailscale up, capture output
    tailscale up --ssh > "${AUTH_LOG}" 2>&1 &
    TS_UP_PID=$!

    # Poll for auth URL up to 15 seconds
    I=0
    AUTH_URL=""
    while [ "${I}" -lt 15 ]; do
        sleep 1
        AUTH_URL=$(grep -o 'https://login\.tailscale\.com/[^ ]*' "${AUTH_LOG}" 2>/dev/null \
            | head -1 || true)
        [ -n "${AUTH_URL}" ] && break
        I=$((I + 1))
    done

    if [ -n "${AUTH_URL}" ]; then
        _show_auth_url "${AUTH_URL}"

        info "Waiting for authentication (up to 3 min)..."
        # Wait for tailscale up to complete (user scans QR / visits URL)
        I=0
        while [ "${I}" -lt 180 ]; do
            sleep 2
            if with_timeout 3 tailscale status >/dev/null 2>&1; then
                TS_IP=$(tailscale ip -4 2>/dev/null | head -1 || true)
                if [ -n "${TS_IP}" ]; then
                    echo ""
                    ok "Authenticated! IP: ${TS_IP}"
                    rm -f "${AUTH_LOG}"
                    return 0
                fi
            fi
            I=$((I + 2))
            printf '.'
        done
        printf '\n'
        warn "Timeout waiting for authentication. Check: tailscale status"
    else
        # No URL — might already be authenticated or error
        wait "${TS_UP_PID}" 2>/dev/null || true
        if with_timeout 5 tailscale status >/dev/null 2>&1; then
            ok "Authenticated (no URL needed)"
        else
            warn "Could not get auth URL. Run: tailscale up --ssh"
            cat "${AUTH_LOG}" | head -10
        fi
    fi

    rm -f "${AUTH_LOG}"
    kill "${TS_UP_PID}" 2>/dev/null || true
}

_show_auth_url() {
    URL="$1"
    echo ""
    printf "  ${C_BOLD}┌─────────────────────────────────────┐${C_NC}\n"
    printf "  ${C_BOLD}│  Scan QR or visit the URL below     │${C_NC}\n"
    printf "  ${C_BOLD}└─────────────────────────────────────┘${C_NC}\n"
    echo ""

    QR_OK=0

    # Try qrencode (best quality — crisp blocks)
    if command -v qrencode >/dev/null 2>&1; then
        qrencode -t UTF8 -m 1 -o - "${URL}" 2>/dev/null \
            | while IFS= read -r line; do printf "  %s\n" "${line}"; done \
            && QR_OK=1
    fi

    # Try installing qrencode if on Alpine/iSH
    if [ "${QR_OK}" -eq 0 ] && command -v apk >/dev/null 2>&1; then
        info "Installing qrencode..."
        if apk add --no-cache qrencode >/dev/null 2>&1; then
            qrencode -t UTF8 -m 1 -o - "${URL}" 2>/dev/null \
                | while IFS= read -r line; do printf "  %s\n" "${line}"; done \
                && QR_OK=1
        fi
    fi

    # Fallback: print URL large and clearly
    if [ "${QR_OK}" -eq 0 ]; then
        echo ""
        printf "  ${C_YELLOW}Open this URL in your browser:${C_NC}\n"
        echo ""
        printf "  ${C_CYAN}${C_BOLD}%s${C_NC}\n" "${URL}"
        echo ""
    fi
}

# =============================================================================
# Dispatch
# =============================================================================

CMD="${1:-help}"
shift 2>/dev/null || true

case "${CMD}" in
    start)   _daemon_start   ;;
    stop)    _daemon_stop    ;;
    restart) _daemon_stop; _daemon_start ;;
    status)  _daemon_status  ;;
    auth)    _authenticate   ;;
    setup)   _full_setup     ;;
    help|*)
        echo ""
        printf "  Usage: sh %s <command>\n" "$0"
        echo ""
        printf "  start    Start tailscaled daemon\n"
        printf "  stop     Stop tailscaled daemon\n"
        printf "  restart  Restart daemon\n"
        printf "  status   Show status and peers\n"
        printf "  auth     Re-authenticate (shows QR code)\n"
        printf "  setup    Full install + start + auth\n"
        echo ""
    ;;
esac

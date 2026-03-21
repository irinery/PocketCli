#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/tailscale_daemon.sh
# Manages tailscaled lifecycle.
#
# On iSH (iPad): tailscaled CANNOT run — kernel lacks netlink support.
# The iOS Tailscale app handles VPN. This script detects that and skips
# the daemon, falling back to ping-based connectivity detection.
# =============================================================================

set -eu

POCKETCLI_DIR="${HOME}/.pocketcli"
. "${POCKETCLI_DIR}/lib/common.sh"

PID_FILE="/tmp/tailscaled.pid"
LOG_FILE="/tmp/tailscaled.log"

# =============================================================================
# iSH guard — called at the top of any command that needs the daemon
# =============================================================================
_assert_not_ish() {
    if is_ish; then
        echo ""
        warn "iSH detected — tailscaled cannot run on this kernel."
        info "The Tailscale iOS app handles VPN for this device."
        info "To check connectivity: pocket ts-ping <hostname>"
        info "To list saved hosts:   pocket menu"
        echo ""
        exit 0
    fi
}

# =============================================================================
# Daemon lifecycle
# =============================================================================

_daemon_start() {
    _assert_not_ish

    if is_tailscale_daemon_running; then
        ok "tailscaled already running (PID $(pgrep tailscaled | head -1))"
        return 0
    fi

    command -v tailscaled >/dev/null 2>&1 || die "tailscaled not installed. Run: pocket tailscale-setup"

    info "Starting tailscaled (userspace networking)..."
    : > "${LOG_FILE}"

    # Minimal flags — compatible with old tailscaled (Alpine 3.14 / v1.8.x)
    tailscaled \
        -tun=userspace-networking \
        -socks5-server=localhost:1055 \
        >> "${LOG_FILE}" 2>&1 &

    DAEMON_PID=$!
    printf '%d\n' "${DAEMON_PID}" > "${PID_FILE}"

    info "Waiting for daemon (PID ${DAEMON_PID})..."
    I=0
    while [ "${I}" -lt 20 ]; do
        sleep 1; printf '.'
        if ! kill -0 "${DAEMON_PID}" 2>/dev/null; then
            printf '\n'
            warn "tailscaled exited early. Log:"
            head -20 "${LOG_FILE}"
            return 1
        fi
        if tailscale status >/dev/null 2>&1; then
            printf '\n'; ok "tailscaled is ready."; return 0
        fi
        I=$((I + 1))
    done
    printf '\n'
    warn "tailscaled not responding after 20s. Check: ${LOG_FILE}"
    return 1
}

_daemon_stop() {
    _assert_not_ish
    info "Stopping tailscaled..."
    if [ -f "${PID_FILE}" ]; then
        PID=$(cat "${PID_FILE}" 2>/dev/null || true)
        [ -n "${PID}" ] && kill "${PID}" 2>/dev/null || true
        rm -f "${PID_FILE}"
    fi
    pkill tailscaled 2>/dev/null || true
    sleep 1
    is_tailscale_daemon_running && pkill -9 tailscaled 2>/dev/null || true
    ok "tailscaled stopped."
}

_daemon_status() {
    step "Tailscale Status"

    # iSH path — no daemon, check via iOS VPN
    if is_ish; then
        warn "iSH: tailscaled daemon not available (kernel limitation)"
        TS_IP=$(get_tailscale_ip)
        if [ -n "${TS_IP}" ]; then
            ok "On Tailscale network via iOS app (IP: ${TS_IP})"
        else
            warn "Not on Tailscale network. Open the Tailscale iOS app and connect."
        fi
        echo ""
        info "Saved hosts (pocket menu for interactive selection):"
        HOSTS_FILE="${POCKETCLI_DIR}/hosts"
        if [ -f "${HOSTS_FILE}" ] && grep -qv '^\s*$' "${HOSTS_FILE}" 2>/dev/null; then
            grep -v '^\s*#' "${HOSTS_FILE}" | grep -v '^\s*$' \
                | while IFS= read -r h; do
                    REACHABLE=""
                    ping_host "${h}" 2 && REACHABLE=" ${C_GREEN}[reachable]${C_NC}" \
                                       || REACHABLE=" ${C_YELLOW}[no response]${C_NC}"
                    printf "  %-25s%b\n" "${h}" "${REACHABLE}"
                  done
        else
            info "No saved hosts yet. Use option 2 in pocket menu to add them."
        fi
        echo ""
        return 0
    fi

    # Normal path
    if ! command -v tailscale >/dev/null 2>&1; then
        warn "tailscale not installed."; return 1
    fi
    if is_tailscale_daemon_running; then
        PID=$(pgrep tailscaled | head -1 || echo "?")
        ok "tailscaled running (PID ${PID})"
    else
        warn "tailscaled NOT running"; return 1
    fi
    if ! with_timeout 5 tailscale status >/dev/null 2>&1; then
        warn "Daemon running but socket not responding"; return 1
    fi

    TS_IP=$(get_tailscale_ip)
    printf "\n  ${C_BOLD}%-18s${C_NC} %s\n" "This device IP:" "${TS_IP:-not assigned}"
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
# ts-ping — reachability check without tailscale CLI
# =============================================================================
_ts_ping() {
    HOST=$(safe_host "${1:-}")
    [ -z "${HOST}" ] && die "Usage: pocket ts-ping <hostname>"

    info "Pinging ${HOST}..."
    if ping_host "${HOST}" 5; then
        ok "${HOST} is reachable on Tailscale network"
        return 0
    else
        warn "${HOST} did not respond"
        return 1
    fi
}

_install_tailscale() {
    if command -v tailscale >/dev/null 2>&1; then
        ok "tailscale already installed."
        return 0
    fi

    info "Installing tailscale..."
    if command -v apk >/dev/null 2>&1; then
        if apk add --no-cache tailscale >/dev/null 2>&1; then
            ok "apk add tailscale"
            apk add --no-cache qrencode 2>/dev/null \
                && ok "qrencode installed" \
                || warn "qrencode not available — URL shown as text"
            return 0
        fi
        warn "Could not install tailscale via apk"
        return 1
    elif command -v apt-get >/dev/null 2>&1; then
        if curl -fsSL https://tailscale.com/install.sh | sh; then
            apt-get install -y --no-install-recommends qrencode 2>/dev/null || true
            return 0
        fi
        warn "Could not install tailscale via install.sh"
        return 1
    fi

    warn "Cannot install tailscale automatically on this system."
    return 1
}

_prompt_fallback_targets() {
    if [ -n "${POCKETCLI_TAILSCALE_FALLBACK_TARGETS:-}" ]; then
        printf '%s
' "${POCKETCLI_TAILSCALE_FALLBACK_TARGETS}"
        return 0
    fi

    if [ ! -r /dev/tty ] || [ ! -w /dev/tty ]; then
        return 1
    fi

    printf '
'
    printf '  ${C_BOLD}Fallback sem app Tailscale${C_NC}
'
    printf '  Informe um host/IP conhecido da tailnet (ex.: 100.113.114.52).
'
    printf '  Você pode informar vários valores separados por espaço ou vírgula.

'
    printf '  Hosts/IPs: ' > /dev/tty
    IFS= read -r INPUT < /dev/tty || return 1
    printf '%s
' "${INPUT}"
}

_save_fallback_targets() {
    RAW_INPUT=${1:-}
    [ -z "${RAW_INPUT}" ] && return 1

    TARGETS=$(printf '%s' "${RAW_INPUT}" | tr ',;' '  ')
    SAVED=0
    for target in ${TARGETS}; do
        target=$(safe_host "${target}")
        [ -z "${target}" ] && continue
        save_known_target "${target}" || continue
        if is_ip_target "${target}"; then
            save_known_target "${target}" "$(_fallback_seeds_file)" || true
        fi
        SAVED=$((SAVED + 1))
    done

    [ "${SAVED}" -gt 0 ]
}

_setup_install_fallback() {
    warn "Tailscale installation failed; continuing with connectivity fallback."
    info "PocketCli can keep scanning known hosts/IPs without tailscale status."

    RAW_TARGETS=$(_prompt_fallback_targets || true)
    if [ -z "${RAW_TARGETS}" ]; then
        warn "No fallback host/IP provided. Add one later in ~/.pocketcli/hosts or rerun pocket tailscale-setup."
        return 1
    fi

    if ! _save_fallback_targets "${RAW_TARGETS}"; then
        warn "No valid fallback host/IP was saved."
        return 1
    fi

    REACHABLE=1
    for target in $(printf '%s' "${RAW_TARGETS}" | tr ',;' '  '); do
        target=$(safe_host "${target}")
        [ -z "${target}" ] && continue
        info "Testing reachability for ${target}..."
        if ping_host "${target}" 3; then
            ok "${target} is reachable and was saved for radar/scan fallback"
            REACHABLE=0
        else
            warn "${target} did not respond, but it was saved for later fallback scans"
        fi
    done

    if [ "${REACHABLE}" -eq 0 ]; then
        info "Use 'pocket scan' or 'pocket radar' to continue without tailscale status."
        return 0
    fi

    warn "No fallback host responded yet. Review the saved IP/host and try again."
    return 1
}

# =============================================================================
# Full setup
# =============================================================================
_full_setup() {
    step "Tailscale Full Setup"

    # On iSH — no daemon possible, just validate iOS connectivity
    if is_ish; then
        echo ""
        info "iSH (iPad) detected."
        info "tailscaled cannot run on this kernel (netlink not supported)."
        echo ""
        TS_IP=$(get_tailscale_ip)
        if [ -n "${TS_IP}" ]; then
            ok "Already on Tailscale network via iOS app (IP: ${TS_IP})"
            info "You can SSH into any Tailscale machine directly."
            info "Add hosts with: pocket menu  (option 2)"
        else
            echo ""
            printf "  ${C_YELLOW}Action required:${C_NC}\n"
            echo "  1. Install the Tailscale app from the App Store"
            echo "  2. Sign in and enable the VPN"
            echo "  3. Re-run: pocket tailscale-setup"
            echo ""
        fi
        return 0
    fi

    # Normal install path
    if ! _install_tailscale; then
        _setup_install_fallback && return 0
        die "Cannot install tailscale automatically. See: https://tailscale.com/download"
    fi

    _daemon_start || die "Could not start tailscaled. Check: ${LOG_FILE}"
    _authenticate
    _daemon_status
}

# =============================================================================
# Authentication with QR code
# =============================================================================
_authenticate() {
    _assert_not_ish

    if with_timeout 5 tailscale status >/dev/null 2>&1; then
        TS_IP=$(get_tailscale_ip)
        [ -n "${TS_IP}" ] && { ok "Already authenticated (IP: ${TS_IP})"; return 0; }
    fi

    info "Running tailscale up..."
    AUTH_LOG="/tmp/ts_auth_$$.log"
    : > "${AUTH_LOG}"

    tailscale up --ssh > "${AUTH_LOG}" 2>&1 &
    TS_UP_PID=$!

    I=0; AUTH_URL=""
    while [ "${I}" -lt 15 ]; do
        sleep 1
        AUTH_URL=$(grep -o 'https://login\.tailscale\.com/[^ ]*' "${AUTH_LOG}" 2>/dev/null | head -1 || true)
        [ -n "${AUTH_URL}" ] && break
        I=$((I + 1))
    done

    if [ -n "${AUTH_URL}" ]; then
        _show_auth_url "${AUTH_URL}"
        info "Waiting for authentication (up to 3 min)..."
        I=0
        while [ "${I}" -lt 180 ]; do
            sleep 2
            TS_IP=$(get_tailscale_ip)
            if [ -n "${TS_IP}" ]; then
                echo ""; ok "Authenticated! IP: ${TS_IP}"
                rm -f "${AUTH_LOG}"; return 0
            fi
            I=$((I + 2)); printf '.'
        done
        printf '\n'; warn "Timeout. Check: tailscale status"
    else
        wait "${TS_UP_PID}" 2>/dev/null || true
        TS_IP=$(get_tailscale_ip)
        if [ -n "${TS_IP}" ]; then ok "Authenticated (IP: ${TS_IP})"
        else warn "Could not get auth URL. Run: tailscale up --ssh"
        fi
    fi

    rm -f "${AUTH_LOG}"
    kill "${TS_UP_PID}" 2>/dev/null || true
}

_show_auth_url() {
    URL="$1"
    echo ""
    printf "  ${C_BOLD}+-------------------------------------+${C_NC}\n"
    printf "  ${C_BOLD}|  Scan QR or open the URL below     |${C_NC}\n"
    printf "  ${C_BOLD}+-------------------------------------+${C_NC}\n"
    echo ""

    QR_OK=0
    if command -v qrencode >/dev/null 2>&1; then
        qrencode -t UTF8 -m 1 -o - "${URL}" 2>/dev/null \
            | while IFS= read -r line; do printf "  %s\n" "${line}"; done \
            && QR_OK=1 || true
    fi

    if [ "${QR_OK}" -eq 0 ] && command -v apk >/dev/null 2>&1; then
        apk add --no-cache qrencode >/dev/null 2>&1 \
            && qrencode -t UTF8 -m 1 -o - "${URL}" 2>/dev/null \
                | while IFS= read -r line; do printf "  %s\n" "${line}"; done \
            && QR_OK=1 || true
    fi

    if [ "${QR_OK}" -eq 0 ]; then
        echo ""
        printf "  ${C_YELLOW}Open in browser:${C_NC}\n\n"
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
    ping)    _ts_ping "$@"   ;;
    help|*)
        echo ""
        printf "  Usage: pocket tailscale-<command>\n\n"
        printf "  setup    Full install + start + auth\n"
        printf "  start    Start daemon (non-iSH only)\n"
        printf "  stop     Stop daemon\n"
        printf "  restart  Restart daemon\n"
        printf "  status   Status + peers (iSH: shows connectivity)\n"
        printf "  auth     Re-authenticate (shows QR)\n"
        printf "  ping     Check host reachability via ping\n"
        echo ""
    ;;
esac

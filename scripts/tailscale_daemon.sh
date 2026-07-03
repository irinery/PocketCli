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
# iSH guard — called at the top of any command that needs a local backend
# =============================================================================
_assert_not_ish() {
    if is_ish; then
        echo ""
        warn "iSH detected — tailscaled cannot run on this kernel."
        info "The Tailscale iOS app handles VPN for this device."
        info "To check connectivity: pocket tailscale-ping <hostname> (or ts-ping)"
        info "To list saved hosts:   pocket menu"
        echo ""
        exit 0
    fi
}

# =============================================================================
# Daemon lifecycle
# =============================================================================

_run_privileged() {
    if [ "$(id -u 2>/dev/null || printf '1')" = "0" ]; then
        "$@"
    elif command -v sudo >/dev/null 2>&1; then
        sudo "$@"
    else
        "$@"
    fi
}

_is_windows_host() {
    case "$(uname -s 2>/dev/null || true)" in
        MINGW*|MSYS*|CYGWIN*) return 0 ;;
    esac
    [ -n "${WSL_DISTRO_NAME:-}" ] && return 0
    grep -qi microsoft /proc/version 2>/dev/null
}

_is_native_tailscale_client() {
    TS_CLI=$(tailscale_cli_path 2>/dev/null || true)
    case "${TS_CLI}" in
        */Tailscale.app/Contents/MacOS/Tailscale|*.exe) return 0 ;;
    esac
    return 1
}

_start_system_backend() {
    if [ "$(uname -s 2>/dev/null || true)" = "Darwin" ]; then
        if command -v open >/dev/null 2>&1 && _is_native_tailscale_client; then
            info "Opening the macOS Tailscale app..."
            open -gja Tailscale >/dev/null 2>&1
            return $?
        fi
        return 1
    fi

    if _is_windows_host; then
        info "Starting the Windows Tailscale service..."
        if command -v powershell.exe >/dev/null 2>&1; then
            powershell.exe -NoProfile -NonInteractive -Command \
                'Start-Service -Name Tailscale -ErrorAction Stop' >/dev/null 2>&1 && return 0
        fi
        if command -v net.exe >/dev/null 2>&1; then
            net.exe start Tailscale >/dev/null 2>&1 && return 0
        fi
        return 1
    fi

    if command -v systemctl >/dev/null 2>&1; then
        info "Starting the system tailscaled service..."
        _run_privileged systemctl start tailscaled >/dev/null 2>&1 && return 0
    fi
    if command -v rc-service >/dev/null 2>&1; then
        info "Starting the OpenRC Tailscale service..."
        _run_privileged rc-service tailscale start >/dev/null 2>&1 && return 0
    fi
    if command -v service >/dev/null 2>&1; then
        info "Starting the Tailscale service..."
        _run_privileged service tailscaled start >/dev/null 2>&1 && return 0
    fi
    return 1
}

_wait_for_backend() {
    MAX=${1:-10}
    I=0
    while [ "${I}" -lt "${MAX}" ]; do
        if is_tailscale_backend_responding; then
            return 0
        fi
        sleep 1
        I=$((I + 1))
    done
    return 1
}

_daemon_start() {
    _assert_not_ish

    if is_tailscale_mesh_operational; then
        TS_IP=$(get_tailscale_ip 2>/dev/null || true)
        ok "Tailscale mesh already operational${TS_IP:+ (IP: ${TS_IP})}."
        info "Using the existing system/app-managed backend; no extra daemon was started."
        return 0
    fi

    if is_tailscale_backend_responding; then
        ok "Tailscale backend is already responding (authentication pending)."
        return 0
    fi

    if _start_system_backend; then
        info "Waiting for the system/app-managed backend..."
        if _wait_for_backend 12; then
            ok "Tailscale backend is ready."
            return 0
        fi
    fi

    if _is_native_tailscale_client; then
        warn "The native Tailscale client is installed, but its backend did not start."
        return 1
    fi

    command -v tailscaled >/dev/null 2>&1 || {
        warn "tailscaled is not installed and no system/app-managed backend was found."
        return 1
    }

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
        if tailscale_cli status >/dev/null 2>&1; then
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

    if _is_native_tailscale_client; then
        info "Disconnecting the system-managed Tailscale client..."
        if tailscale_cli down >/dev/null 2>&1; then
            ok "Tailscale disconnected."
            return 0
        fi
        warn "Could not disconnect the native Tailscale client."
        return 1
    fi

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

    TS_IP=$(get_tailscale_ip 2>/dev/null || true)
    if ! is_tailscale_mesh_operational; then
        if is_tailscale_backend_responding; then
            warn "Tailscale backend is responding, but this device is not connected."
            info "Run: pocket tailscale-auth"
        elif has_tailscale_cli; then
            warn "Tailscale is installed, but the backend/mesh is unavailable."
            info "Run: pocket tailscale-start"
        else
            warn "Tailscale mesh and CLI were not detected."
            info "Run: pocket tailscale-setup"
        fi
        return 1
    fi

    if is_tailscale_backend_responding; then
        ok "Tailscale backend and mesh are operational."
    else
        ok "Tailscale mesh is operational through the system-managed VPN."
        info "The PocketCli fallback is using the OS network interface."
    fi

    printf "\n  ${C_BOLD}%-18s${C_NC} %s\n" "This device IP:" "${TS_IP:-not assigned}"
    echo ""

    if ! has_tailscale_cli; then
        info "Peer listing requires the optional Tailscale CLI integration."
        echo ""
        return 0
    fi

    printf "  ${C_BOLD}%-22s %-16s %-8s${C_NC}\n" "Hostname" "IP" "Online"
    printf "  %-22s %-16s %-8s\n" "----------------------" "----------------" "--------"
    tailscale_cli status 2>/dev/null \
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
    if has_tailscale_cli; then
        ok "tailscale already installed."
        return 0
    fi

    info "Installing tailscale..."
    if [ "$(uname -s 2>/dev/null || true)" = "Darwin" ]; then
        if command -v brew >/dev/null 2>&1 \
            && brew install --cask tailscale-app; then
            ok "Tailscale macOS app installed."
            return 0
        fi
        warn "Could not install the Tailscale macOS app automatically."
        return 1
    elif _is_windows_host; then
        if command -v winget.exe >/dev/null 2>&1 \
            && winget.exe install --exact --id Tailscale.Tailscale \
                --accept-package-agreements --accept-source-agreements; then
            ok "Tailscale Windows app installed."
            return 0
        fi
        warn "Could not install the Tailscale Windows app automatically."
        return 1
    elif command -v apk >/dev/null 2>&1; then
        if apk add --no-cache tailscale >/dev/null 2>&1; then
            ok "apk add tailscale"
            if apk add --no-cache qrencode 2>/dev/null; then
                ok "qrencode installed"
            else
                warn "qrencode not available — URL shown as text"
            fi
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

    printf '\n'
    printf '  %sFallback sem app Tailscale%s\n' "${C_BOLD}" "${C_NC}"
    printf '  Informe um host/IP conhecido da tailnet (ex.: 100.113.114.52).\n'
    printf '  Você pode informar vários valores separados por espaço ou vírgula.\n\n'
    printf '  Hosts/IPs: ' > /dev/tty
    IFS= read -r INPUT < /dev/tty || return 1
    printf '%s\n' "${INPUT}"
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

    TARGETS_FILE=$(mktemp)
    printf '%s\n' "${RAW_TARGETS}" | tr ',;' '  ' | tr ' ' '\n' > "${TARGETS_FILE}"

    REACHABLE=1
    while IFS= read -r target || [ -n "${target}" ]; do
        target=$(safe_host "${target}")
        [ -z "${target}" ] && continue
        info "Testing reachability for ${target}..."
        if ping_host "${target}" 3; then
            ok "${target} is reachable and was saved for radar/scan fallback"
            REACHABLE=0
        else
            warn "${target} did not respond, but it was saved for later fallback scans"
        fi
    done < "${TARGETS_FILE}"
    rm -f "${TARGETS_FILE}"

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

    if is_tailscale_mesh_operational; then
        TS_IP=$(get_tailscale_ip 2>/dev/null || true)
        ok "Tailscale mesh already operational${TS_IP:+ (IP: ${TS_IP})}."
        info "Keeping the existing system/app-managed installation."
        _daemon_status
        return 0
    fi

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
            printf '  %sAction required:%s\n' "${C_YELLOW}" "${C_NC}"
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

    _daemon_start || die "Could not start the Tailscale backend. Run: pocket tailscale-status"
    _authenticate
    _daemon_status
}

# =============================================================================
# Authentication with QR code
# =============================================================================
_authenticate() {
    _assert_not_ish

    if is_tailscale_mesh_operational; then
        TS_IP=$(get_tailscale_ip)
        [ -n "${TS_IP}" ] && { ok "Already authenticated (IP: ${TS_IP})"; return 0; }
    fi

    info "Running tailscale up..."
    AUTH_LOG="/tmp/ts_auth_$$.log"
    : > "${AUTH_LOG}"

    TS_CLI=$(tailscale_cli_path 2>/dev/null || true)
    case "${TS_CLI}" in
        */Tailscale.app/Contents/MacOS/Tailscale|*.exe)
            tailscale_cli up > "${AUTH_LOG}" 2>&1 &
        ;;
        *)
            tailscale_cli up --ssh > "${AUTH_LOG}" 2>&1 &
        ;;
    esac
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
        else warn "Could not authenticate. Run: pocket tailscale-auth"
        fi
    fi

    rm -f "${AUTH_LOG}"
    kill "${TS_UP_PID}" 2>/dev/null || true
}

_show_auth_url() {
    URL="$1"
    echo ""
    printf '  %s+-------------------------------------%s\n' "${C_BOLD}" "${C_NC}"
    printf '  %s|  Scan QR or open the URL below     |%s\n' "${C_BOLD}" "${C_NC}"
    printf '  %s+-------------------------------------%s\n' "${C_BOLD}" "${C_NC}"
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
        printf '  %sOpen in browser:%s\n\n' "${C_YELLOW}" "${C_NC}"
        printf '  %s%s%s%s\n' "${C_CYAN}" "${C_BOLD}" "${URL}" "${C_NC}"
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
    restart) _daemon_stop; _daemon_start; _authenticate ;;
    status)  _daemon_status  ;;
    auth)    _authenticate   ;;
    setup)   _full_setup     ;;
    ping)    _ts_ping "$@"   ;;
    help|*)
        echo ""
        printf "  Usage: pocket tailscale-<command>\n\n"
        printf "  setup    Full install + start + auth\n"
        printf "  start    Start the available Tailscale backend\n"
        printf "  stop     Stop or disconnect the Tailscale backend\n"
        printf "  restart  Restart and reconnect the Tailscale backend\n"
        printf "  status   Status + peers (iSH: shows connectivity)\n"
        printf "  auth     Re-authenticate (shows QR)\n"
        printf "  ping     Check host reachability via ping\n"
        echo ""
    ;;
esac

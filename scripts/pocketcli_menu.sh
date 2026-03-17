#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/pocketcli_menu.sh
# Plain read menu. No fzf, no tailscale CLI — works on iSH.
# =============================================================================

set -eu

POCKETCLI_DIR="${HOME}/.pocketcli"
. "${POCKETCLI_DIR}/lib/common.sh"

# Ensure pocket is in PATH for this session
export PATH="${POCKETCLI_DIR}:${PATH}"

HOSTS_FILE="${POCKETCLI_DIR}/hosts"

_header() {
    clear
    echo ""
    echo "  =================================="
    echo "        P o c k e t C l i"
    echo "  =================================="
    echo ""
}

# ---------------------------------------------------------------------------
# Try to list hosts: saved file first, then tailscale CLI if available
# ---------------------------------------------------------------------------
_list_hosts() {
    # Tailscale CLI available (non-iSH)
    if command -v tailscale >/dev/null 2>&1 && command -v jq >/dev/null 2>&1; then
        tailscale status --json 2>/dev/null \
            | jq -r '.Peer | to_entries[] | .value | select(.Online) | .HostName' \
            | sort \
            | while IFS= read -r h; do
                printf '%s' "${h}" | tr -cd 'a-zA-Z0-9._-'; printf '\n'
              done
        return
    fi
    # Fallback: saved hosts file
    if [ -f "${HOSTS_FILE}" ]; then
        grep -v '^\s*#' "${HOSTS_FILE}" | grep -v '^\s*$' \
            | while IFS= read -r h; do
                printf '%s' "${h}" | tr -cd 'a-zA-Z0-9._-'; printf '\n'
              done
        return
    fi
    # Nothing
    return 1
}

# ---------------------------------------------------------------------------
# Pick a host by number or let user type one
# ---------------------------------------------------------------------------
_pick_host() {
    HOSTS=$(_list_hosts 2>/dev/null || true)

    if [ -n "${HOSTS}" ]; then
        echo ""
        echo "  Saved / online machines:"
        echo ""
        I=1
        printf '%s\n' "${HOSTS}" | while IFS= read -r h; do
            printf "    %d)  %s\n" "${I}" "${h}"
            I=$((I + 1))
        done
        echo ""
        printf "  Number or type hostname: "
    else
        echo ""
        printf "  Hostname (e.g. server-01): "
    fi

    read -r INPUT < /dev/tty
    [ -z "${INPUT}" ] && return 1

    # If input is a number, pick from list
    case "${INPUT}" in
        ''|*[!0-9]*)
            # Treat as literal hostname
            printf '%s' "${INPUT}" | tr -cd 'a-zA-Z0-9._-'
        ;;
        *)
            printf '%s\n' "${HOSTS}" | sed -n "${INPUT}p" | tr -cd 'a-zA-Z0-9._-'
        ;;
    esac
}

# ---------------------------------------------------------------------------
# Manage saved hosts
# ---------------------------------------------------------------------------
_manage_hosts() {
    while true; do
        _header
        echo "  Saved hosts:"
        echo ""
        if [ -f "${HOSTS_FILE}" ]; then
            grep -v '^\s*#' "${HOSTS_FILE}" | grep -v '^\s*$' \
                | awk '{printf "    %d)  %s\n", NR, $0}' || echo "  (none)"
        else
            echo "    (none)"
        fi
        echo ""
        echo "    a)  Add host"
        echo "    d)  Delete host"
        echo "    b)  Back"
        echo ""
        printf "  Choice: "
        read -r A < /dev/tty
        case "${A}" in
            a|A)
                printf "  Hostname to add: "
                read -r NH < /dev/tty
                NH=$(printf '%s' "${NH}" | tr -cd 'a-zA-Z0-9._-')
                [ -z "${NH}" ] && continue
                printf '%s\n' "${NH}" >> "${HOSTS_FILE}"
                printf '  Added: %s\n' "${NH}"
                sleep 1
            ;;
            d|D)
                printf "  Line number to delete: "
                read -r DN < /dev/tty
                case "${DN}" in
                    ''|*[!0-9]*) printf '  Invalid.\n'; sleep 1; continue ;;
                esac
                sed -i "${DN}d" "${HOSTS_FILE}" 2>/dev/null || true
                printf '  Deleted line %s.\n' "${DN}"
                sleep 1
            ;;
            b|B|'') return ;;
        esac
    done
}

# ---------------------------------------------------------------------------
# Main loop
# ---------------------------------------------------------------------------
while true; do
    _header
    echo "    1)  Connect to server"
    echo "    2)  Manage saved hosts"
    echo "    3)  Radar  (Tailscale machines)"
    echo "    4)  Update PocketCli"
    echo "    5)  Exit"
    echo ""
    printf "  Choice [1-5]: "
    read -r CHOICE < /dev/tty

    case "${CHOICE}" in

        1)
            HOST=$(_pick_host) || { printf '\n  Cancelled.\n'; sleep 1; continue; }
            [ -z "${HOST}" ] && { printf '\n  No host selected.\n'; sleep 1; continue; }
            printf '\n  Connecting to %s...\n\n' "${HOST}"
            ssh "${HOST}" || printf '\n  Connection failed.\n'
            printf '\n  Press Enter...'
            read -r _D < /dev/tty
        ;;

        2)
            _manage_hosts
        ;;

        3)
            if ! command -v tailscale >/dev/null 2>&1; then
                printf '\n  Tailscale not installed. Run: pocket tailscale-setup\n'
            elif ! is_tailscale_daemon_running 2>/dev/null; then
                printf '\n  tailscaled not running. Run: pocket tailscale-start\n'
            else
                sh "${HOME}/.pocketcli/radar.sh" 2>/dev/null \
                    || printf '\n  Radar unavailable.\n'
            fi
            printf '\n  Press Enter...'
            read -r _D < /dev/tty
        ;;

        4)
            printf '\n  Updating PocketCli...\n\n'
            git -C "${HOME}/.pocketcli" pull --ff-only \
                && printf '\n  Done.\n' \
                || printf '\n  Update failed.\n'
            printf '  Press Enter...'
            read -r _D < /dev/tty
        ;;

        5|q|Q)
            printf '\n  Bye!\n\n'; exit 0
        ;;

        *)
            printf '\n  Invalid: %s\n' "${CHOICE}"
            sleep 1
        ;;

    esac
done
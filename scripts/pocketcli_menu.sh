#!/usr/bin/env bash
# =============================================================================
# PocketCli — scripts/pocketcli_menu.sh
# Interactive menu: connect to machines via fzf + Tailscale.
# =============================================================================

set -euo pipefail

_header() {
    clear
    echo ""
    echo "  ╔══════════════════════════════════════╗"
    echo "  ║          P o c k e t C l i           ║"
    echo "  ╚══════════════════════════════════════╝"
    echo ""
}

# ---------------------------------------------------------------------------
# Require fzf
# ---------------------------------------------------------------------------
command -v fzf >/dev/null 2>&1 || {
    printf '[PocketCli] fzf is required for the menu. Install it first.\n' >&2
    exit 1
}

# ---------------------------------------------------------------------------
# Main menu
# ---------------------------------------------------------------------------
while true; do
    _header
    CHOICE=$(printf '  1  Connect to server\n  2  Radar (machine list)\n  3  Update PocketCli\n  4  Exit' \
        | fzf --ansi --no-info --height=40% --prompt="  » " \
              --header="  Use ↑↓ or j/k  |  Enter to select" \
        | awk '{print $1}')

    case "${CHOICE}" in

        1)
            # List Tailscale peers and let the user pick one
            if ! command -v tailscale >/dev/null 2>&1; then
                printf '\n  [!] Tailscale not installed.\n'; sleep 2; continue
            fi

            PEERS=$(tailscale status --json 2>/dev/null \
                | jq -r '.Peer | to_entries[] | .value | select(.Online) | .HostName' \
                | sort)

            [ -z "${PEERS}" ] && {
                printf '\n  [!] No online machines found on Tailscale.\n'; sleep 2; continue
            }

            SELECTED=$(echo "${PEERS}" | fzf --prompt="  Connect to » " --height=50%)
            [ -z "${SELECTED}" ] && continue

            # Sanitise host name before passing to ssh
            SAFE_HOST="${SELECTED//[^a-zA-Z0-9._-]/}"
            [ -z "${SAFE_HOST}" ] && { printf '\n  [!] Invalid hostname.\n'; sleep 2; continue; }

            printf '\n  Connecting to %s...\n\n' "${SAFE_HOST}"
            ssh "${SAFE_HOST}" || printf '\n  [!] Connection failed.\n'
            sleep 1
        ;;

        2)
            "${HOME}/.pocketcli/radar.sh" 2>/dev/null || printf '\n  [!] Radar unavailable.\n'
            printf '\n  Press Enter to return...'
            read -r _
        ;;

        3)
            printf '\n  Updating PocketCli...\n\n'
            git -C "${HOME}/.pocketcli" pull --ff-only
            printf '\n  Done. Press Enter...'
            read -r _
        ;;

        4|'')
            printf '\n  Bye!\n\n'
            exit 0
        ;;

    esac
done

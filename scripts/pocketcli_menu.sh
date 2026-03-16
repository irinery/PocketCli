#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/pocketcli_menu.sh
# Interactive menu via fzf.
# =============================================================================

set -eu

command -v fzf >/dev/null 2>&1 || {
    printf '[PocketCli] fzf is required for the menu.\n' >&2; exit 1
}

_header() {
    clear
    echo ""
    echo "  =================================="
    echo "        P o c k e t C l i"
    echo "  =================================="
    echo ""
}

while true; do
    _header
    CHOICE=$(printf '1  Connect to server\n2  Radar (machine list)\n3  Update PocketCli\n4  Exit' \
        | fzf --no-info --height=40% --prompt="  > " \
              --header="  Up/Down to move | Enter to select" \
              < /dev/tty \
        | cut -d' ' -f1)

    case "${CHOICE}" in

        1)
            command -v tailscale >/dev/null 2>&1 || {
                printf '\n  Tailscale not installed.\n'; sleep 2; continue
            }
            PEERS=$(tailscale status --json 2>/dev/null \
                | jq -r '.Peer | to_entries[] | .value | select(.Online) | .HostName' \
                | sort)
            [ -z "${PEERS}" ] && {
                printf '\n  No online machines on Tailscale.\n'; sleep 2; continue
            }
            SELECTED=$(printf '%s\n' "${PEERS}" \
                | fzf --prompt="  Connect to > " --height=50% < /dev/tty)
            [ -z "${SELECTED}" ] && continue
            SAFE=$(printf '%s' "${SELECTED}" | tr -cd 'a-zA-Z0-9._-')
            [ -z "${SAFE}" ] && { printf '\n  Invalid hostname.\n'; sleep 2; continue; }
            printf '\n  Connecting to %s...\n\n' "${SAFE}"
            ssh "${SAFE}" || printf '\n  Connection failed.\n'
            sleep 1
        ;;

        2)
            sh "${HOME}/.pocketcli/radar.sh" 2>/dev/null || printf '\n  Radar unavailable.\n'
            printf '\n  Press Enter...'
            read -r _DUMMY
        ;;

        3)
            printf '\n  Updating PocketCli...\n\n'
            git -C "${HOME}/.pocketcli" pull --ff-only
            printf '\n  Done. Press Enter...'
            read -r _DUMMY
        ;;

        4|'')
            printf '\n  Bye!\n\n'; exit 0
        ;;

    esac
done

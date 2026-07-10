#!/usr/bin/env sh

POCKETCLI_DIR="${HOME}/.pocketcli"
if [ ! -r "${POCKETCLI_DIR}/lib/common.sh" ]; then
    printf 'offline\n'
    exit 0
fi

. "${POCKETCLI_DIR}/lib/common.sh"

TS_IP=$(get_tailscale_ip 2>/dev/null || true)
printf '%s\n' "${TS_IP:-offline}"

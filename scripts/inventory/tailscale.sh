#!/usr/bin/env sh

inventory_tailscale_hosts() {
    if [ "${HAS_TAILSCALE}" != "true" ] || [ "${HAS_JQ}" != "true" ]; then
        return 0
    fi

    TS_STATUS=$(with_timeout 8 tailscale status --json 2>/dev/null || true)
    [ -n "${TS_STATUS}" ] || return 0

    printf '%s\n' "${TS_STATUS}" | jq -r '.Peer | to_entries[] | .value | [.HostName, (.TailscaleIPs[0] // ""), (if .Online then "true" else "false" end), (.OS // "")] | @tsv' 2>/dev/null
}

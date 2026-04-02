#!/usr/bin/env sh

inventory_seed_hosts() {
    FILE="$(pocket_seed_hosts_file)"
    [ -f "${FILE}" ] || return 0
    grep -v '^[[:space:]]*#' "${FILE}" 2>/dev/null | grep -v '^[[:space:]]*$' | while IFS= read -r H; do
        H=$(safe_host "${H}")
        [ -n "${H}" ] && printf '%s\n' "${H}"
    done
}

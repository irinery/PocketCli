#!/usr/bin/env sh

inventory_find_host_json() {
    HOST_QUERY=$(safe_host "${1:-}")
    [ -n "${HOST_QUERY}" ] || return 1
    FILE="$(pocket_inventory_file)"
    [ -s "${FILE}" ] || return 1

    if command -v jq >/dev/null 2>&1; then
        jq -c --arg host "${HOST_QUERY}" '.hosts[] | select(.hostname == $host or .id == $host)' "${FILE}" 2>/dev/null | head -1
        return 0
    fi

    return 1
}

inventory_host_is_approved() {
    HOST_QUERY="$1"
    JSON=$(inventory_find_host_json "${HOST_QUERY}" || true)
    [ -n "${JSON}" ] || return 1
    printf '%s' "${JSON}" | grep -q '"approved": true'
}

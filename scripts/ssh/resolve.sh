#!/usr/bin/env sh

. "${POCKETCLI_DIR}/scripts/inventory/selectors.sh"

ssh_resolve_target() {
    INPUT=$(safe_host "${1:-}")
    [ -n "${INPUT}" ] || return 1

    if is_ip_target "${INPUT}"; then
        TARGET_HOST="${INPUT}"
        TARGET_IP="${INPUT}"
        TARGET_SOURCE="direct"
        TARGET_APPROVED=false
        return 0
    fi

    JSON=$(inventory_find_host_json "${INPUT}" || true)
    if [ -n "${JSON}" ] && command -v jq >/dev/null 2>&1; then
        TARGET_HOST=$(printf '%s' "${JSON}" | jq -r '.hostname // empty')
        TARGET_IP=$(printf '%s' "${JSON}" | jq -r '.tailscale_ip // empty')
        TARGET_SOURCE=$(printf '%s' "${JSON}" | jq -r '(.source // ["unknown"]) | join(",")')
        TARGET_APPROVED=$(printf '%s' "${JSON}" | jq -r '.approved // false')
    else
        TARGET_HOST="${INPUT}"
        TARGET_IP=""
        TARGET_SOURCE="unknown"
        TARGET_APPROVED=false
    fi

    [ -n "${TARGET_HOST}" ] || TARGET_HOST="${INPUT}"
}

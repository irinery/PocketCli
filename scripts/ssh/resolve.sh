#!/usr/bin/env sh

. "${POCKETCLI_DIR}/scripts/inventory/selectors.sh"

ssh_resolve_target() {
    RAW_INPUT=${1:-}
    INPUT=$(safe_host "${RAW_INPUT}")
    [ -n "${INPUT}" ] && [ "${INPUT}" = "${RAW_INPUT}" ] || return 1

    if is_ip_target "${INPUT}"; then
        TARGET_HOST="${INPUT}"
        # shellcheck disable=SC2034
        TARGET_IP="${INPUT}"
        # shellcheck disable=SC2034
        TARGET_SOURCE="direct"
        # shellcheck disable=SC2034
        TARGET_APPROVED=false
        return 0
    fi

    JSON=$(inventory_find_host_json "${INPUT}" || true)
    if [ -n "${JSON}" ] && command -v jq >/dev/null 2>&1; then
        TARGET_HOST=$(printf '%s' "${JSON}" | jq -r '.hostname // empty')
        # shellcheck disable=SC2034
        TARGET_IP=$(printf '%s' "${JSON}" | jq -r '.tailscale_ip // empty')
        # shellcheck disable=SC2034
        TARGET_SOURCE=$(printf '%s' "${JSON}" | jq -r '(.source // ["unknown"]) | join(",")')
        # shellcheck disable=SC2034
        TARGET_APPROVED=$(printf '%s' "${JSON}" | jq -r '.approved // false')
    else
        TARGET_HOST="${INPUT}"
        # shellcheck disable=SC2034
        TARGET_IP=""
        # shellcheck disable=SC2034
        TARGET_SOURCE="unknown"
        # shellcheck disable=SC2034
        TARGET_APPROVED=false
    fi

    [ -n "${TARGET_HOST}" ] || TARGET_HOST="${INPUT}"
    SAFE_TARGET=$(safe_host "${TARGET_HOST}")
    [ -n "${SAFE_TARGET}" ] && [ "${SAFE_TARGET}" = "${TARGET_HOST}" ] || return 1
}

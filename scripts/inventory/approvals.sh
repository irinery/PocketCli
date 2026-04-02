#!/usr/bin/env sh

. "${POCKETCLI_DIR}/scripts/inventory/selectors.sh"

approval_summary_line() {
    LABEL="$1"
    VALUE="$2"
    printf '  %-18s %s\n' "${LABEL}:" "${VALUE}"
}

approval_prompt_for_host() {
    HOST_QUERY=$(safe_host "${1:-}")
    ACTION="${2:-interactive}"
    LAYOUT_ID="${3:-viewer-default}"

    JSON=$(inventory_find_host_json "${HOST_QUERY}" || true)

    HOSTNAME="${HOST_QUERY}"
    IP=""
    SOURCE="unknown"
    ONLINE="unknown"
    REACHABLE="unknown"
    TRUST="unknown"

    if [ -n "${JSON}" ] && command -v jq >/dev/null 2>&1; then
        HOSTNAME=$(printf '%s' "${JSON}" | jq -r '.hostname // empty')
        IP=$(printf '%s' "${JSON}" | jq -r '.tailscale_ip // empty')
        SOURCE=$(printf '%s' "${JSON}" | jq -r '(.source // []) | join(",")')
        ONLINE=$(printf '%s' "${JSON}" | jq -r '.online // false')
        REACHABLE=$(printf '%s' "${JSON}" | jq -r '.reachable // false')
        TRUST=$(printf '%s' "${JSON}" | jq -r '.trust_level // "unknown"')
    fi

    echo ""
    echo "Session approval required"
    approval_summary_line "Hostname" "${HOSTNAME}"
    approval_summary_line "IP Tailscale" "${IP:-n/a}"
    approval_summary_line "Source" "${SOURCE}"
    approval_summary_line "Online" "${ONLINE}"
    approval_summary_line "Reachable" "${REACHABLE}"
    approval_summary_line "Trust level" "${TRUST}"
    approval_summary_line "Layout" "${LAYOUT_ID}"
    approval_summary_line "Action" "${ACTION}"
    echo ""

    confirm "Approve this SSH session?"
}

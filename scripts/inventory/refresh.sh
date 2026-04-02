#!/usr/bin/env sh

. "${POCKETCLI_DIR}/scripts/inventory/tailscale.sh"
. "${POCKETCLI_DIR}/scripts/inventory/saved_hosts.sh"
. "${POCKETCLI_DIR}/scripts/inventory/seeds.sh"
. "${POCKETCLI_DIR}/scripts/inventory/reconcile.sh"

inventory_refresh() {
    TMP_FILE=$(mktemp)

    inventory_tailscale_hosts | while IFS=$'\t' read -r HOST IP ONLINE _OS; do
        HOST=$(safe_host "${HOST}")
        [ -n "${HOST}" ] && printf '%s\t%s\t%s\ttailscale\n' "${HOST}" "${IP}" "${ONLINE:-false}"
    done >> "${TMP_FILE}"

    inventory_saved_hosts | while IFS= read -r HOST; do
        printf '%s\t\tfalse\tsaved\n' "${HOST}"
    done >> "${TMP_FILE}"

    inventory_seed_hosts | while IFS= read -r HOST; do
        printf '%s\t\tfalse\tseed\n' "${HOST}"
    done >> "${TMP_FILE}"

    if [ ! -s "${TMP_FILE}" ]; then
        NOW=$(date -u '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date '+%Y-%m-%dT%H:%M:%SZ')
        cat > "$(pocket_inventory_file)" <<JSON
{"schema_version":1,"generated_at":"${NOW}","sources":{"tailscale":${HAS_TAILSCALE},"saved_hosts":true,"fallback_seeds":true},"hosts":[]}
JSON
        INVENTORY_LAST_REFRESH_AT="${NOW}"
        INVENTORY_REFRESH_STATUS="empty"
        INVENTORY_KNOWN_COUNT=0
        INVENTORY_ONLINE_COUNT=0
        rm -f "${TMP_FILE}"
        return 0
    fi

    inventory_reconcile_to_json "${TMP_FILE}"
    rm -f "${TMP_FILE}"
}

#!/usr/bin/env sh

inventory_reconcile_to_json() {
    TMP_FILE="$1"
    NOW="$(date -u '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date '+%Y-%m-%dT%H:%M:%SZ')"

    KNOWN_COUNT=$(awk -F'\t' 'NF>0{print $1}' "${TMP_FILE}" | awk '!seen[$0]++{c++} END{print c+0}')
    ONLINE_COUNT=$(awk -F'\t' '$3=="true"{print $1}' "${TMP_FILE}" | awk '!seen[$0]++{c++} END{print c+0}')

    {
        echo '{'
        echo '  "schema_version": 1,'
        printf '  "generated_at": "%s",\n' "${NOW}"
        printf '  "sources": {"tailscale": %s, "saved_hosts": true, "fallback_seeds": true},\n' "${HAS_TAILSCALE}"
        echo '  "hosts": ['

        FIRST=1
        awk -F'\t' 'NF>0{if(!seen[$1]++){print $0}}' "${TMP_FILE}" | while IFS=$'\t' read -r HOST IP ONLINE SOURCE; do
            [ -n "${HOST}" ] || continue
            if [ "${FIRST}" -eq 0 ]; then
                echo '    ,'
            fi
            FIRST=0
            TRUST="seed"
            [ "${SOURCE}" = "saved" ] && TRUST="known"
            [ "${SOURCE}" = "tailscale" ] && TRUST="observed"
            cat <<JSON
    {
      "id": "host-${HOST}",
      "hostname": "${HOST}",
      "tailscale_ip": "${IP}",
      "source": ["${SOURCE}"],
      "online": ${ONLINE:-false},
      "reachable": ${ONLINE:-false},
      "approved": false,
      "trust_level": "${TRUST}",
      "last_seen_at": "${NOW}",
      "last_approved_at": "",
      "preferred_transport": "ssh",
      "labels": [],
      "meta": {"origin": "${SOURCE}"}
    }
JSON
        done
        echo ''
        echo '  ]'
        echo '}'
    } > "$(pocket_inventory_file)"

    # shellcheck disable=SC2034
    INVENTORY_LAST_REFRESH_AT="${NOW}"
    # shellcheck disable=SC2034
    INVENTORY_REFRESH_STATUS="ok"
    # shellcheck disable=SC2034
    INVENTORY_KNOWN_COUNT="${KNOWN_COUNT}"
    # shellcheck disable=SC2034
    INVENTORY_ONLINE_COUNT="${ONLINE_COUNT}"
}

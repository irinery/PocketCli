#!/usr/bin/env sh

. "${POCKETCLI_DIR}/scripts/runtime/paths.sh"

ssh_policy_ensure_file() {
    FILE="$(pocket_ssh_policy_file)"
    [ -s "${FILE}" ] && return 0
    mkdir -p "$(dirname "${FILE}")"
    cat > "${FILE}" <<JSON
{
  "schema_version": 1,
  "host_key_policy": "accept-new",
  "connect_timeout_sec": 10,
  "strict_mode": false,
  "require_approval_for_unknown": true,
  "auto_approve_saved_hosts": true,
  "auto_approve_tailscale_known_hosts": true,
  "log_all_connections": true
}
JSON
}

ssh_policy_get() {
    KEY="$1"
    FILE="$(pocket_ssh_policy_file)"
    ssh_policy_ensure_file

    if command -v jq >/dev/null 2>&1; then
        jq -r ".${KEY}" "${FILE}" 2>/dev/null
        return 0
    fi

    case "${KEY}" in
        connect_timeout_sec) printf '10\n' ;;
        host_key_policy) printf 'accept-new\n' ;;
        strict_mode) printf 'false\n' ;;
        require_approval_for_unknown) printf 'true\n' ;;
        auto_approve_saved_hosts|auto_approve_tailscale_known_hosts|log_all_connections) printf 'true\n' ;;
        *) printf '\n' ;;
    esac
}

ssh_policy_flags() {
    HK=$(ssh_policy_get host_key_policy)
    CT=$(ssh_policy_get connect_timeout_sec)
    printf '%s' "-o StrictHostKeyChecking=${HK} -o ConnectTimeout=${CT}"
}

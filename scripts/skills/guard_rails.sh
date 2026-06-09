#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/skills/lib.sh
. "${SCRIPT_DIR}/lib.sh"

readonly MAX_CONCURRENT_EXECUTE_PER_HOST=1

guard_emit() {
    local decision reason request_id lock_path timestamp
    decision=$1
    reason=$2
    request_id=$3
    lock_path=${4:-}
    timestamp=$(pocket_skill_timestamp)

    python3 - "$decision" "$reason" "$request_id" "$timestamp" "$lock_path" <<'PY'
import json
import sys

decision, reason, request_id, timestamp, lock_path = sys.argv[1:6]
payload = {
    "decision": decision,
    "reason": None if reason == "" else reason,
    "request_id": None if request_id == "" else request_id,
    "timestamp": timestamp,
}
if lock_path:
    payload["lock_path"] = lock_path
print(json.dumps(payload, ensure_ascii=False, separators=(",", ":")))
PY
}

guard_request_fields() {
    local request_file
    request_file=$1
    python3 - "$request_file" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    data = json.load(handle)

for key in ["request_id", "skill_name", "host", "risk_level", "source", "confirmation_token"]:
    value = data.get(key)
    print("" if value is None else str(value))
PY
}

guard_skill_allowed() {
    local allowed_file skill_name line candidate
    allowed_file=$1
    skill_name=$2

    while IFS= read -r line || [[ -n "$line" ]]; do
        candidate=${line%%#*}
        candidate=${candidate//[[:space:]]/}
        [[ -z "$candidate" ]] && continue
        [[ "$candidate" == "$skill_name" ]] && return 0
    done < "$allowed_file"

    return 1
}

guard_host_in_inventory() {
    local inventory_file host line first
    inventory_file=$1
    host=$2

    while IFS= read -r line || [[ -n "$line" ]]; do
        line=${line%%#*}
        line=${line//$'\t'/ }
        line=$(printf '%s' "$line" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        [[ -z "$line" ]] && continue
        [[ "$line" == \[*\] ]] && continue
        first=${line%% *}
        [[ "$first" == "$host" ]] && return 0
    done < "$inventory_file"

    return 1
}

guard_params_reason() {
    local request_file
    request_file=$1
    python3 - "$request_file" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    data = json.load(handle)

params = data.get("params") or {}
path_markers = ["../", "./", "/etc", "/proc", "/sys", "/dev", "~"]
injection_markers = ["$(", "`", "; ", "&&", "||", ">", "<", "|"]
safe_absolute_prefixes = ("/var/log", "/tmp", "/var/cache")

for key, value in params.items():
    text = str(value)
    if any(marker in text for marker in injection_markers):
        print("injection_attempt:params")
        raise SystemExit(0)
    if text.startswith("/") and not text.startswith(safe_absolute_prefixes):
        print(f"path_traversal_attempt:{key}")
        raise SystemExit(0)
    if any(marker in text for marker in path_markers):
        print(f"path_traversal_attempt:{key}")
        raise SystemExit(0)

print("")
PY
}

guard_acquire_execute_lock() {
    local host request_id lock_dir lock_path current_count
    host=$1
    request_id=$2
    lock_dir=$(pocket_skill_lock_dir)
    mkdir -p "$lock_dir"

    current_count=$(find "$lock_dir" -maxdepth 1 -type d -name "${host}.*" 2>/dev/null | wc -l | tr -d ' ')
    if [[ "$current_count" -ge "$MAX_CONCURRENT_EXECUTE_PER_HOST" ]]; then
        printf 'concurrent_execute_limit:host\n'
        return 1
    fi

    lock_path="${lock_dir}/${host}.${request_id}.lock"
    if mkdir "$lock_path" 2>/dev/null; then
        printf '%s\n' "$lock_path"
        return 0
    fi

    printf 'concurrent_execute_limit:host\n'
    return 1
}

guard_release_lock() {
    local lock_path lock_dir
    lock_path=$1
    lock_dir=$(pocket_skill_lock_dir)
    [[ -z "$lock_path" ]] && return 0
    case "$lock_path" in
        "$lock_dir"/*) rm -rf -- "$lock_path" ;;
    esac
}

guard_rails_validate() {
    local request_file allowed_file inventory_file request_id skill_name host risk_level source confirmation_token
    local params_reason lock_result lock_path
    request_file=$1
    allowed_file=$(pocket_skill_allowed_file)
    inventory_file=$(pocket_skill_inventory_file)

    pocket_skill_require_python >/dev/null

    if [[ ! -r "$allowed_file" ]]; then
        printf 'whitelist_unavailable\n' >&2
        guard_emit "DENY" "whitelist_unavailable" ""
        return 2
    fi
    if [[ ! -r "$inventory_file" ]]; then
        printf 'inventory_unavailable\n' >&2
        guard_emit "DENY" "inventory_unavailable" ""
        return 2
    fi

    {
        IFS= read -r request_id || request_id=""
        IFS= read -r skill_name || skill_name=""
        IFS= read -r host || host=""
        IFS= read -r risk_level || risk_level=""
        IFS= read -r source || source=""
        IFS= read -r confirmation_token || confirmation_token=""
    } < <(guard_request_fields "$request_file")
    : "$source"

    if [[ "$risk_level" == "destructive" ]]; then
        guard_emit "DENY" "destructive_blocked_hardcoded" "$request_id"
        return 1
    fi
    if ! guard_skill_allowed "$allowed_file" "$skill_name"; then
        guard_emit "DENY" "skill_not_whitelisted" "$request_id"
        return 1
    fi
    if ! guard_host_in_inventory "$inventory_file" "$host"; then
        guard_emit "DENY" "host_not_in_inventory" "$request_id"
        return 1
    fi

    params_reason=$(guard_params_reason "$request_file")
    if [[ -n "$params_reason" ]]; then
        guard_emit "DENY" "$params_reason" "$request_id"
        return 1
    fi

    if [[ "$risk_level" == "execute" && -z "$confirmation_token" ]]; then
        guard_emit "DENY" "execute_requires_confirmation_token" "$request_id"
        return 1
    fi

    lock_path=""
    if [[ "$risk_level" == "execute" ]]; then
        if ! lock_result=$(guard_acquire_execute_lock "$host" "$request_id"); then
            guard_emit "DENY" "$lock_result" "$request_id"
            return 1
        fi
        lock_path=$lock_result
    fi

    guard_emit "ALLOW" "" "$request_id" "$lock_path"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    case "${1:-}" in
        release)
            guard_release_lock "${2:-}"
            ;;
        *)
            if [[ $# -ne 1 ]]; then
                guard_emit "DENY" "usage:guard_rails.sh <skill_request_json_file>" ""
                exit 1
            fi
            guard_rails_validate "$1"
            ;;
    esac
fi

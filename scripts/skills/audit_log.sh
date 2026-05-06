#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/skills/lib.sh
. "${SCRIPT_DIR}/lib.sh"

audit_max_size() {
    printf '%s\n' "${POCKETCLI_SKILL_LOG_MAX_BYTES:-10485760}"
}

audit_write() {
    local entry_file log_file log_dir tmp_entry current_size rotation_suffix
    entry_file=$1
    log_file=$(pocket_skill_log_file)
    log_dir=$(dirname -- "$log_file")

    pocket_skill_require_python >/dev/null || return 2

    if ! mkdir -p "$log_dir"; then
        printf 'AUDIT_LOG_WRITE_FAILED: mkdir_failed\n' >&2
        return 2
    fi
    chmod 700 "$log_dir" 2>/dev/null || true

    if [[ -f "$log_file" ]]; then
        current_size=$(wc -c < "$log_file" | tr -d ' ')
        if [[ "$current_size" -ge "$(audit_max_size)" ]]; then
            rotation_suffix=$(date -u +%Y%m%d%H%M%S)
            if ! mv "$log_file" "${log_file}.${rotation_suffix}"; then
                printf 'AUDIT_LOG_WRITE_FAILED: rotate_failed\n' >&2
                return 2
            fi
        fi
    fi

    tmp_entry=$(mktemp "${log_dir}/.entry_XXXXXXXX")
    if ! python3 - "$entry_file" > "$tmp_entry" <<'PY'
import json
import sys
from datetime import datetime, timezone

path = sys.argv[1]
with open(path, "r", encoding="utf-8") as handle:
    raw = json.load(handle)

if "request" in raw and "response" in raw:
    request = raw.get("request") if isinstance(raw.get("request"), dict) else {}
    response = raw.get("response") if isinstance(raw.get("response"), dict) else {}
    entry = {
        "request_id": request.get("request_id") or response.get("request_id"),
        "timestamp": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "source": request.get("source"),
        "skill_name": request.get("skill_name") or response.get("skill_name"),
        "host": request.get("host") or response.get("host"),
        "risk_level": request.get("risk_level"),
        "status": response.get("status"),
        "changed": response.get("changed"),
        "duration_ms": response.get("duration_ms"),
        "error": response.get("error"),
        "block_reason": response.get("error") if response.get("status") == "blocked" else None,
        "ansible_exit_code": response.get("exit_code"),
    }
else:
    entry = {
        "request_id": raw.get("request_id"),
        "timestamp": raw.get("timestamp") or datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "source": raw.get("source"),
        "skill_name": raw.get("skill_name"),
        "host": raw.get("host"),
        "risk_level": raw.get("risk_level"),
        "status": raw.get("status"),
        "changed": raw.get("changed"),
        "duration_ms": raw.get("duration_ms"),
        "error": raw.get("error"),
        "block_reason": raw.get("block_reason"),
        "ansible_exit_code": raw.get("ansible_exit_code"),
    }

print(json.dumps(entry, ensure_ascii=False, separators=(",", ":")))
PY
    then
        rm -f "$tmp_entry"
        printf 'AUDIT_LOG_WRITE_FAILED: invalid_entry\n' >&2
        return 2
    fi

    if ! cat "$tmp_entry" >> "$log_file"; then
        rm -f "$tmp_entry"
        printf 'AUDIT_LOG_WRITE_FAILED: append_failed\n' >&2
        return 2
    fi
    rm -f "$tmp_entry"
    chmod 600 "$log_file" 2>/dev/null || true
}

audit_query() {
    local host skill status since last log_file
    host=""
    skill=""
    status=""
    since=""
    last="50"
    log_file=$(pocket_skill_log_file)

    while [[ $# -gt 0 ]]; do
        case "$1" in
            --host)
                host=${2:-}
                shift 2
                ;;
            --skill)
                skill=${2:-}
                shift 2
                ;;
            --status)
                status=${2:-}
                shift 2
                ;;
            --since)
                since=${2:-}
                shift 2
                ;;
            --last)
                last=${2:-50}
                shift 2
                ;;
            *)
                printf 'invalid_query_option:%s\n' "$1" >&2
                return 1
                ;;
        esac
    done

    [[ -f "$log_file" ]] || return 0
    pocket_skill_require_python >/dev/null || return 2

    python3 - "$log_file" "$host" "$skill" "$status" "$since" "$last" <<'PY'
import json
import sys

path, host, skill, status, since, last_raw = sys.argv[1:7]
try:
    last = int(last_raw)
except ValueError:
    last = 50
last = max(1, min(last, 1000))
if since and len(since) == 10:
    since = since + "T00:00:00Z"

rows = []
with open(path, "r", encoding="utf-8") as handle:
    for line in handle:
        line = line.strip()
        if not line:
            continue
        try:
            entry = json.loads(line)
        except Exception:
            continue
        if host and entry.get("host") != host:
            continue
        if skill and entry.get("skill_name") != skill:
            continue
        if status and entry.get("status") != status:
            continue
        if since and (entry.get("timestamp") or "") < since:
            continue
        rows.append(entry)

for entry in rows[-last:]:
    print(json.dumps(entry, ensure_ascii=False, separators=(",", ":")))
PY
}

main() {
    case "${1:-}" in
        write)
            [[ $# -eq 2 ]] || {
                printf 'usage:audit_log.sh write <json_entry_file>\n' >&2
                return 1
            }
            audit_write "$2"
            ;;
        query)
            shift
            audit_query "$@"
            ;;
        *)
            printf 'usage:audit_log.sh write|query\n' >&2
            return 1
            ;;
    esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    main "$@"
fi

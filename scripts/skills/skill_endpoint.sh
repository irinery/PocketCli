#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/skills/lib.sh
. "${SCRIPT_DIR}/lib.sh"

readonly EXECUTION_TIMEOUT_SECONDS=60
POCKET_SKILL_CLEANUP_FILES=()
POCKET_SKILL_ENDPOINT_LOCK_PATH=""

endpoint_cleanup() {
    local file
    for file in "${POCKET_SKILL_CLEANUP_FILES[@]}"; do
        rm -f "$file"
    done
    if [[ -n "$POCKET_SKILL_ENDPOINT_LOCK_PATH" ]]; then
        "${SCRIPT_DIR}/guard_rails.sh" release "$POCKET_SKILL_ENDPOINT_LOCK_PATH" >/dev/null 2>&1 || true
    fi
}

response_json() {
    local request_file status error changed exit_code stdout duration_ms
    request_file=$1
    status=$2
    error=$3
    changed=$4
    exit_code=$5
    stdout=$6
    duration_ms=$7

    python3 - "$request_file" "$status" "$error" "$changed" "$exit_code" "$stdout" "$duration_ms" "$(pocket_skill_timestamp)" <<'PY'
import json
import sys

request_path, status, error, changed_raw, exit_code_raw, stdout, duration_ms_raw, timestamp = sys.argv[1:9]
try:
    with open(request_path, "r", encoding="utf-8") as handle:
        request = json.load(handle)
except Exception:
    request = {}

try:
    exit_code = int(exit_code_raw)
except ValueError:
    exit_code = 1
try:
    duration_ms = int(duration_ms_raw)
except ValueError:
    duration_ms = 0

stdout_truncated = False
if len(stdout) > 8192:
    stdout = stdout[:8173] + "\n[output truncated]"
    stdout_truncated = True

payload = {
    "request_id": request.get("request_id"),
    "status": status,
    "skill_name": request.get("skill_name"),
    "host": request.get("host"),
    "changed": changed_raw == "true",
    "exit_code": exit_code,
    "stdout": stdout,
    "stdout_truncated": stdout_truncated,
    "error": None if error == "" else error,
    "duration_ms": duration_ms,
    "timestamp": timestamp,
}
print(json.dumps(payload, ensure_ascii=False, separators=(",", ":")))
PY
}

write_audit_from_response() {
    local request_file response_file audit_input
    request_file=$1
    response_file=$2
    audit_input=$(mktemp)
    python3 - "$request_file" "$response_file" > "$audit_input" <<'PY'
import json
import sys

request_path, response_path = sys.argv[1:3]
try:
    with open(request_path, "r", encoding="utf-8") as handle:
        request = json.load(handle)
except Exception:
    request = {}

with open(response_path, "r", encoding="utf-8") as handle:
    response = json.load(handle)

print(json.dumps({"request": request, "response": response}, ensure_ascii=False, separators=(",", ":")))
PY

    if ! "${SCRIPT_DIR}/audit_log.sh" write "$audit_input"; then
        :
    fi
    rm -f "$audit_input"
}

emit_and_audit() {
    local request_file status error changed exit_code stdout duration_ms response_file
    request_file=$1
    status=$2
    error=$3
    changed=$4
    exit_code=$5
    stdout=$6
    duration_ms=$7
    response_file=$(mktemp)

    response_json "$request_file" "$status" "$error" "$changed" "$exit_code" "$stdout" "$duration_ms" > "$response_file"
    write_audit_from_response "$request_file" "$response_file"
    cat "$response_file"
    rm -f "$response_file"
}

extract_guard_field() {
    local guard_file field
    guard_file=$1
    field=$2
    pocket_skill_json_get "$guard_file" "$field"
}

params_to_file() {
    local request_file params_file
    request_file=$1
    params_file=$2
    python3 - "$request_file" > "$params_file" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    request = json.load(handle)
params = request.get("params") or {}
print(json.dumps(params, ensure_ascii=False, separators=(",", ":")))
PY
}

extract_pocket_result() {
    local stdout_file
    stdout_file=$1
    python3 - "$stdout_file" <<'PY'
import json
import re
import sys

text = open(sys.argv[1], "r", encoding="utf-8", errors="replace").read()
lines = text.splitlines()

for line in lines:
    if line.startswith("POCKET_RESULT:"):
        candidate = line.split(":", 1)[1].strip()
        try:
            json.loads(candidate)
        except Exception:
            continue
        print(candidate)
        raise SystemExit(0)

for line in lines:
    if '"msg"' not in line:
        continue
    match = re.search(r'"msg"\s*:\s*(".*?"|\{.*\})(?:,|\s*\})?$', line)
    if not match:
        match = re.search(r'"msg"\s*:\s*(".*?"|\{.*\})', line)
    if not match:
        continue
    raw = match.group(1)
    try:
        parsed = json.loads(raw)
        if isinstance(parsed, str):
            json.loads(parsed)
            print(parsed)
        elif isinstance(parsed, dict):
            print(json.dumps(parsed, ensure_ascii=False, separators=(",", ":")))
        raise SystemExit(0)
    except Exception:
        continue

raise SystemExit(1)
PY
}

result_changed() {
    local result_json
    result_json=$1
    python3 - "$result_json" <<'PY'
import json
import sys

try:
    data = json.loads(sys.argv[1])
except Exception:
    print("false")
    raise SystemExit(0)
print("true" if data.get("changed") is True else "false")
PY
}

run_ansible_with_timeout() {
    local stdout_file stderr_file
    stdout_file=$1
    stderr_file=$2
    shift 2

    if command -v timeout >/dev/null 2>&1; then
        timeout "${EXECUTION_TIMEOUT_SECONDS}" "$@" > "$stdout_file" 2> "$stderr_file"
        return $?
    fi

    python3 - "$EXECUTION_TIMEOUT_SECONDS" "$stdout_file" "$stderr_file" "$@" <<'PY'
import subprocess
import sys

timeout_seconds = int(sys.argv[1])
stdout_path = sys.argv[2]
stderr_path = sys.argv[3]
command = sys.argv[4:]

with open(stdout_path, "w", encoding="utf-8") as stdout, open(stderr_path, "w", encoding="utf-8") as stderr:
    proc = subprocess.Popen(command, stdout=stdout, stderr=stderr)
    try:
        raise SystemExit(proc.wait(timeout=timeout_seconds))
    except subprocess.TimeoutExpired:
        proc.kill()
        proc.wait()
        raise SystemExit(124)
PY
}

skill_playbook_name() {
    case "$1" in
        disk_check) printf 'disk_check.yml\n' ;;
        disk_cleanup_safe) printf 'disk_cleanup_safe.yml\n' ;;
        service_status) printf 'service_status.yml\n' ;;
        service_restart_safe) printf 'service_restart_safe.yml\n' ;;
        log_tail) printf 'log_tail.yml\n' ;;
        *) return 1 ;;
    esac
}

main() {
    local request_file temp_request start_seconds validation_file guard_file guard_reason lock_path
    local skill_name host playbook_name playbook_path params_file stdout_file stderr_file rc duration_ms
    local result_json changed response_error response_stdout
    temp_request=""
    POCKET_SKILL_CLEANUP_FILES=()
    POCKET_SKILL_ENDPOINT_LOCK_PATH=""

    if [[ $# -gt 1 ]]; then
        printf '{"request_id":null,"status":"validation_failed","skill_name":null,"host":null,"changed":false,"exit_code":1,"stdout":"","stdout_truncated":false,"error":"usage:skill_endpoint.sh [request_file]","duration_ms":0,"timestamp":"%s"}\n' "$(pocket_skill_timestamp)"
        exit 1
    fi

    if [[ $# -eq 1 ]]; then
        request_file=$1
    else
        temp_request=$(mktemp)
        cat > "$temp_request"
        request_file=$temp_request
        POCKET_SKILL_CLEANUP_FILES+=("$temp_request")
    fi

    trap 'endpoint_cleanup' EXIT

    start_seconds=$(date +%s)
    pocket_skill_require_python >/dev/null || {
        emit_and_audit "$request_file" "error" "missing_dependency:python3" "false" 1 "" 0
        exit 1
    }

    validation_file=$(mktemp)
    POCKET_SKILL_CLEANUP_FILES+=("$validation_file")
    if ! "${SCRIPT_DIR}/skill_request_schema.sh" "$request_file" > "$validation_file"; then
        response_error=$(pocket_skill_json_get "$validation_file" "error")
        if [[ "$response_error" == "blocked_risk_level:destructive" || "$response_error" == injection_attempt:* ]]; then
            emit_and_audit "$request_file" "blocked" "$response_error" "false" 1 "" 0
        else
            emit_and_audit "$request_file" "validation_failed" "$response_error" "false" 1 "" 0
        fi
        exit 1
    fi

    guard_file=$(mktemp)
    POCKET_SKILL_CLEANUP_FILES+=("$guard_file")
    if ! "${SCRIPT_DIR}/guard_rails.sh" "$request_file" > "$guard_file"; then
        guard_reason=$(extract_guard_field "$guard_file" "reason")
        emit_and_audit "$request_file" "blocked" "$guard_reason" "false" 1 "" 0
        exit 1
    fi
    lock_path=$(extract_guard_field "$guard_file" "lock_path")
    if [[ -n "$lock_path" ]]; then
        POCKET_SKILL_ENDPOINT_LOCK_PATH=$lock_path
    fi

    if ! command -v ansible-playbook >/dev/null 2>&1; then
        emit_and_audit "$request_file" "error" "missing_dependency:ansible-playbook" "false" 1 "" 0
        exit 1
    fi

    skill_name=$(pocket_skill_json_get "$request_file" "skill_name")
    host=$(pocket_skill_json_get "$request_file" "host")
    if ! playbook_name=$(skill_playbook_name "$skill_name"); then
        emit_and_audit "$request_file" "blocked" "skill_not_mapped" "false" 1 "" 0
        exit 1
    fi
    playbook_path="$(pocket_skill_playbook_dir)/${playbook_name}"
    if [[ ! -r "$playbook_path" ]]; then
        emit_and_audit "$request_file" "error" "playbook_not_found" "false" 1 "" 0
        exit 1
    fi

    params_file=$(mktemp)
    stdout_file=$(mktemp)
    stderr_file=$(mktemp)
    POCKET_SKILL_CLEANUP_FILES+=("$params_file" "$stdout_file" "$stderr_file")
    params_to_file "$request_file" "$params_file"

    set +e
    run_ansible_with_timeout "$stdout_file" "$stderr_file" \
        ansible-playbook \
        -i "$(pocket_skill_inventory_file)" \
        -l "$host" \
        --extra-vars "@${params_file}" \
        "$playbook_path"
    rc=$?
    set -e

    duration_ms=$(( ( $(date +%s) - start_seconds ) * 1000 ))
    response_stdout=$(cat "$stdout_file")

    if [[ "$rc" -eq 124 ]]; then
        emit_and_audit "$request_file" "timeout" "execution_timeout_60s" "false" 1 "$response_stdout" "$duration_ms"
        exit 1
    fi

    if [[ "$rc" -ne 0 ]]; then
        response_error="playbook_failed"
        if grep -F "UNREACHABLE" "$stdout_file" "$stderr_file" >/dev/null 2>&1; then
            response_error="host_unreachable"
        fi
        emit_and_audit "$request_file" "error" "$response_error" "false" "$rc" "$response_stdout" "$duration_ms"
        exit 1
    fi

    if ! result_json=$(extract_pocket_result "$stdout_file"); then
        emit_and_audit "$request_file" "error" "missing_pocket_result_task" "false" 1 "$response_stdout" "$duration_ms"
        exit 1
    fi

    changed=$(result_changed "$result_json")
    emit_and_audit "$request_file" "success" "" "$changed" 0 "$result_json" "$duration_ms"
}

main "$@"

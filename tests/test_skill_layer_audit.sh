#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
trap 'rm -rf "${WORKDIR}"' EXIT INT TERM

HOME_DIR="${WORKDIR}/home"
LOG_FILE="${WORKDIR}/logs/skills.jsonl"
mkdir -p "${HOME_DIR}"

fail() {
    printf 'FAIL %s\n' "$1" >&2
    exit 1
}

write_entry_file() {
    file=$1
    request_id=$2
    status=$3
    changed=$4
    error_value=$5
    timestamp=$6
    skill_name=${7:-disk_check}
    host=${8:-srv-prod-01}
    python3 - "$file" "$request_id" "$status" "$changed" "$error_value" "$timestamp" "$skill_name" "$host" <<'PY'
import json
import sys
path, request_id, status, changed_raw, error_value, timestamp, skill_name, host = sys.argv[1:9]
entry = {
    "request_id": request_id,
    "timestamp": timestamp,
    "source": "test",
    "skill_name": None if skill_name == "null" else skill_name,
    "host": None if host == "null" else host,
    "risk_level": "diagnose",
    "status": status,
    "changed": changed_raw == "true",
    "duration_ms": 12,
    "error": None if error_value == "null" else error_value,
    "block_reason": error_value if status == "blocked" else None,
    "ansible_exit_code": 0 if status == "success" else 1,
    "stdout": "sensitive",
    "params": {"secret_path": "/var/log/app.log"},
    "confirmation_token": "secret-token",
}
json.dump(entry, open(path, "w", encoding="utf-8"), separators=(",", ":"))
PY
}

audit_write() {
    entry=$1
    set +e
    env HOME="${HOME_DIR}" \
        POCKETCLI_SKILL_LOG_FILE="${LOG_FILE}" \
        bash "${REPO_ROOT}/scripts/skills/audit_log.sh" write "${entry}" > "${entry}.out" 2> "${entry}.err"
    rc=$?
    set -e
    return "${rc}"
}

audit_query() {
    out=$1
    shift
    env HOME="${HOME_DIR}" \
        POCKETCLI_SKILL_LOG_FILE="${LOG_FILE}" \
        bash "${REPO_ROOT}/scripts/skills/audit_log.sh" query "$@" > "${out}"
}

json_field_line() {
    file=$1
    field=$2
    python3 - "$file" "$field" <<'PY'
import json
import sys
line = open(sys.argv[1], encoding="utf-8").read().splitlines()[0]
value = json.loads(line).get(sys.argv[2])
if value is True:
    print("true")
elif value is False:
    print("false")
elif value is None:
    print("null")
else:
    print(value)
PY
}

entry="${WORKDIR}/entry.json"
write_entry_file "${entry}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f741" success true null "2026-05-05T12:00:00Z"
audit_write "${entry}" || fail T-41
[ "$(wc -l < "${LOG_FILE}" | tr -d ' ')" = "1" ] || fail T-41-line-count
[ "$(json_field_line "${LOG_FILE}" status)" = "success" ] || fail T-41-status
[ "$(json_field_line "${LOG_FILE}" changed)" = "true" ] || fail T-41-changed
if grep -F 'sensitive' "${LOG_FILE}" >/dev/null 2>&1 || grep -F 'secret-token' "${LOG_FILE}" >/dev/null 2>&1; then
    fail T-41-sensitive-fields
fi

write_entry_file "${entry}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f742" blocked false skill_not_whitelisted "2026-05-05T12:01:00Z"
audit_write "${entry}" || fail T-42
tail -n 1 "${LOG_FILE}" > "${WORKDIR}/last.jsonl"
[ "$(json_field_line "${WORKDIR}/last.jsonl" status)" = "blocked" ] || fail T-42-status
[ "$(json_field_line "${WORKDIR}/last.jsonl" block_reason)" = "skill_not_whitelisted" ] || fail T-42-reason

write_entry_file "${entry}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f743" validation_failed false invalid_json "2026-05-05T12:02:00Z" null null
audit_write "${entry}" || fail T-43
tail -n 1 "${LOG_FILE}" > "${WORKDIR}/last.jsonl"
[ "$(json_field_line "${WORKDIR}/last.jsonl" status)" = "validation_failed" ] || fail T-43-status
[ "$(json_field_line "${WORKDIR}/last.jsonl" skill_name)" = "null" ] || fail T-43-skill-null

printf 'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n' > "${LOG_FILE}"
write_entry_file "${entry}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f744" success false null "2026-05-05T12:03:00Z"
env HOME="${HOME_DIR}" \
    POCKETCLI_SKILL_LOG_FILE="${LOG_FILE}" \
    POCKETCLI_SKILL_LOG_MAX_BYTES=10 \
    bash "${REPO_ROOT}/scripts/skills/audit_log.sh" write "${entry}" >/dev/null 2>"${WORKDIR}/rotate.err" || fail T-44
ls "${LOG_FILE}".* >/dev/null 2>&1 || fail T-44-rotated
[ "$(wc -l < "${LOG_FILE}" | tr -d ' ')" = "1" ] || fail T-44-new-log

rm -rf "$(dirname "${LOG_FILE}")"
write_entry_file "${entry}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f745" success false null "2026-05-05T12:04:00Z"
audit_write "${entry}" || fail T-45
mode=$(python3 - "$(dirname "${LOG_FILE}")" <<'PY'
import os
import sys
print(oct(os.stat(sys.argv[1]).st_mode & 0o777))
PY
)
[ "${mode}" = "0o700" ] || fail T-45-mode

bad_log="${WORKDIR}/bad-log"
mkdir -p "${bad_log}"
set +e
env HOME="${HOME_DIR}" POCKETCLI_SKILL_LOG_FILE="${bad_log}" bash "${REPO_ROOT}/scripts/skills/audit_log.sh" write "${entry}" > "${WORKDIR}/bad.out" 2> "${WORKDIR}/bad.err"
rc=$?
set -e
[ "${rc}" -eq 2 ] || fail T-46-rc
grep -F 'AUDIT_LOG_WRITE_FAILED:' "${WORKDIR}/bad.err" >/dev/null 2>&1 || fail T-46-stderr

rm -f "${LOG_FILE}"
for n in 1 2 3; do
    write_entry_file "${entry}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f75${n}" success false null "2026-05-0${n}T12:00:00Z" disk_check srv-prod-01
    audit_write "${entry}" || fail "T-47-seed-${n}"
done
write_entry_file "${entry}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f759" success false null "2026-05-04T12:00:00Z" disk_check srv-other
audit_write "${entry}" || fail T-47-seed-other
audit_query "${WORKDIR}/query-host.out" --host srv-prod-01 --last 20
[ "$(wc -l < "${WORKDIR}/query-host.out" | tr -d ' ')" = "3" ] || fail T-47-count

write_entry_file "${entry}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f760" blocked false skill_not_whitelisted "2026-04-30T12:00:00Z" disk_check srv-prod-01
audit_write "${entry}" || fail T-48-seed-old
write_entry_file "${entry}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f761" blocked false skill_not_whitelisted "2026-05-02T12:00:00Z" disk_check srv-prod-01
audit_write "${entry}" || fail T-48-seed-new
audit_query "${WORKDIR}/query-blocked.out" --status blocked --since 2026-05-01
grep -F 'e6f761' "${WORKDIR}/query-blocked.out" >/dev/null 2>&1 || fail T-48-new
if grep -F 'e6f760' "${WORKDIR}/query-blocked.out" >/dev/null 2>&1; then
    fail T-48-old
fi

printf 'PASS skill audit log tests T-41..T-48\n'

#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
trap 'rm -rf "${WORKDIR}"' EXIT INT TERM

HOME_DIR="${WORKDIR}/home"
ANSIBLE_DIR="${WORKDIR}/ansible"
LOG_FILE="${WORKDIR}/skills.jsonl"
mkdir -p "${HOME_DIR}" "${ANSIBLE_DIR}" "$(dirname "${LOG_FILE}")"
cp "${REPO_ROOT}/ansible/allowed_skills.txt" "${ANSIBLE_DIR}/allowed_skills.txt"

fail() {
    printf 'FAIL %s\n' "$1" >&2
    exit 1
}

assert_json_field() {
    file=$1
    field=$2
    expected=$3
    actual=$(python3 - "$file" "$field" <<'PY'
import json
import sys
with open(sys.argv[1], "r", encoding="utf-8") as handle:
    data = json.load(handle)
value = data.get(sys.argv[2])
if value is True:
    print("true")
elif value is False:
    print("false")
elif value is None:
    print("null")
else:
    print(value)
PY
)
    [ "${actual}" = "${expected}" ] || fail "${field}: expected ${expected}, got ${actual}"
}

write_request() {
    file=$1
    request_id=$2
    skill_name=$3
    host=$4
    risk_level=$5
    params_json=${6:-'{}'}
    python3 - "$file" "$request_id" "$skill_name" "$host" "$risk_level" "$params_json" <<'PY'
import json
import sys
path, request_id, skill_name, host, risk_level, params_raw = sys.argv[1:7]
data = {
    "request_id": request_id,
    "skill_name": skill_name,
    "host": host,
    "risk_level": risk_level,
    "params": json.loads(params_raw),
    "source": "test",
    "timestamp": "2026-05-05T12:00:00Z",
}
with open(path, "w", encoding="utf-8") as handle:
    json.dump(data, handle, separators=(",", ":"))
PY
}

run_schema() {
    request=$1
    out=$2
    set +e
    env HOME="${HOME_DIR}" \
        POCKETCLI_ANSIBLE_DIR="${ANSIBLE_DIR}" \
        POCKETCLI_SKILL_LOG_FILE="${LOG_FILE}" \
        bash "${REPO_ROOT}/scripts/skills/skill_request_schema.sh" "${request}" > "${out}" 2> "${out}.err"
    rc=$?
    set -e
    return "${rc}"
}

VALID_ID="a1b2c3d4-e5f6-4789-a012-b3c4d5e6f700"

req="${WORKDIR}/valid.json"
out="${WORKDIR}/valid.out"
write_request "${req}" "${VALID_ID}" "disk_check" "srv-prod-01" "diagnose"
run_schema "${req}" "${out}" || fail T-01
assert_json_field "${out}" valid true

python3 - "${req}" <<'PY'
import json
import sys
path = sys.argv[1]
with open(path, "r", encoding="utf-8") as handle:
    data = json.load(handle)
del data["skill_name"]
with open(path, "w", encoding="utf-8") as handle:
    json.dump(data, handle)
PY
run_schema "${req}" "${out}" && fail T-02
assert_json_field "${out}" error missing_field:skill_name

write_request "${req}" "${VALID_ID}" "disk_check" "srv-prod-01" "diagnose"
python3 - "${req}" <<'PY'
import json
import sys
path = sys.argv[1]
data = json.load(open(path, encoding="utf-8"))
del data["host"]
json.dump(data, open(path, "w", encoding="utf-8"))
PY
run_schema "${req}" "${out}" && fail T-03
assert_json_field "${out}" error missing_field:host

write_request "${req}" "${VALID_ID}" "disk_check" "srv-prod-01" "weird"
run_schema "${req}" "${out}" && fail T-04
assert_json_field "${out}" error invalid_enum:risk_level

write_request "${req}" "${VALID_ID}" "disk-check" "srv-prod-01" "diagnose"
run_schema "${req}" "${out}" && fail T-05
assert_json_field "${out}" error invalid_format:skill_name

write_request "${req}" "${VALID_ID}" "disk_check" 'srv$prod' "diagnose"
run_schema "${req}" "${out}" && fail T-06
assert_json_field "${out}" error injection_attempt:host

long_params=$(python3 - <<'PY'
import json
print(json.dumps({"target_path": "x" * 4097}))
PY
)
write_request "${req}" "${VALID_ID}" "disk_check" "srv-prod-01" "diagnose" "${long_params}"
run_schema "${req}" "${out}" && fail T-07
assert_json_field "${out}" error field_too_long:params

printf '{ skill_name: broken\n' > "${req}"
run_schema "${req}" "${out}" && fail T-08
assert_json_field "${out}" error invalid_json

write_request "${req}" "${VALID_ID}" "disk_check" "srv-prod-01" "destructive"
run_schema "${req}" "${out}" && fail T-09
assert_json_field "${out}" error blocked_risk_level:destructive

printf '{"request_id":"%s","timestamp":"2026-05-05T12:00:01Z","source":"test","skill_name":"disk_check","host":"srv-prod-01","risk_level":"diagnose","status":"success","changed":false,"duration_ms":1,"error":null,"block_reason":null,"ansible_exit_code":0}\n' "${VALID_ID}" > "${LOG_FILE}"
write_request "${req}" "${VALID_ID}" "disk_check" "srv-prod-01" "diagnose"
run_schema "${req}" "${out}" && fail T-10
assert_json_field "${out}" error duplicate_request_id

printf 'PASS skill request schema tests T-01..T-10\n'

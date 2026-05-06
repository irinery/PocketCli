#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
trap 'rm -rf "${WORKDIR}"' EXIT INT TERM

HOME_DIR="${WORKDIR}/home"
ANSIBLE_DIR="${WORKDIR}/ansible"
LOCK_DIR="${WORKDIR}/locks"
mkdir -p "${HOME_DIR}" "${ANSIBLE_DIR}" "${LOCK_DIR}"
cp "${REPO_ROOT}/ansible/allowed_skills.txt" "${ANSIBLE_DIR}/allowed_skills.txt"
cat > "${ANSIBLE_DIR}/inventory.ini" <<'EOF'
[test]
srv-prod-01 ansible_host=127.0.0.1
srv-prod-02 ansible_host=127.0.0.2
EOF

fail() {
    printf 'FAIL %s\n' "$1" >&2
    exit 1
}

json_field() {
    file=$1
    field=$2
    python3 - "$file" "$field" <<'PY'
import json
import sys
data = json.load(open(sys.argv[1], encoding="utf-8"))
value = data.get(sys.argv[2])
if value is None:
    print("null")
else:
    print(value)
PY
}

assert_field() {
    file=$1
    field=$2
    expected=$3
    actual=$(json_field "$file" "$field")
    [ "${actual}" = "${expected}" ] || fail "${field}: expected ${expected}, got ${actual}"
}

write_request() {
    file=$1
    request_id=$2
    skill_name=$3
    host=$4
    risk_level=$5
    params_json=${6:-'{}'}
    WR_TOKEN=${7:-}
    python3 - "$file" "$request_id" "$skill_name" "$host" "$risk_level" "$params_json" "$WR_TOKEN" <<'PY'
import json
import sys
path, request_id, skill_name, host, risk_level, params_raw, token = sys.argv[1:8]
data = {
    "request_id": request_id,
    "skill_name": skill_name,
    "host": host,
    "risk_level": risk_level,
    "params": json.loads(params_raw),
    "source": "test",
    "timestamp": "2026-05-05T12:00:00Z",
}
if token:
    data["confirmation_token"] = token
json.dump(data, open(path, "w", encoding="utf-8"), separators=(",", ":"))
PY
}

run_guard() {
    request=$1
    out=$2
    set +e
    env HOME="${HOME_DIR}" \
        POCKETCLI_ANSIBLE_DIR="${ANSIBLE_DIR}" \
        POCKETCLI_SKILL_LOCK_DIR="${LOCK_DIR}" \
        bash "${REPO_ROOT}/scripts/skills/guard_rails.sh" "${request}" > "${out}" 2> "${out}.err"
    rc=$?
    set -e
    return "${rc}"
}

req="${WORKDIR}/request.json"
out="${WORKDIR}/guard.out"
token="abcdefghijklmnopqrstuvwxyz012345"

write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f701" "disk_check" "srv-prod-01" "read"
run_guard "${req}" "${out}" || fail T-11
assert_field "${out}" decision ALLOW

write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f702" "unknown_skill" "srv-prod-01" "read"
run_guard "${req}" "${out}" && fail T-12
assert_field "${out}" reason skill_not_whitelisted

write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f703" "disk_check" "srv-missing" "read"
run_guard "${req}" "${out}" && fail T-13
assert_field "${out}" reason host_not_in_inventory

write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f704" "disk_check" "srv-prod-01" "destructive"
run_guard "${req}" "${out}" && fail T-14
assert_field "${out}" reason destructive_blocked_hardcoded

write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f705" "disk_cleanup_safe" "srv-prod-01" "execute"
run_guard "${req}" "${out}" && fail T-15
assert_field "${out}" reason execute_requires_confirmation_token

write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f706" "disk_cleanup_safe" "srv-prod-01" "execute" '{"target_path":"/var/log"}' "${token}"
run_guard "${req}" "${out}" || fail T-16
assert_field "${out}" decision ALLOW
lock_path=$(json_field "${out}" lock_path)
bash "${REPO_ROOT}/scripts/skills/guard_rails.sh" release "${lock_path}" >/dev/null 2>&1 || true

write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f707" "disk_cleanup_safe" "srv-prod-01" "execute" '{"target_path":"../../../etc/passwd"}' "${token}"
run_guard "${req}" "${out}" && fail T-17
assert_field "${out}" reason path_traversal_attempt:target_path

write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f708" "disk_cleanup_safe" "srv-prod-01" "execute" '{"target_path":"safe$(date)"}' "${token}"
run_guard "${req}" "${out}" && fail T-18
assert_field "${out}" reason injection_attempt:params

mv "${ANSIBLE_DIR}/allowed_skills.txt" "${ANSIBLE_DIR}/allowed_skills.txt.bak"
write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f709" "disk_check" "srv-prod-01" "read"
run_guard "${req}" "${out}" && fail T-19
[ "$?" -eq 0 ] && :
assert_field "${out}" reason whitelist_unavailable
mv "${ANSIBLE_DIR}/allowed_skills.txt.bak" "${ANSIBLE_DIR}/allowed_skills.txt"

mv "${ANSIBLE_DIR}/inventory.ini" "${ANSIBLE_DIR}/inventory.ini.bak"
write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f710" "disk_check" "srv-prod-01" "read"
run_guard "${req}" "${out}" && fail T-20
assert_field "${out}" reason inventory_unavailable
mv "${ANSIBLE_DIR}/inventory.ini.bak" "${ANSIBLE_DIR}/inventory.ini"

write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f711" "service_status" "srv-prod-01" "diagnose"
run_guard "${req}" "${out}" || fail T-21
assert_field "${out}" decision ALLOW

write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f712" "service_restart_safe" "srv-prod-02" "execute" '{"service_name":"nginx"}' "${token}"
run_guard "${req}" "${out}" || fail T-22-first
for n in 13 14 15 16 17 18 19 20 21; do
    write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f7${n}" "service_restart_safe" "srv-prod-02" "execute" '{"service_name":"nginx"}' "${token}"
    run_guard "${req}" "${out}" && fail "T-22-${n}"
    assert_field "${out}" reason concurrent_execute_limit:host
done

printf 'PASS skill guard rails tests T-11..T-22\n'

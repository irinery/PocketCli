#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
trap 'rm -rf "${WORKDIR}"' EXIT INT TERM

HOME_DIR="${WORKDIR}/home"
ANSIBLE_DIR="${WORKDIR}/ansible"
LOCK_DIR="${WORKDIR}/locks"
LOG_FILE="${WORKDIR}/skills.jsonl"
MOCKBIN="${WORKDIR}/mockbin"
mkdir -p "${HOME_DIR}" "${ANSIBLE_DIR}" "${LOCK_DIR}" "${MOCKBIN}"
cp -R "${REPO_ROOT}/ansible/." "${ANSIBLE_DIR}/"
cat > "${ANSIBLE_DIR}/inventory.ini" <<'EOF'
[test]
srv-prod-01 ansible_host=127.0.0.1
srv-prod-02 ansible_host=127.0.0.2
EOF

cat > "${MOCKBIN}/timeout" <<'EOF'
#!/usr/bin/env sh
set -eu
if [ "${POCKETCLI_TEST_TIMEOUT:-0}" = "1" ]; then
    exit 124
fi
shift
exec "$@"
EOF
chmod +x "${MOCKBIN}/timeout"

cat > "${MOCKBIN}/ansible-playbook" <<'EOF'
#!/usr/bin/env sh
set -eu
case "${POCKETCLI_TEST_ANSIBLE_MODE:-success}" in
    success)
        printf 'TASK [pocket_result]\n'
        printf 'ok: [srv-prod-01] => {"msg": "{\\"mount_point\\":\\"/\\",\\"used_percent\\":42,\\"changed\\":false}"}\n'
        ;;
    changed)
        printf 'TASK [pocket_result]\n'
        printf 'ok: [srv-prod-01] => {"msg": "{\\"restarted\\":true,\\"state\\":\\"active\\",\\"changed\\":true}"}\n'
        ;;
    unreachable)
        printf 'UNREACHABLE! host failed\n'
        exit 2
        ;;
    missing_result)
        printf 'PLAY RECAP without result\n'
        ;;
    sleep_changed)
        sleep 2
        printf 'TASK [pocket_result]\n'
        printf 'ok: [srv-prod-02] => {"msg": "{\\"restarted\\":true,\\"state\\":\\"active\\",\\"changed\\":true}"}\n'
        ;;
esac
EOF
chmod +x "${MOCKBIN}/ansible-playbook"

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

assert_field() {
    file=$1
    field=$2
    expected=$3
    actual=$(json_field "$file" "$field")
    [ "${actual}" = "${expected}" ] || fail "${field}: expected ${expected}, got ${actual}"
}

assert_contains() {
    file=$1
    needle=$2
    grep -F "$needle" "$file" >/dev/null 2>&1 || fail "missing ${needle}"
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

run_endpoint() {
    request=$1
    out=$2
    mode=${3:-success}
    timeout_mode=${4:-0}
    set +e
    env HOME="${HOME_DIR}" \
        PATH="${MOCKBIN}:/usr/bin:/bin:/opt/homebrew/bin" \
        POCKETCLI_ANSIBLE_DIR="${ANSIBLE_DIR}" \
        POCKETCLI_SKILL_LOCK_DIR="${LOCK_DIR}" \
        POCKETCLI_SKILL_LOG_FILE="${LOG_FILE}" \
        POCKETCLI_TEST_ANSIBLE_MODE="${mode}" \
        POCKETCLI_TEST_TIMEOUT="${timeout_mode}" \
        bash "${REPO_ROOT}/scripts/skills/skill_endpoint.sh" "${request}" > "${out}" 2> "${out}.err"
    rc=$?
    set -e
    return "${rc}"
}

req="${WORKDIR}/request.json"
out="${WORKDIR}/endpoint.out"
approval_token="abcdefghijklmnopqrstuvwxyz012345"

write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f733" "disk_check" "srv-prod-01" "diagnose"
run_endpoint "${req}" "${out}" success || fail T-33
assert_field "${out}" status success
assert_contains "${out}" mount_point
assert_contains "${out}" used_percent

printf '{ skill_name: broken\n' > "${req}"
run_endpoint "${req}" "${out}" success && fail T-34
assert_field "${out}" status validation_failed
assert_field "${out}" error invalid_json

write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f735" "bad_skill" "srv-prod-01" "diagnose"
run_endpoint "${req}" "${out}" success && fail T-35
assert_field "${out}" status blocked
assert_field "${out}" error skill_not_whitelisted

write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f736" "disk_check" "srv-prod-01" "diagnose"
run_endpoint "${req}" "${out}" unreachable && fail T-36
assert_field "${out}" status error
assert_field "${out}" error host_unreachable

write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f737" "disk_check" "srv-prod-01" "diagnose"
run_endpoint "${req}" "${out}" success 1 && fail T-37
assert_field "${out}" status timeout
assert_field "${out}" error execution_timeout_60s

write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f738" "service_restart_safe" "srv-prod-01" "execute" '{"service_name":"nginx"}' "${approval_token}"
run_endpoint "${req}" "${out}" changed || fail T-38
assert_field "${out}" status success
assert_field "${out}" changed true

write_request "${req}" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f739" "disk_check" "srv-prod-01" "diagnose"
run_endpoint "${req}" "${out}" missing_result && fail T-39
assert_field "${out}" status error
assert_field "${out}" error missing_pocket_result_task

write_request "${WORKDIR}/request-a.json" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f740" "service_restart_safe" "srv-prod-02" "execute" '{"service_name":"nginx"}' "${approval_token}"
write_request "${WORKDIR}/request-b.json" "a1b2c3d4-e5f6-4789-a012-b3c4d5e6f741" "service_restart_safe" "srv-prod-02" "execute" '{"service_name":"nginx"}' "${approval_token}"
run_endpoint "${WORKDIR}/request-a.json" "${WORKDIR}/endpoint-a.out" sleep_changed &
first_pid=$!
i=0
while [ "$i" -lt 20 ] && [ "$(find "${LOCK_DIR}" -maxdepth 1 -type d -name 'srv-prod-02.*' | wc -l | tr -d ' ')" -eq 0 ]; do
    i=$((i + 1))
    sleep 0.1
done
run_endpoint "${WORKDIR}/request-b.json" "${out}" changed && fail T-40
assert_field "${out}" status blocked
assert_field "${out}" error concurrent_execute_limit:host
wait "${first_pid}"

printf 'PASS skill dispatcher tests T-33..T-40\n'

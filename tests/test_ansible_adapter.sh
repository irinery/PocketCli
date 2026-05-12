#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d "${TMPDIR:-/tmp}/pocketcli-ansible.XXXXXX")
HOME_DIR="${WORKDIR}/home"
MOCKBIN="${WORKDIR}/mockbin"
OUT_FILE="${WORKDIR}/stdout"
ERR_FILE="${WORKDIR}/stderr"
CALL_LOG="${WORKDIR}/calls.log"

cleanup() {
    rm -rf "${WORKDIR}"
}

fail() {
    printf 'FAIL %s\n' "$1" >&2
    printf '%s\n' '--- stdout ---' >&2
    [ -f "${OUT_FILE}" ] && sed -n '1,80p' "${OUT_FILE}" >&2 || true
    printf '%s\n' '--- stderr ---' >&2
    [ -f "${ERR_FILE}" ] && sed -n '1,80p' "${ERR_FILE}" >&2 || true
    exit 1
}

assert_file_contains() {
    FILE="$1"
    TEXT="$2"
    LABEL="$3"
    grep -F -- "${TEXT}" "${FILE}" >/dev/null 2>&1 || fail "${LABEL}"
}

assert_file_not_exists() {
    FILE="$1"
    LABEL="$2"
    [ ! -e "${FILE}" ] || fail "${LABEL}"
}

run_adapter() {
    env HOME="${HOME_DIR}" \
        PATH="${MOCKBIN}:/usr/bin:/bin" \
        POCKET_DIR="${HOME_DIR}/.pocketcli" \
        ANSIBLE_TIMEOUT_SECONDS="${ANSIBLE_TIMEOUT_SECONDS:-300}" \
        ANSIBLE_MAX_OUTPUT_BYTES="${ANSIBLE_MAX_OUTPUT_BYTES:-524288}" \
        ANSIBLE_LOG_ROTATE_BYTES="${ANSIBLE_LOG_ROTATE_BYTES:-10485760}" \
        POCKET_ANSIBLE_TEST_CALL_LOG="${CALL_LOG}" \
        sh "${REPO_ROOT}/lib/ansible_adapter.sh" "$@" >"${OUT_FILE}" 2>"${ERR_FILE}"
}

run_adapter_capture_rc() {
    set +e
    run_adapter "$@"
    RC=$?
    set -e
    printf '%s' "${RC}"
}

reset_ansible_tree() {
    rm -rf "${HOME_DIR}/.pocketcli"
    mkdir -p "${HOME_DIR}/.pocketcli/ansible/inventory" "${HOME_DIR}/.pocketcli/ansible/playbooks" "${HOME_DIR}/.pocketcli/lib"
    cp "${REPO_ROOT}/lib/ansible_inventory.sh" "${HOME_DIR}/.pocketcli/lib/ansible_inventory.sh"
    chmod +x "${HOME_DIR}/.pocketcli/lib/ansible_inventory.sh"
    cat > "${HOME_DIR}/.pocketcli/ansible/inventory/hosts.yml" <<'EOF'
all:
  hosts:
    pocket-test:
      ansible_host: 127.0.0.1
EOF
    printf -- '- hosts: all\n  tasks: []\n' > "${HOME_DIR}/.pocketcli/ansible/playbooks/dummy_playbook.yml"
    : > "${CALL_LOG}"
}

write_ansible_stub() {
    cat > "${MOCKBIN}/ansible" <<'EOF'
#!/usr/bin/env sh
printf 'ansible [core 9.9.9]\n'
EOF
    chmod +x "${MOCKBIN}/ansible"
}

write_playbook_stub() {
    MODE="$1"
    cat > "${MOCKBIN}/ansible-playbook" <<'EOF'
#!/usr/bin/env sh
set -eu
printf '%s\n' "$*" >> "${POCKET_ANSIBLE_TEST_CALL_LOG}"
case "${POCKET_ANSIBLE_STUB_MODE:-success}" in
    success)
        printf 'PLAY OK\n'
        exit 0
        ;;
    failure)
        printf 'PLAY FAIL\n' >&2
        exit 42
        ;;
    timeout)
        sleep 2
        printf 'LATE\n'
        exit 0
        ;;
    large)
        awk 'BEGIN { for (i = 0; i < 200; i++) printf "0123456789" }'
        exit 0
        ;;
esac
EOF
    chmod +x "${MOCKBIN}/ansible-playbook"
    POCKET_ANSIBLE_STUB_MODE="${MODE}"
    export POCKET_ANSIBLE_STUB_MODE
}

trap cleanup EXIT INT TERM HUP

mkdir -p "${MOCKBIN}" "${HOME_DIR}"
write_ansible_stub
write_playbook_stub success
reset_ansible_tree

RC=$(run_adapter_capture_rc run dummy_playbook.yml)
[ "${RC}" = "0" ] || fail "A-01/A-03 execução check deveria retornar 0"
assert_file_contains "${OUT_FILE}" "[DRY-RUN]" "A-03 header dry-run"
assert_file_contains "${CALL_LOG}" "--check" "A-03 ansible chamado com --check"
assert_file_contains "${HOME_DIR}/.pocketcli/logs/ansible.log" '"ansible_version":"ansible [core 9.9.9]"' "A-01 versão registrada"
assert_file_contains "${HOME_DIR}/.pocketcli/logs/ansible.log" '"result":"success"' "A-07 result success"
assert_file_contains "${HOME_DIR}/.pocketcli/logs/ansible.log" '"duration_seconds":' "A-07 duration_seconds"

rm -f "${MOCKBIN}/ansible-playbook" "${HOME_DIR}/.pocketcli/logs/ansible.log"
RC=$(run_adapter_capture_rc run dummy_playbook.yml)
[ "${RC}" = "127" ] || fail "A-02 ausência de ansible-playbook deveria retornar 127"
assert_file_contains "${ERR_FILE}" "ansible-playbook não encontrado. Instale via:" "A-02 mensagem"
assert_file_not_exists "${HOME_DIR}/.pocketcli/logs/ansible.log" "A-02 não deve criar log"

write_playbook_stub success
reset_ansible_tree
RC=$(run_adapter_capture_rc run dummy_playbook.yml --run)
[ "${RC}" = "0" ] || fail "A-04 execução real deveria retornar 0"
assert_file_contains "${OUT_FILE}" "[EXECUÇÃO REAL]" "A-04 header execução real"
if grep -F -- "--check" "${CALL_LOG}" >/dev/null 2>&1; then
    fail "A-04 não deveria passar --check"
fi

reset_ansible_tree
POCKET_ANSIBLE_STUB_MODE=timeout
export POCKET_ANSIBLE_STUB_MODE
ANSIBLE_TIMEOUT_SECONDS=1
export ANSIBLE_TIMEOUT_SECONDS
RC=$(run_adapter_capture_rc run dummy_playbook.yml)
unset ANSIBLE_TIMEOUT_SECONDS
[ "${RC}" = "124" ] || fail "A-05 timeout deveria retornar 124"
assert_file_contains "${ERR_FILE}" "Execução encerrada por timeout (1s)" "A-05 mensagem timeout"
assert_file_contains "${HOME_DIR}/.pocketcli/logs/ansible.log" '"result":"aborted"' "A-05 log aborted"

reset_ansible_tree
rm -rf "${HOME_DIR}/.pocketcli/ansible"
POCKET_ANSIBLE_STUB_MODE=success
export POCKET_ANSIBLE_STUB_MODE
RC=$(run_adapter_capture_rc run dummy_playbook.yml)
[ "${RC}" = "1" ] || fail "A-06 ansible dir ausente deveria retornar 1"
assert_file_contains "${ERR_FILE}" "Diretório ~/.pocketcli/ansible não encontrado. Execute: pocket ansible init" "A-06 mensagem"
assert_file_not_exists "${HOME_DIR}/.pocketcli/logs/ansible.log" "A-06 não deve criar log"

reset_ansible_tree
POCKET_ANSIBLE_STUB_MODE=failure
export POCKET_ANSIBLE_STUB_MODE
RC=$(run_adapter_capture_rc run dummy_playbook.yml)
[ "${RC}" = "2" ] || fail "A-08 falha ansible deveria mapear para 2"
assert_file_contains "${HOME_DIR}/.pocketcli/logs/ansible.log" '"result":"failure"' "A-08 result failure"
assert_file_contains "${HOME_DIR}/.pocketcli/logs/ansible.log" '"exit_code":42' "A-08 exit_code real"
assert_file_contains "${HOME_DIR}/.pocketcli/logs/ansible.log" '"stderr_excerpt":"PLAY FAIL"' "A-08 stderr excerpt"

reset_ansible_tree
POCKET_ANSIBLE_STUB_MODE=success
export POCKET_ANSIBLE_STUB_MODE
# shellcheck disable=SC2016
for BAD_PLAYBOOK in '../etc/passwd' '$(rm -rf /)' 'playbook;ls'; do
    : > "${CALL_LOG}"
    RC=$(run_adapter_capture_rc run "${BAD_PLAYBOOK}")
    [ "${RC}" = "4" ] || fail "A-09 ${BAD_PLAYBOOK} deveria retornar 4"
    assert_file_contains "${ERR_FILE}" "Nome de playbook inválido" "A-09 mensagem"
    [ ! -s "${CALL_LOG}" ] || fail "A-09 não deveria chamar ansible"
done

reset_ansible_tree
mkdir -p "${HOME_DIR}/.pocketcli/logs"
dd if=/dev/zero of="${HOME_DIR}/.pocketcli/logs/ansible.log" bs=1024 count=12 >/dev/null 2>&1
ANSIBLE_LOG_ROTATE_BYTES=10240
export ANSIBLE_LOG_ROTATE_BYTES
RC=$(run_adapter_capture_rc run dummy_playbook.yml)
unset ANSIBLE_LOG_ROTATE_BYTES
[ "${RC}" = "0" ] || fail "A-10 execução com rotação deveria retornar 0"
[ -f "${HOME_DIR}/.pocketcli/logs/ansible.log.1" ] || fail "A-10 deveria criar ansible.log.1"
LINES=$(wc -l < "${HOME_DIR}/.pocketcli/logs/ansible.log" | tr -d '[:space:]')
[ "${LINES}" = "1" ] || fail "A-10 novo log deveria conter só a nova entrada"

reset_ansible_tree
POCKET_ANSIBLE_STUB_MODE=large
export POCKET_ANSIBLE_STUB_MODE
ANSIBLE_MAX_OUTPUT_BYTES=64
export ANSIBLE_MAX_OUTPUT_BYTES
RC=$(run_adapter_capture_rc run dummy_playbook.yml)
unset ANSIBLE_MAX_OUTPUT_BYTES
[ "${RC}" = "0" ] || fail "A-11 execução com output grande deveria retornar 0"
assert_file_contains "${OUT_FILE}" "[TRUNCADO]" "A-11 stdout truncado"
assert_file_contains "${HOME_DIR}/.pocketcli/logs/ansible.log" "[TRUNCADO]" "A-11 log contém output truncado"

mkdir -p "${HOME_DIR}/.pocketcli/scripts/lib" "${HOME_DIR}/.pocketcli/lib"
cp "${REPO_ROOT}/pocket" "${HOME_DIR}/.pocketcli/pocket"
cp "${REPO_ROOT}/scripts/lib/common.sh" "${HOME_DIR}/.pocketcli/scripts/lib/common.sh"
cp "${REPO_ROOT}/lib/ansible_adapter.sh" "${HOME_DIR}/.pocketcli/lib/ansible_adapter.sh"
chmod +x "${HOME_DIR}/.pocketcli/pocket" "${HOME_DIR}/.pocketcli/lib/ansible_adapter.sh"
env HOME="${HOME_DIR}" PATH="${MOCKBIN}:/usr/bin:/bin" sh "${HOME_DIR}/.pocketcli/pocket" ansible help >"${OUT_FILE}" 2>"${ERR_FILE}"
assert_file_contains "${OUT_FILE}" "pocket ansible run <playbook>" "dispatch pocket ansible"

printf 'PASS ansible adapter contract A-01..A-11\n'

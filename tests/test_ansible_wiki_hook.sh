#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d "${TMPDIR:-/tmp}/pocketcli-wiki.XXXXXX")
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
    [ -f "${OUT_FILE}" ] && sed -n '1,140p' "${OUT_FILE}" >&2 || true
    printf '%s\n' '--- stderr ---' >&2
    [ -f "${ERR_FILE}" ] && sed -n '1,140p' "${ERR_FILE}" >&2 || true
    exit 1
}

assert_file_contains() {
    FILE="$1"
    TEXT="$2"
    LABEL="$3"
    grep -F -- "${TEXT}" "${FILE}" >/dev/null 2>&1 || fail "${LABEL}"
}

assert_file_not_contains() {
    FILE="$1"
    TEXT="$2"
    LABEL="$3"
    if grep -F -- "${TEXT}" "${FILE}" >/dev/null 2>&1; then
        fail "${LABEL}"
    fi
}

run_ansible() {
    env HOME="${HOME_DIR}" \
        PATH="${MOCKBIN}:/usr/bin:/bin" \
        POCKET_DIR="${HOME_DIR}/.pocketcli" \
        ANSIBLE_TIMEOUT_SECONDS="${ANSIBLE_TIMEOUT_SECONDS:-300}" \
        POCKET_ANSIBLE_TEST_CALL_LOG="${CALL_LOG}" \
        sh "${REPO_ROOT}/lib/ansible_adapter.sh" "$@" >"${OUT_FILE}" 2>"${ERR_FILE}"
}

run_capture_rc() {
    set +e
    run_ansible "$@"
    RC=$?
    set -e
    printf '%s' "${RC}"
}

run_hook() {
    env HOME="${HOME_DIR}" \
        POCKET_DIR="${HOME_DIR}/.pocketcli" \
        WIKI_LOCK_TIMEOUT_SECONDS="${WIKI_LOCK_TIMEOUT_SECONDS:-10}" \
        sh "${HOME_DIR}/.pocketcli/lib/ansible_wiki_hook.sh" "$@" >"${OUT_FILE}" 2>"${ERR_FILE}"
}

run_hook_capture_rc() {
    set +e
    run_hook "$@"
    RC=$?
    set -e
    printf '%s' "${RC}"
}

reset_home() {
    rm -rf "${HOME_DIR}"
    mkdir -p "${HOME_DIR}/.pocketcli/lib" "${HOME_DIR}/.pocketcli/ansible/inventory" "${HOME_DIR}/.pocketcli/ansible/playbooks" "${MOCKBIN}"
    cp "${REPO_ROOT}/lib/ansible_adapter.sh" "${HOME_DIR}/.pocketcli/lib/ansible_adapter.sh"
    cp "${REPO_ROOT}/lib/ansible_inventory.sh" "${HOME_DIR}/.pocketcli/lib/ansible_inventory.sh"
    cp "${REPO_ROOT}/lib/ansible_wiki_hook.sh" "${HOME_DIR}/.pocketcli/lib/ansible_wiki_hook.sh"
    chmod +x "${HOME_DIR}/.pocketcli/lib/ansible_adapter.sh" "${HOME_DIR}/.pocketcli/lib/ansible_inventory.sh" "${HOME_DIR}/.pocketcli/lib/ansible_wiki_hook.sh"
    cat > "${HOME_DIR}/.pocketcli/ansible/inventory/hosts.yml" <<'EOF'
all:
  hosts:
    local:
      ansible_host: 127.0.0.1
EOF
    printf -- '- hosts: all\n  tasks: []\n' > "${HOME_DIR}/.pocketcli/ansible/playbooks/deploy_nginx.yml"
    : > "${CALL_LOG}"
}

write_ansible_stubs() {
    cat > "${MOCKBIN}/ansible" <<'EOF'
#!/usr/bin/env sh
printf 'ansible [core 9.9.9]\n'
EOF
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
        printf 'fatal: deployment failed\n' >&2
        exit 42
        ;;
    timeout)
        sleep 2
        exit 0
        ;;
esac
EOF
    chmod +x "${MOCKBIN}/ansible" "${MOCKBIN}/ansible-playbook"
}

append_log_line() {
    RUN_ID="$1"
    TS="$2"
    PLAYBOOK="$3"
    MODE="$4"
    RESULT="$5"
    DURATION="$6"
    STDERR="$7"
    mkdir -p "${HOME_DIR}/.pocketcli/logs"
    printf '{"run_id":"%s","timestamp":"%s","playbook":"%s","mode":"%s","inventory_source":"static","result":"%s","exit_code":0,"duration_seconds":%s,"ansible_version":"ansible [core 9.9.9]","hosts_targeted":1,"stderr_excerpt":"%s","stdout_excerpt":"ok"}\n' \
        "${RUN_ID}" "${TS}" "${PLAYBOOK}" "${MODE}" "${RESULT}" "${DURATION}" "${STDERR}" >> "${HOME_DIR}/.pocketcli/logs/ansible.log"
}

index_entry_count() {
    grep -c '^| 20' "${HOME_DIR}/.pocketcli/wiki/ansible/_index.md" | tr -d '[:space:]'
}

trap cleanup EXIT INT TERM HUP

reset_home
write_ansible_stubs
POCKET_ANSIBLE_STUB_MODE=success
export POCKET_ANSIBLE_STUB_MODE
RC=$(run_capture_rc run deploy_nginx.yml --run)
[ "${RC}" = "0" ] || fail "W-01 run success deveria retornar 0"
RUN_ID=$(sed -n 's/.*"run_id":"\([^"]*\)".*/\1/p' "${HOME_DIR}/.pocketcli/logs/ansible.log" | tail -n 1)
WIKI_FILE="${HOME_DIR}/.pocketcli/wiki/ansible/${RUN_ID}.md"
[ -f "${WIKI_FILE}" ] || fail "W-01 arquivo wiki não criado"
assert_file_contains "${WIKI_FILE}" "## Resumo" "W-01 seção Resumo"
assert_file_contains "${WIKI_FILE}" "## Playbook" "W-01 seção Playbook"
assert_file_contains "${WIKI_FILE}" "## Inventário" "W-01 seção Inventário"
assert_file_contains "${WIKI_FILE}" "## Resultado" "W-01 seção Resultado"
assert_file_contains "${WIKI_FILE}" "## Duração" "W-01 seção Duração"
assert_file_contains "${WIKI_FILE}" "## Modo" "W-01 seção Modo"
assert_file_contains "${WIKI_FILE}" "## Timestamp" "W-01 seção Timestamp"
assert_file_contains "${OUT_FILE}" "PocketWiki:" "W-01 caminho exibido"

reset_home
write_ansible_stubs
POCKET_ANSIBLE_STUB_MODE=failure
export POCKET_ANSIBLE_STUB_MODE
RC=$(run_capture_rc run deploy_nginx.yml --run)
[ "${RC}" = "2" ] || fail "W-02 run failure deveria retornar 2"
RUN_ID=$(sed -n 's/.*"run_id":"\([^"]*\)".*/\1/p' "${HOME_DIR}/.pocketcli/logs/ansible.log" | tail -n 1)
WIKI_FILE="${HOME_DIR}/.pocketcli/wiki/ansible/${RUN_ID}.md"
assert_file_contains "${WIKI_FILE}" "❌ FALHA" "W-02 cabeçalho falha"
assert_file_contains "${WIKI_FILE}" "## Erros" "W-02 seção erros"
assert_file_contains "${WIKI_FILE}" "fatal: deployment failed" "W-02 stderr no wiki"

reset_home
RUN_ID="2026-05-11T09:30:00-0300_timeout"
append_log_line "${RUN_ID}" "2026-05-11T09:30:00-0300" "deploy_nginx.yml" check aborted 300 "timeout"
RC=$(run_hook_capture_rc generate "${RUN_ID}")
[ "${RC}" = "0" ] || fail "W-03 hook timeout deveria retornar 0"
WIKI_FILE="${HOME_DIR}/.pocketcli/wiki/ansible/${RUN_ID}.md"
assert_file_contains "${WIKI_FILE}" "⚠️ ABORTADO POR TIMEOUT (300s)" "W-03 nota timeout"

reset_home
I=1
while [ "${I}" -le 3 ]; do
    RID=$(printf '2026-05-11T10:0%d:00-0300_old%d' "${I}" "${I}")
    append_log_line "${RID}" "2026-05-11T10:0${I}:00-0300" "old${I}.yml" check success "${I}" ""
    RC=$(run_hook_capture_rc generate "${RID}")
    [ "${RC}" = "0" ] || fail "W-04 hook antigo ${I}"
    I=$((I + 1))
done
RID_NEW="2026-05-11T10:04:00-0300_new"
append_log_line "${RID_NEW}" "2026-05-11T10:04:00-0300" "new.yml" run success 4 ""
RC=$(run_hook_capture_rc generate "${RID_NEW}")
[ "${RC}" = "0" ] || fail "W-04 hook novo"
FIRST_ENTRY=$(awk '/^\| 20/ { print; exit }' "${HOME_DIR}/.pocketcli/wiki/ansible/_index.md")
printf '%s\n' "${FIRST_ENTRY}" | grep -F "new.yml" >/dev/null 2>&1 || fail "W-04 nova entrada no topo"
[ "$(index_entry_count)" = "4" ] || fail "W-04 índice deveria ter 4 entradas"

reset_home
mkdir -p "${HOME_DIR}/.pocketcli/wiki/ansible"
OLD_RUN="2026-05-11T00:00:00-0300_oldest"
printf 'preserved\n' > "${HOME_DIR}/.pocketcli/wiki/ansible/${OLD_RUN}.md"
{
    printf '# Ansible Runs — PocketWiki Index\n\n'
    printf '_Última atualização: 2026-05-11T00:00:00-0300_\n\n'
    printf '| Data | Playbook | Resultado | Modo | Duração | Arquivo |\n'
    printf '|---|---|---|---|---|---|\n'
    I=1
    while [ "${I}" -le 499 ]; do
        printf '| 2026-05-11 12:%03d | pb%03d.yml | ✅ sucesso | check | 1s | [ver](./run%03d.md) |\n' "${I}" "${I}" "${I}"
        I=$((I + 1))
    done
    printf '| 2026-05-11 00:00:00 | oldest.yml | ✅ sucesso | check | 1s | [ver](./%s.md) |\n' "${OLD_RUN}"
} > "${HOME_DIR}/.pocketcli/wiki/ansible/_index.md"
RID_NEW="2026-05-11T13:00:00-0300_newlimit"
append_log_line "${RID_NEW}" "2026-05-11T13:00:00-0300" "newlimit.yml" check success 1 ""
RC=$(run_hook_capture_rc generate "${RID_NEW}")
[ "${RC}" = "0" ] || fail "W-05 hook limite"
[ "$(index_entry_count)" = "500" ] || fail "W-05 índice deveria manter 500 entradas"
assert_file_not_contains "${HOME_DIR}/.pocketcli/wiki/ansible/_index.md" "${OLD_RUN}.md" "W-05 entrada mais antiga removida do índice"
assert_file_contains "${HOME_DIR}/.pocketcli/wiki/ansible/${OLD_RUN}.md" "preserved" "W-05 arquivo antigo preservado"

reset_home
rm -rf "${HOME_DIR}/.pocketcli/wiki/ansible"
RID="2026-05-11T14:00:00-0300_first"
append_log_line "${RID}" "2026-05-11T14:00:00-0300" "first.yml" check success 1 ""
RC=$(run_hook_capture_rc generate "${RID}")
[ "${RC}" = "0" ] || fail "W-06 hook primeira execução"
[ -d "${HOME_DIR}/.pocketcli/wiki/ansible" ] || fail "W-06 diretório não criado"
[ -f "${HOME_DIR}/.pocketcli/wiki/ansible/_index.md" ] || fail "W-06 índice não criado"

reset_home
RID="missing_run"
RC=$(run_hook_capture_rc generate "${RID}")
[ "${RC}" = "0" ] || fail "W-07 hook sem log deveria retornar 0"
WIKI_FILE="${HOME_DIR}/.pocketcli/wiki/ansible/${RID}.md"
assert_file_contains "${WIKI_FILE}" "⚠️ Dados de log indisponíveis para este run_id" "W-07 nota sem log"
assert_file_contains "${WIKI_FILE}" "| Playbook | N/A |" "W-07 campos N/A"

reset_home
I=1
while [ "${I}" -le 8 ]; do
    RID=$(printf '2026-05-11T15:%02d:00-0300_log%d' "${I}" "${I}")
    append_log_line "${RID}" "2026-05-11T15:${I}:00-0300" "log${I}.yml" check success "${I}" ""
    RC=$(run_hook_capture_rc generate "${RID}")
    [ "${RC}" = "0" ] || fail "W-08 gera log ${I}"
    I=$((I + 1))
done
RC=$(run_capture_rc log --last 5)
[ "${RC}" = "0" ] || fail "W-08 log --last deveria retornar 0"
ROWS=$(awk 'NR > 1 && /log[0-9]+\.yml/ { count++ } END { print count + 0 }' "${OUT_FILE}")
[ "${ROWS}" = "5" ] || fail "W-08 deveria exibir 5 entradas"
assert_file_contains "${OUT_FILE}" "[ver](./" "W-08 mostra link wiki"

reset_home
RC=$(run_capture_rc log --last 5)
[ "${RC}" = "0" ] || fail "W-09 log vazio deveria retornar 0"
assert_file_contains "${OUT_FILE}" "Nenhuma execução registrada ainda." "W-09 mensagem vazio"

reset_home
ESC=$(printf '\033')
RID="2026-05-11T16:00:00-0300_ansi"
append_log_line "${RID}" "2026-05-11T16:00:00-0300" "ansi.yml" run failure 1 "${ESC}[31mRED${ESC}[0m clean"
RC=$(run_hook_capture_rc generate "${RID}")
[ "${RC}" = "0" ] || fail "W-10 hook ansi"
WIKI_FILE="${HOME_DIR}/.pocketcli/wiki/ansible/${RID}.md"
assert_file_contains "${WIKI_FILE}" "RED clean" "W-10 texto sanitizado mantido"
assert_file_not_contains "${WIKI_FILE}" "[31m" "W-10 escape removido"

reset_home
RID_A="2026-05-11T17:00:00-0300_parallel_a"
RID_B="2026-05-11T17:00:01-0300_parallel_b"
append_log_line "${RID_A}" "2026-05-11T17:00:00-0300" "parallel_a.yml" check success 1 ""
append_log_line "${RID_B}" "2026-05-11T17:00:01-0300" "parallel_b.yml" check success 1 ""
env HOME="${HOME_DIR}" POCKET_DIR="${HOME_DIR}/.pocketcli" sh "${HOME_DIR}/.pocketcli/lib/ansible_wiki_hook.sh" generate "${RID_A}" >"${WORKDIR}/parallel_a.out" 2>"${WORKDIR}/parallel_a.err" &
PID_A=$!
env HOME="${HOME_DIR}" POCKET_DIR="${HOME_DIR}/.pocketcli" sh "${HOME_DIR}/.pocketcli/lib/ansible_wiki_hook.sh" generate "${RID_B}" >"${WORKDIR}/parallel_b.out" 2>"${WORKDIR}/parallel_b.err" &
PID_B=$!
wait "${PID_A}" || fail "W-11 hook paralelo A"
wait "${PID_B}" || fail "W-11 hook paralelo B"
assert_file_contains "${HOME_DIR}/.pocketcli/wiki/ansible/_index.md" "parallel_a.yml" "W-11 entrada A preservada"
assert_file_contains "${HOME_DIR}/.pocketcli/wiki/ansible/_index.md" "parallel_b.yml" "W-11 entrada B preservada"

printf 'PASS ansible wiki hook contract W-01..W-11\n'

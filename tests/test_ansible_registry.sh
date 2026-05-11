#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d "${TMPDIR:-/tmp}/pocketcli-registry.XXXXXX")
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

reset_home() {
    rm -rf "${HOME_DIR}"
    mkdir -p "${HOME_DIR}/.pocketcli/lib" "${HOME_DIR}/.pocketcli/ansible/inventory" "${HOME_DIR}/.pocketcli/ansible/playbooks"
    cp "${REPO_ROOT}/lib/ansible_adapter.sh" "${HOME_DIR}/.pocketcli/lib/ansible_adapter.sh"
    cp "${REPO_ROOT}/lib/ansible_inventory.sh" "${HOME_DIR}/.pocketcli/lib/ansible_inventory.sh"
    cp "${REPO_ROOT}/lib/ansible_registry.sh" "${HOME_DIR}/.pocketcli/lib/ansible_registry.sh"
    cp "${REPO_ROOT}/lib/ansible_init.sh" "${HOME_DIR}/.pocketcli/lib/ansible_init.sh"
    chmod +x "${HOME_DIR}/.pocketcli/lib/ansible_adapter.sh" "${HOME_DIR}/.pocketcli/lib/ansible_inventory.sh" "${HOME_DIR}/.pocketcli/lib/ansible_registry.sh" "${HOME_DIR}/.pocketcli/lib/ansible_init.sh"
    cat > "${HOME_DIR}/.pocketcli/ansible/inventory/hosts.yml" <<'EOF'
all:
  hosts:
    local:
      ansible_host: 127.0.0.1
EOF
    : > "${CALL_LOG}"
}

write_playbook() {
    SLUG="$1"
    DESCRIPTION="$2"
    CATEGORY="$3"
    SAFE_MODES="$4"
    INVENTORY_SOURCE="$5"
    FILE="${HOME_DIR}/.pocketcli/ansible/playbooks/${SLUG}.yml"

    {
        printf -- '- name: pocket_meta\n'
        printf '  hosts: localhost\n'
        printf '  gather_facts: false\n'
        printf '  vars:\n'
        printf '    pocket_meta:\n'
        printf '      name: %s\n' "${SLUG}"
        printf '      description: %s\n' "${DESCRIPTION}"
        printf '      author: irinery\n'
        printf '      version: "1.0.0"\n'
        printf '      category: %s\n' "${CATEGORY}"
        printf '      safe_modes:\n'
        OLD_IFS=${IFS}
        IFS=,
        for MODE in ${SAFE_MODES}; do
            printf '        - %s\n' "${MODE}"
        done
        IFS=${OLD_IFS}
        printf '      inventory_source: %s\n' "${INVENTORY_SOURCE}"
        printf '      created_at: "2026-05-11"\n'
        printf '      updated_at: "2026-05-11"\n'
        printf '  tasks: []\n'
        printf -- '- name: real play\n'
        printf '  hosts: all\n'
        printf '  tasks: []\n'
    } > "${FILE}"
}

write_playbook_without_meta() {
    SLUG="$1"
    cat > "${HOME_DIR}/.pocketcli/ansible/playbooks/${SLUG}.yml" <<'EOF'
- name: no metadata
  hosts: all
  tasks: []
EOF
}

write_invalid_yaml_playbook() {
    SLUG="$1"
    cat > "${HOME_DIR}/.pocketcli/ansible/playbooks/${SLUG}.yml" <<'EOF'
- name: pocket_meta
  hosts: localhost
  vars: [
EOF
}

write_ansible_stubs() {
    mkdir -p "${MOCKBIN}"
    cat > "${MOCKBIN}/ansible" <<'EOF'
#!/usr/bin/env sh
printf 'ansible [core 9.9.9]\n'
EOF
    cat > "${MOCKBIN}/ansible-playbook" <<'EOF'
#!/usr/bin/env sh
set -eu
printf '%s\n' "$*" >> "${POCKET_ANSIBLE_TEST_CALL_LOG}"
printf 'PLAY OK\n'
EOF
    chmod +x "${MOCKBIN}/ansible" "${MOCKBIN}/ansible-playbook"
}

trap cleanup EXIT INT TERM HUP

write_ansible_stubs

reset_home
write_playbook diag_info "Collect facts" diagnostic check static
write_playbook deploy_nginx "Deploy nginx" deploy check,run static
write_playbook rotate_logs "Rotate logs" maintenance check,diff static
RC=$(run_capture_rc list)
[ "${RC}" = "0" ] || fail "R-01 list deveria retornar 0"
assert_file_contains "${OUT_FILE}" "diag_info" "R-01 lista diag_info"
assert_file_contains "${OUT_FILE}" "Deploy nginx" "R-01 lista descrição"
assert_file_contains "${OUT_FILE}" "deploy" "R-01 lista categoria"
assert_file_contains "${OUT_FILE}" "modo_padrão" "R-01 mostra modo padrão"
assert_file_contains "${OUT_FILE}" "[check, run]" "R-01 mostra safe_modes"

reset_home
RC=$(run_capture_rc list)
[ "${RC}" = "0" ] || fail "R-02 list vazio deveria retornar 0"
assert_file_contains "${OUT_FILE}" "Nenhum playbook encontrado em ~/.pocketcli/ansible/playbooks/" "R-02 mensagem vazio"

reset_home
write_playbook deploy_nginx "Deploy nginx" deploy check,run static
write_playbook rotate_logs "Rotate logs" maintenance check static
write_playbook_without_meta bad_meta
RC=$(run_capture_rc list)
[ "${RC}" = "0" ] || fail "R-03 list com inválido deveria retornar 0"
assert_file_contains "${OUT_FILE}" "deploy_nginx" "R-03 lista válido"
assert_file_contains "${OUT_FILE}" "rotate_logs" "R-03 lista segundo válido"
assert_file_contains "${OUT_FILE}" "Playbooks com erro de metadados: 1" "R-03 seção inválidos"
assert_file_contains "${OUT_FILE}" "bad_meta: pocket_meta ausente" "R-03 detalhe inválido"

reset_home
write_playbook deploy_nginx "Deploy nginx" deploy check,run static
RC=$(run_capture_rc run deploy_nginx)
[ "${RC}" = "0" ] || fail "R-04 run check deveria retornar 0"
assert_file_contains "${OUT_FILE}" "[DRY-RUN] deploy_nginx" "R-04 header dry-run"
assert_file_contains "${CALL_LOG}" "--check" "R-04 adapter recebeu --check"
assert_file_contains "${HOME_DIR}/.pocketcli/logs/ansible.log" '"result":"success"' "R-04 log gravado"

reset_home
write_playbook deploy_nginx "Deploy nginx" deploy check static
RC=$(run_capture_rc run deploy_nginx --run)
[ "${RC}" = "1" ] || fail "R-05 run não permitido deveria retornar 1"
assert_file_contains "${ERR_FILE}" "Playbook deploy_nginx não permite modo run. safe_modes: [check]" "R-05 mensagem safe_modes"
[ ! -s "${CALL_LOG}" ] || fail "R-05 não deveria chamar adapter"

reset_home
write_playbook deploy_nginx "Deploy nginx" deploy check static
RC=$(run_capture_rc run inexistente)
[ "${RC}" = "3" ] || fail "R-06 inexistente deveria retornar 3"
assert_file_contains "${ERR_FILE}" "Playbook 'inexistente' não encontrado. Use: pocket ansible list" "R-06 mensagem not found"

reset_home
write_invalid_yaml_playbook bad_yaml
RC=$(run_capture_rc run bad_yaml)
[ "${RC}" = "3" ] || fail "R-07 YAML inválido deveria retornar 3"
assert_file_contains "${ERR_FILE}" "Erro de sintaxe YAML em bad_yaml.yml:" "R-07 mensagem YAML"
[ ! -s "${CALL_LOG}" ] || fail "R-07 não deveria chamar adapter"

reset_home
I=1
while [ "${I}" -le 201 ]; do
    SLUG=$(printf 'pb%03d' "${I}")
    write_playbook "${SLUG}" "Playbook ${I}" utility check static
    I=$((I + 1))
done
RC=$(run_capture_rc list)
[ "${RC}" = "0" ] || fail "R-08 list limite deveria retornar 0"
assert_file_contains "${ERR_FILE}" "Limite de 200 playbooks atingido — os demais foram ignorados" "R-08 aviso limite"
assert_file_contains "${OUT_FILE}" "pb200" "R-08 indexa pb200"
assert_file_not_contains "${OUT_FILE}" "pb201" "R-08 ignora pb201"

reset_home
write_playbook deploy_nginx "Deploy nginx" deploy check static
EXTERNAL_FILE="${WORKDIR}/external.yml"
cat > "${EXTERNAL_FILE}" <<'EOF'
outside content
EOF
ln -s "${EXTERNAL_FILE}" "${HOME_DIR}/.pocketcli/ansible/playbooks/external.yml"
RC=$(run_capture_rc list)
[ "${RC}" = "0" ] || fail "R-09 list com symlink deveria retornar 0"
assert_file_contains "${OUT_FILE}" "deploy_nginx" "R-09 lista playbook real"
assert_file_not_contains "${OUT_FILE}" "external" "R-09 ignora symlink"

reset_home
write_playbook weird "Invalid category" banana check static
RC=$(run_capture_rc list)
[ "${RC}" = "0" ] || fail "R-10 list categoria inválida deveria retornar 0"
assert_file_contains "${OUT_FILE}" "weird: categoria desconhecida: banana" "R-10 detalhe categoria"
RC=$(run_capture_rc run weird)
[ "${RC}" = "3" ] || fail "R-10 run categoria inválida deveria retornar 3"
assert_file_contains "${ERR_FILE}" "Playbook 'weird' inválido: categoria desconhecida: banana" "R-10 run bloqueado"

reset_home
write_playbook ts_diag "TS diagnostic" diagnostic check tailscale
rm -f "${HOME_DIR}/.pocketcli/ansible/inventory/tailscale_generated.yml"
RC=$(run_capture_rc run ts_diag)
[ "${RC}" = "2" ] || fail "R-11 tailscale sem cache deveria retornar 2"
assert_file_contains "${ERR_FILE}" "Playbook requer fonte 'tailscale' mas inventário não foi gerado. Execute: pocket ansible inventory refresh" "R-11 mensagem tailscale"
[ ! -s "${CALL_LOG}" ] || fail "R-11 não deveria chamar adapter"

printf 'PASS ansible registry contract R-01..R-11\n'

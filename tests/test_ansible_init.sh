#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d "${TMPDIR:-/tmp}/pocketcli-init.XXXXXX")
HOME_DIR="${WORKDIR}/home"
OUT_FILE="${WORKDIR}/stdout"
ERR_FILE="${WORKDIR}/stderr"

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
    ASSERT_FILE="$1"
    TEXT="$2"
    LABEL="$3"
    grep -F -- "${TEXT}" "${ASSERT_FILE}" >/dev/null 2>&1 || fail "${LABEL}"
}

assert_file_not_contains() {
    ASSERT_FILE="$1"
    TEXT="$2"
    LABEL="$3"
    if grep -F -- "${TEXT}" "${ASSERT_FILE}" >/dev/null 2>&1; then
        fail "${LABEL}"
    fi
}

run_ansible() {
    env HOME="${HOME_DIR}" \
        POCKET_DIR="${HOME_DIR}/.pocketcli" \
        PATH="/usr/bin:/bin" \
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
    mkdir -p "${HOME_DIR}/.pocketcli/lib"
    cp "${REPO_ROOT}/lib/ansible_adapter.sh" "${HOME_DIR}/.pocketcli/lib/ansible_adapter.sh"
    cp "${REPO_ROOT}/lib/ansible_registry.sh" "${HOME_DIR}/.pocketcli/lib/ansible_registry.sh"
    cp "${REPO_ROOT}/lib/ansible_init.sh" "${HOME_DIR}/.pocketcli/lib/ansible_init.sh"
    chmod +x "${HOME_DIR}/.pocketcli/lib/ansible_adapter.sh" "${HOME_DIR}/.pocketcli/lib/ansible_registry.sh" "${HOME_DIR}/.pocketcli/lib/ansible_init.sh"
}

playbook_path() {
    printf '%s/.pocketcli/ansible/playbooks/%s.yml' "${HOME_DIR}" "$1"
}

count_mode_line() {
    COUNT_FILE="$1"
    MODE="$2"
    grep -c "^[[:space:]]*- ${MODE}$" "${COUNT_FILE}" | tr -d '[:space:]'
}

trap cleanup EXIT INT TERM HUP

TODAY=$(date '+%Y-%m-%d')

reset_home
RC=$(run_capture_rc init meu_playbook --category diagnostic)
[ "${RC}" = "0" ] || fail "P-01 init diagnostic deveria retornar 0"
FILE=$(playbook_path meu_playbook)
[ -f "${FILE}" ] || fail "P-01 arquivo não criado"
assert_file_contains "${FILE}" 'name: "meu_playbook"' "P-01 pocket_meta name"
assert_file_contains "${FILE}" 'category: "diagnostic"' "P-01 categoria"
assert_file_contains "${FILE}" 'inventory_source: merged' "P-01 inventory_source"
assert_file_contains "${FILE}" "created_at: \"${TODAY}\"" "P-01 created_at atual"
assert_file_contains "${FILE}" "updated_at: \"${TODAY}\"" "P-01 updated_at atual"
[ "$(count_mode_line "${FILE}" check)" = "1" ] || fail "P-01 safe_modes check"
[ "$(count_mode_line "${FILE}" diff)" = "1" ] || fail "P-01 safe_modes diff"
[ "$(count_mode_line "${FILE}" run)" = "0" ] || fail "P-01 diagnostic não deveria incluir run"

reset_home
RC=$(run_capture_rc init meu_playbook)
[ "${RC}" = "0" ] || fail "P-02 init default deveria retornar 0"
FILE=$(playbook_path meu_playbook)
assert_file_contains "${FILE}" 'category: "utility"' "P-02 categoria utility"
assert_file_contains "${ERR_FILE}" "Categoria padrão 'utility' aplicada — considere especificar --category" "P-02 aviso default"

reset_home
RC=$(run_capture_rc init meu_playbook --category utility)
[ "${RC}" = "0" ] || fail "P-03 setup inicial"
FILE=$(playbook_path meu_playbook)
printf 'original-content\n' > "${FILE}"
RC=$(run_capture_rc init meu_playbook --category deploy)
[ "${RC}" = "1" ] || fail "P-03 existente deveria retornar 1"
assert_file_contains "${ERR_FILE}" "Playbook 'meu_playbook' já existe. Use --force para sobrescrever." "P-03 mensagem existente"
assert_file_contains "${FILE}" "original-content" "P-03 arquivo existente modificado"

reset_home
RC=$(run_capture_rc init meu_playbook --category utility)
[ "${RC}" = "0" ] || fail "P-04 setup inicial"
FILE=$(playbook_path meu_playbook)
printf 'old\n' > "${FILE}"
RC=$(run_capture_rc init meu_playbook --category deploy --force)
[ "${RC}" = "0" ] || fail "P-04 force deveria retornar 0"
assert_file_contains "${OUT_FILE}" "Playbook sobrescrito." "P-04 mensagem overwrite"
assert_file_contains "${FILE}" 'category: "deploy"' "P-04 arquivo sobrescrito"
assert_file_not_contains "${FILE}" "old" "P-04 conteúdo antigo removido"

reset_home
RC=$(run_capture_rc init meu_playbook --category utility)
[ "${RC}" = "0" ] || fail "P-05 init deveria retornar 0"
RC=$(run_capture_rc list)
[ "${RC}" = "0" ] || fail "P-05 list deveria retornar 0"
assert_file_contains "${OUT_FILE}" "meu_playbook" "P-05 aparece na listagem"
assert_file_not_contains "${OUT_FILE}" "Playbooks com erro de metadados" "P-05 sem erros de metadados"

reset_home
RC=$(run_capture_rc init secure_book --category security)
[ "${RC}" = "0" ] || fail "P-06 security deveria retornar 0"
FILE=$(playbook_path secure_book)
[ "$(count_mode_line "${FILE}" check)" = "1" ] || fail "P-06 security check"
[ "$(count_mode_line "${FILE}" diff)" = "1" ] || fail "P-06 security diff"
[ "$(count_mode_line "${FILE}" run)" = "0" ] || fail "P-06 security não deveria incluir run"
assert_file_contains "${FILE}" "# security: modo run requer autorização explícita" "P-06 comentário security"

reset_home
for BAD_NAME in "meu playbook" "my/book" "book;run"; do
    RC=$(run_capture_rc init "${BAD_NAME}" --category utility)
    [ "${RC}" = "1" ] || fail "P-07 ${BAD_NAME} deveria retornar 1"
    assert_file_contains "${ERR_FILE}" "Nome de playbook inválido. Use apenas [a-z0-9_-]." "P-07 mensagem inválido"
done
[ ! -d "${HOME_DIR}/.pocketcli/ansible/playbooks" ] || {
    if find "${HOME_DIR}/.pocketcli/ansible/playbooks" -type f | grep -q .; then
        fail "P-07 não deveria criar arquivos"
    fi
}

reset_home
RC=$(run_capture_rc init meu_playbook --category deploy)
[ "${RC}" = "0" ] || fail "P-08 deploy deveria retornar 0"
FILE=$(playbook_path meu_playbook)
[ "$(count_mode_line "${FILE}" check)" = "1" ] || fail "P-08 deploy check"
[ "$(count_mode_line "${FILE}" diff)" = "1" ] || fail "P-08 deploy diff"
[ "$(count_mode_line "${FILE}" run)" = "1" ] || fail "P-08 deploy run"
assert_file_contains "${FILE}" "inventory_source: merged" "P-08 inventory merged"
assert_file_contains "${FILE}" "# Variáveis do playbook" "P-08 bloco vars"
assert_file_contains "${FILE}" "# Formato: pocket_meu_playbook_<variavel>" "P-08 comentário var env"

reset_home
RC=$(run_capture_rc init meu_playbook --category diagnostic)
[ "${RC}" = "0" ] || fail "P-09 diagnostic deveria retornar 0"
FILE=$(playbook_path meu_playbook)
[ "$(count_mode_line "${FILE}" check)" = "1" ] || fail "P-09 diagnostic check"
[ "$(count_mode_line "${FILE}" diff)" = "1" ] || fail "P-09 diagnostic diff"
[ "$(count_mode_line "${FILE}" run)" = "0" ] || fail "P-09 diagnostic não deveria incluir run"
assert_file_contains "${FILE}" "# diagnostic: nunca altera estado dos hosts" "P-09 comentário diagnostic"

reset_home
LONG_NAME="a$(printf 'b%.0s' $(seq 1 80))"
RC=$(run_capture_rc init "${LONG_NAME}" --category utility)
[ "${RC}" = "1" ] || fail "P-10 nome longo deveria retornar 1"
assert_file_contains "${ERR_FILE}" "Nome de playbook excede 80 caracteres." "P-10 mensagem longo"
[ ! -d "${HOME_DIR}/.pocketcli/ansible/playbooks" ] || {
    if find "${HOME_DIR}/.pocketcli/ansible/playbooks" -type f | grep -q .; then
        fail "P-10 não deveria criar arquivos"
    fi
}

printf 'PASS ansible init contract P-01..P-10\n'

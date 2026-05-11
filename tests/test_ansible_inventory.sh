#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d "${TMPDIR:-/tmp}/pocketcli-inventory.XXXXXX")
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
    [ -f "${OUT_FILE}" ] && sed -n '1,120p' "${OUT_FILE}" >&2 || true
    printf '%s\n' '--- stderr ---' >&2
    [ -f "${ERR_FILE}" ] && sed -n '1,120p' "${ERR_FILE}" >&2 || true
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
    cp "${REPO_ROOT}/lib/ansible_inventory.sh" "${HOME_DIR}/.pocketcli/lib/ansible_inventory.sh"
    chmod +x "${HOME_DIR}/.pocketcli/lib/ansible_inventory.sh"
    printf -- '- hosts: all\n  tasks: []\n' > "${HOME_DIR}/.pocketcli/ansible/playbooks/dummy_playbook.yml"
    : > "${CALL_LOG}"
}

write_static_inventory_3() {
    cat > "${HOME_DIR}/.pocketcli/ansible/inventory/hosts.yml" <<'EOF'
all:
  vars:
    ansible_user: root
  hosts:
    web01:
      ansible_host: 100.1.1.1
      pocket_tags:
        - web
    db01:
      ansible_host: 100.1.1.2
      pocket_tags:
        - db
    jump01:
      ansible_host: jump.internal
  children:
    prod:
      hosts:
        web01: {}
        db01: {}
EOF
}

write_static_conflict() {
    cat > "${HOME_DIR}/.pocketcli/ansible/inventory/hosts.yml" <<'EOF'
all:
  hosts:
    web01:
      ansible_host: 100.1.1.1
EOF
}

write_tailscale_conflict() {
    cat > "${HOME_DIR}/.pocketcli/ansible/inventory/tailscale_generated.yml" <<'EOF'
# Gerado automaticamente — não editar manualmente
# generated_at: 2026-05-11T09:00:00-0300
# source: tailscale
all:
  hosts:
    web01:
      ansible_host: 100.1.1.2
      ansible_port: 22
      pocket_source: tailscale
      pocket_tailscale_id: node-web01
  children:
    tailscale:
      hosts:
        web01: {}
EOF
}

write_static_invalid_host() {
    cat > "${HOME_DIR}/.pocketcli/ansible/inventory/hosts.yml" <<'EOF'
all:
  hosts:
    "host; rm -rf /":
      ansible_host: 100.9.9.9
    good01:
      ansible_host: 100.9.9.10
EOF
}

write_many_static() {
    COUNT="$1"
    {
        printf 'all:\n'
        printf '  hosts:\n'
        I=1
        while [ "${I}" -le "${COUNT}" ]; do
            printf '    static%02d:\n' "${I}"
            printf '      ansible_host: 100.10.0.%d\n' "${I}"
            I=$((I + 1))
        done
    } > "${HOME_DIR}/.pocketcli/ansible/inventory/hosts.yml"
}

write_many_tailscale() {
    COUNT="$1"
    FILE="${HOME_DIR}/.pocketcli/ansible/inventory/tailscale_generated.yml"
    {
        printf '# Gerado automaticamente — não editar manualmente\n'
        printf '# generated_at: 2026-05-11T09:10:00-0300\n'
        printf '# source: tailscale\n'
        printf 'all:\n'
        printf '  hosts:\n'
        I=1
        while [ "${I}" -le "${COUNT}" ]; do
            printf '    ts%02d:\n' "${I}"
            printf '      ansible_host: 100.20.0.%d\n' "${I}"
            printf '      ansible_port: 22\n'
            printf '      pocket_source: tailscale\n'
            printf '      pocket_tailscale_id: ts-%02d\n' "${I}"
            I=$((I + 1))
        done
        printf '  children:\n'
        printf '    tailscale:\n'
        printf '      hosts:\n'
        I=1
        while [ "${I}" -le "${COUNT}" ]; do
            printf '        ts%02d: {}\n' "${I}"
            I=$((I + 1))
        done
    } > "${FILE}"
}

write_tailscale_stub() {
    cat > "${MOCKBIN}/tailscale" <<'EOF'
#!/usr/bin/env sh
set -eu
if [ "${1:-}" != "status" ] || [ "${2:-}" != "--json" ]; then
    exit 1
fi
case "${POCKET_TAILSCALE_MODE:-offline}" in
    online4)
        cat <<'JSON'
{"Peer":{"node1":{"HostName":"web01","TailscaleIPs":["100.64.0.1"],"ID":"node1"},"node2":{"HostName":"db01","TailscaleIPs":["100.64.0.2"],"ID":"node2"},"node3":{"HostName":"jump01","TailscaleIPs":["100.64.0.3"],"ID":"node3"},"node4":{"HostName":"worker01","TailscaleIPs":["100.64.0.4"],"ID":"node4"}}}
JSON
        exit 0
        ;;
    injection)
        cat <<'JSON'
{"Peer":{"bad":{"HostName":"host; rm -rf /","TailscaleIPs":["100.64.9.9"],"ID":"bad-node"}}}
JSON
        exit 0
        ;;
    online)
        cat <<'JSON'
{"Peer":{}}
JSON
        exit 0
        ;;
    offline)
        exit 2
        ;;
esac
EOF
    chmod +x "${MOCKBIN}/tailscale"
}

write_ansible_stubs() {
    cat > "${MOCKBIN}/ansible" <<'EOF'
#!/usr/bin/env sh
printf 'ansible [core 9.9.9]\n'
EOF
    cat > "${MOCKBIN}/ansible-playbook" <<'EOF'
#!/usr/bin/env sh
printf '%s\n' "$*" >> "${POCKET_ANSIBLE_TEST_CALL_LOG}"
printf 'PLAY OK\n'
EOF
    chmod +x "${MOCKBIN}/ansible" "${MOCKBIN}/ansible-playbook"
}

trap cleanup EXIT INT TERM HUP

mkdir -p "${MOCKBIN}"
write_tailscale_stub
write_ansible_stubs

reset_home
write_static_inventory_3
RC=$(run_capture_rc inventory --source static)
[ "${RC}" = "0" ] || fail "I-01 inventory static deveria retornar 0"
assert_file_contains "${OUT_FILE}" "web01" "I-01 mostra web01"
assert_file_contains "${OUT_FILE}" "100.1.1.1" "I-01 mostra IP"
assert_file_contains "${OUT_FILE}" "web,prod" "I-01 mostra grupos"
assert_file_contains "${OUT_FILE}" "db01" "I-01 mostra db01"
assert_file_contains "${OUT_FILE}" "jump01" "I-01 mostra jump01"

reset_home
POCKET_TAILSCALE_MODE=online4
export POCKET_TAILSCALE_MODE
RC=$(run_capture_rc inventory refresh)
[ "${RC}" = "0" ] || fail "I-02 refresh deveria retornar 0"
TS_FILE="${HOME_DIR}/.pocketcli/ansible/inventory/tailscale_generated.yml"
[ -f "${TS_FILE}" ] || fail "I-02 tailscale_generated.yml não criado"
COUNT=$(grep -c 'ansible_host:' "${TS_FILE}" | tr -d '[:space:]')
[ "${COUNT}" = "4" ] || fail "I-02 deveria gerar 4 hosts"
assert_file_contains "${TS_FILE}" "ansible_host: 100.64.0.4" "I-02 IP Tailscale preenchido"

printf 'preserve-me\n' > "${TS_FILE}"
POCKET_TAILSCALE_MODE=offline
export POCKET_TAILSCALE_MODE
RC=$(run_capture_rc inventory refresh)
[ "${RC}" = "2" ] || fail "I-03 refresh offline deveria retornar 2"
assert_file_contains "${ERR_FILE}" "Tailscale não está ativo — inventário dinâmico não gerado" "I-03 mensagem offline"
assert_file_contains "${TS_FILE}" "preserve-me" "I-03 cache anterior preservado"

reset_home
write_static_conflict
write_tailscale_conflict
POCKET_TAILSCALE_MODE=online
export POCKET_TAILSCALE_MODE
RC=$(run_capture_rc inventory --source merged)
[ "${RC}" = "0" ] || fail "I-04 merged deveria retornar 0"
WEB_COUNT=$(grep -c '^web01[[:space:]]' "${OUT_FILE}" | tr -d '[:space:]')
[ "${WEB_COUNT}" = "1" ] || fail "I-04 web01 deveria aparecer uma vez"
assert_file_contains "${OUT_FILE}" "100.1.1.1" "I-04 estático prevalece"
assert_file_not_contains "${OUT_FILE}" "100.1.1.2" "I-04 IP dinâmico não deveria prevalecer"
assert_file_contains "${ERR_FILE}" "Conflito resolvido: web01 — estático prevalece" "I-04 aviso conflito"

reset_home
rm -rf "${HOME_DIR}/.pocketcli/ansible/inventory"
RC=$(run_capture_rc inventory)
[ "${RC}" = "0" ] || fail "I-05 inventory default deveria inicializar"
assert_file_contains "${OUT_FILE}" "Inventário inicializado em ~/.pocketcli/ansible/inventory/" "I-05 mensagem init"
[ -f "${HOME_DIR}/.pocketcli/ansible/inventory/hosts.yml" ] || fail "I-05 hosts.yml não criado"

reset_home
cat > "${HOME_DIR}/.pocketcli/ansible/inventory/hosts.yml" <<'EOF'
all:
  hosts:
    web01: [
EOF
RC=$(run_capture_rc inventory --source static)
[ "${RC}" = "3" ] || fail "I-06 YAML inválido deveria retornar 3"
assert_file_contains "${ERR_FILE}" "Inventário estático com erro de sintaxe YAML: linha" "I-06 mensagem YAML"

reset_home
write_many_static 26
write_many_tailscale 25
POCKET_TAILSCALE_MODE=online
export POCKET_TAILSCALE_MODE
RC=$(run_capture_rc run dummy_playbook.yml --source merged)
[ "${RC}" = "1" ] || fail "I-07 execução com >50 hosts deveria retornar 1"
assert_file_contains "${ERR_FILE}" "Limite de 50 hosts excedido (51 encontrados). Use --limit para restringir." "I-07 mensagem limite"
[ ! -s "${CALL_LOG}" ] || fail "I-07 ansible-playbook não deveria ser chamado"

reset_home
write_static_invalid_host
RC=$(run_capture_rc inventory --source static)
[ "${RC}" = "0" ] || fail "I-08 host inválido deveria rejeitar só o host"
assert_file_contains "${ERR_FILE}" "Hostname inválido rejeitado: host; rm -rf /" "I-08 aviso hostname"
assert_file_contains "${OUT_FILE}" "good01" "I-08 host válido carregado"
assert_file_not_contains "${OUT_FILE}" "host; rm -rf /" "I-08 host inválido não deve aparecer"

reset_home
POCKET_TAILSCALE_MODE=injection
export POCKET_TAILSCALE_MODE
RC=$(run_capture_rc inventory refresh)
[ "${RC}" = "0" ] || fail "I-09 refresh com hostname malicioso deveria retornar 0"
assert_file_contains "${ERR_FILE}" "Hostname Tailscale sanitizado: host; rm -rf / -> hostrm-rf" "I-09 aviso sanitize"
assert_file_contains "${HOME_DIR}/.pocketcli/ansible/inventory/tailscale_generated.yml" "hostrm-rf:" "I-09 hostname sanitizado incluído"

reset_home
rm -f "${HOME_DIR}/.pocketcli/ansible/inventory/hosts.yml"
write_many_tailscale 1
POCKET_TAILSCALE_MODE=offline
export POCKET_TAILSCALE_MODE
RC=$(run_capture_rc inventory --source merged)
[ "${RC}" = "0" ] || fail "I-10 merged com cache deveria retornar 0"
assert_file_contains "${ERR_FILE}" "Usando inventário Tailscale em cache (tailscale offline)" "I-10 aviso cache"
assert_file_contains "${ERR_FILE}" "generated_at: 2026-05-11T09:10:00-0300" "I-10 timestamp cache"
assert_file_contains "${OUT_FILE}" "ts01" "I-10 host do cache exibido"

reset_home
rm -f "${HOME_DIR}/.pocketcli/ansible/inventory/hosts.yml" "${HOME_DIR}/.pocketcli/ansible/inventory/tailscale_generated.yml"
POCKET_TAILSCALE_MODE=offline
export POCKET_TAILSCALE_MODE
RC=$(run_capture_rc inventory --source merged)
[ "${RC}" = "2" ] || fail "I-11 sem fontes deveria retornar 2"
assert_file_contains "${ERR_FILE}" "Nenhuma fonte de inventário disponível" "I-11 mensagem sem fonte"
[ ! -f "${HOME_DIR}/.pocketcli/ansible/inventory/hosts.yml" ] || fail "I-11 não deveria criar hosts.yml silenciosamente"

printf 'PASS ansible inventory contract I-01..I-11\n'

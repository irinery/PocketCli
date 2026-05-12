#!/usr/bin/env sh
# PocketCli — Ansible playbook scaffold.
# POSIX sh only; this file is installed as ~/.pocketcli/lib/ansible_init.sh.

set -u

POCKET_DIR="${POCKET_DIR:-${POCKETCLI_DIR:-${HOME}/.pocketcli}}"
ANSIBLE_DIR="${ANSIBLE_DIR:-${POCKET_DIR}/ansible}"
PLAYBOOKS_DIR="${PLAYBOOKS_DIR:-${ANSIBLE_DIR}/playbooks}"

usage() {
    cat <<'EOF'
Usage:
  pocket ansible init <nome> [--category <categoria>] [--force]
EOF
}

fail() {
    printf '%s\n' "$1" >&2
    exit "${2:-1}"
}

warn() {
    printf '%s\n' "$1" >&2
}

valid_category() {
    case "$1" in
        diagnostic|setup|deploy|maintenance|security|network|utility) return 0 ;;
        *) return 1 ;;
    esac
}

safe_modes_for_category() {
    case "$1" in
        diagnostic|security|network)
            printf 'check diff'
            ;;
        setup|deploy|maintenance|utility)
            printf 'check diff run'
            ;;
    esac
}

category_comment() {
    case "$1" in
        diagnostic)
            printf '# diagnostic: nunca altera estado dos hosts\n'
            ;;
        security)
            printf '# security: modo run requer autorização explícita\n'
            ;;
        network)
            printf '# network: modo run requer autorização explícita para evitar lock-out\n'
            ;;
        *)
            printf '# Remova "run" se este playbook não deve ser executado em modo real\n'
            ;;
    esac
}

validate_name() {
    NAME="$1"
    LEN=${#NAME}
    if [ "${LEN}" -gt 80 ]; then
        fail "Nome de playbook excede 80 caracteres." 1
    fi
    case "${NAME}" in
        ''|[!abcdefghijklmnopqrstuvwxyz0123456789]*|*[!abcdefghijklmnopqrstuvwxyz0123456789_-]*)
            fail "Nome de playbook inválido. Use apenas [a-z0-9_-]." 1
            ;;
    esac
}

now_date() {
    date '+%Y-%m-%d'
}

now_iso8601() {
    date '+%Y-%m-%dT%H:%M:%S%z'
}

default_author() {
    if [ -n "${USER:-}" ]; then
        printf '%s' "${USER}"
    else
        printf 'pocketcli'
    fi
}

ensure_playbooks_dir() {
    if ! mkdir -p "${PLAYBOOKS_DIR}" 2>/dev/null; then
        fail "Não foi possível criar ${PLAYBOOKS_DIR}" 1
    fi
}

write_scaffold() {
    DEST_FILE="$1"
    SLUG="$2"
    CATEGORY="$3"
    CREATED_DATE="$4"
    CREATED_TS="$5"
    AUTHOR="$6"
    SAFE_MODES=$(safe_modes_for_category "${CATEGORY}")
    TMP_FILE=$(mktemp "${TMPDIR:-/tmp}/pocket-ansible-init.XXXXXX") || exit 1

    {
        printf '# ============================================================\n'
        printf '# PocketCLI Playbook — gerado por: pocket ansible init\n'
        printf '# Criado em: %s\n' "${CREATED_TS}"
        printf '# ============================================================\n\n'
        printf '# ---\n'
        printf '# BLOCO DE METADADOS — obrigatório, não remover\n'
        printf '# ---\n'
        printf -- '- name: pocket_meta\n'
        printf '  hosts: localhost\n'
        printf '  gather_facts: false\n'
        printf '  vars:\n'
        printf '    pocket_meta:\n'
        printf '      name: "%s"\n' "${SLUG}"
        printf '      description: "Descreva o que este playbook faz (máx 120 chars)"\n'
        printf '      author: "%s"\n' "${AUTHOR}"
        printf '      version: "1.0.0"\n'
        printf '      category: "%s"\n' "${CATEGORY}"
        printf '      safe_modes:\n'
        category_comment "${CATEGORY}" | sed 's/^/        /'
        for MODE in ${SAFE_MODES}; do
            printf '        - %s\n' "${MODE}"
        done
        printf '      inventory_source: merged\n'
        printf '      tags: []\n'
        printf '      min_ansible_version: "2.14"\n'
        printf '      created_at: "%s"\n' "${CREATED_DATE}"
        printf '      updated_at: "%s"\n' "${CREATED_DATE}"
        printf '  tasks: []\n\n'
        printf '# ---\n'
        printf '# PLAY PRINCIPAL\n'
        printf '# ---\n'
        printf -- '- name: "Descreva a execução principal"\n'
        printf '  hosts: all          # altere para o grupo correto do inventário\n'
        printf '  gather_facts: true  # defina false se não precisar de facts\n\n'
        printf '  vars:\n'
        printf '    # Variáveis do playbook\n'
        printf '    # Variáveis que podem ser sobrescritas via -e na linha de comando\n'
        printf '    # Formato: pocket_%s_<variavel>\n' "${SLUG}"
        printf '    # pocket_%s_porta: 8080\n' "${SLUG}"
        printf '    # pocket_%s_usuario: "deploy"\n' "${SLUG}"
        printf '    # pocket_dry_run: "{{ ansible_check_mode }}"\n\n'
        printf '  pre_tasks:\n'
        printf '    - name: Verificar pré-requisitos\n'
        printf '      ansible.builtin.assert:\n'
        printf '        that:\n'
        printf '          - ansible_os_family is defined\n'
        printf '        fail_msg: "Pré-requisito não atendido"\n'
        printf '      tags: [always]\n\n'
        printf '  tasks:\n'
        printf '    - name: Exemplo — exibir informações do host\n'
        printf '      ansible.builtin.debug:\n'
        printf '        msg: "Host: {{ inventory_hostname }} | OS: {{ ansible_distribution }}"\n'
        printf '      tags: [info]\n\n'
        printf '  post_tasks:\n'
        printf '    - name: Registrar conclusão\n'
        printf '      ansible.builtin.debug:\n'
        printf '        msg: "Playbook concluído em {{ inventory_hostname }}"\n'
        printf '      tags: [always]\n\n'
        printf '  handlers:\n'
        printf '    - name: restart service\n'
        printf '      listen: restart service\n'
        printf '      ansible.builtin.service:\n'
        printf '        name: "{{ service_name | default('\''undefined'\'') }}"\n'
        printf '        state: restarted\n'
    } > "${TMP_FILE}" || {
        rm -f "${TMP_FILE}"
        fail "Falha ao escrever scaffold temporário" 1
    }

    if ! mv "${TMP_FILE}" "${DEST_FILE}" 2>/dev/null; then
        rm -f "${TMP_FILE}"
        fail "Falha ao escrever ${DEST_FILE}" 1
    fi
}

init_playbook() {
    [ $# -ge 1 ] || {
        usage >&2
        exit 1
    }

    SLUG="$1"
    shift
    validate_name "${SLUG}"

    CATEGORY=""
    FORCE=0

    while [ $# -gt 0 ]; do
        case "$1" in
            --category)
                [ $# -ge 2 ] || fail "Uso inválido: --category requer uma categoria" 1
                CATEGORY="$2"
                valid_category "${CATEGORY}" || fail "Categoria inválida: ${CATEGORY}. Use: diagnostic, setup, deploy, maintenance, security, network, utility" 1
                shift 2
                ;;
            --force)
                FORCE=1
                shift
                ;;
            *)
                fail "Argumento inválido para init: $1" 1
                ;;
        esac
    done

    if [ -z "${CATEGORY}" ]; then
        CATEGORY="utility"
        warn "Categoria padrão 'utility' aplicada — considere especificar --category"
    fi

    ensure_playbooks_dir
    DEST_FILE="${PLAYBOOKS_DIR}/${SLUG}.yml"
    if [ -f "${DEST_FILE}" ] && [ "${FORCE}" -ne 1 ]; then
        fail "Playbook '${SLUG}' já existe. Use --force para sobrescrever." 1
    fi

    OVERWROTE=0
    [ -f "${DEST_FILE}" ] && OVERWROTE=1
    write_scaffold "${DEST_FILE}" "${SLUG}" "${CATEGORY}" "$(now_date)" "$(now_iso8601)" "$(default_author)"

    if [ "${OVERWROTE}" -eq 1 ]; then
        printf 'Playbook sobrescrito.\n'
    else
        printf 'Playbook criado: %s\n' "${DEST_FILE}"
    fi
}

main() {
    COMMAND="${1:-help}"
    case "${COMMAND}" in
        init)
            shift
            init_playbook "$@"
            ;;
        help|-h|--help)
            usage
            ;;
        *)
            init_playbook "$@"
            ;;
    esac
}

main "$@"

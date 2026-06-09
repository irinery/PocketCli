#!/usr/bin/env sh
# PocketCli — Ansible adapter.
# POSIX sh only; this file is installed as ~/.pocketcli/lib/ansible_adapter.sh.

set -u

POCKET_DIR="${POCKET_DIR:-${POCKETCLI_DIR:-${HOME}/.pocketcli}}"
ANSIBLE_DIR="${ANSIBLE_DIR:-${POCKET_DIR}/ansible}"
INVENTORY_DIR="${INVENTORY_DIR:-${ANSIBLE_DIR}/inventory}"
PLAYBOOKS_DIR="${PLAYBOOKS_DIR:-${ANSIBLE_DIR}/playbooks}"
ANSIBLE_LOG="${ANSIBLE_LOG:-${POCKET_DIR}/logs/ansible.log}"
ANSIBLE_TIMEOUT_SECONDS="${ANSIBLE_TIMEOUT_SECONDS:-300}"
ANSIBLE_TASK_TIMEOUT_SECONDS="${ANSIBLE_TASK_TIMEOUT_SECONDS:-60}"
ANSIBLE_MAX_OUTPUT_BYTES="${ANSIBLE_MAX_OUTPUT_BYTES:-524288}"
ANSIBLE_LOG_ROTATE_BYTES="${ANSIBLE_LOG_ROTATE_BYTES:-10485760}"
ANSIBLE_MAX_HOSTS="${ANSIBLE_MAX_HOSTS:-50}"

usage() {
    cat <<'EOF'
Usage:
  pocket ansible list
  pocket ansible init <nome> [--category <categoria>] [--force]
  pocket ansible run <playbook> [--run] [--diff] [--source <src>] [--inventory <file>] [--limit <pattern>]
  pocket ansible inventory [--source static|tailscale|merged]
  pocket ansible inventory refresh
  pocket ansible log [--last N]
EOF
}

fail() {
    printf '%s\n' "$1" >&2
    exit "${2:-1}"
}

warn() {
    printf 'warning: %s\n' "$1" >&2
}

ansible_install_hint() {
    if [ -f /etc/alpine-release ]; then
        printf 'apk add ansible'
    elif command -v apt-get >/dev/null 2>&1; then
        printf 'sudo apt-get install -y ansible'
    elif command -v dnf >/dev/null 2>&1; then
        printf 'sudo dnf install -y ansible'
    elif command -v brew >/dev/null 2>&1; then
        printf 'brew install ansible'
    else
        printf 'instale o pacote ansible pelo gerenciador do sistema'
    fi
}

require_ansible() {
    if ! command -v ansible-playbook >/dev/null 2>&1; then
        fail "ansible-playbook não encontrado. Instale via: $(ansible_install_hint)" 127
    fi
    if ! command -v ansible >/dev/null 2>&1; then
        fail "ansible não encontrado. Instale via: $(ansible_install_hint)" 127
    fi
}

require_ansible_tree() {
    if [ ! -d "${ANSIBLE_DIR}" ]; then
        fail "Diretório ~/.pocketcli/ansible não encontrado. Execute: pocket ansible init" 1
    fi
    if [ ! -d "${INVENTORY_DIR}" ] || [ ! -d "${PLAYBOOKS_DIR}" ]; then
        fail "Estrutura de ansible incompleta em ${ANSIBLE_DIR}. Esperado: inventory/ e playbooks/" 1
    fi
}

validate_playbook_name() {
    PLAYBOOK_NAME="$1"
    case "${PLAYBOOK_NAME}" in
        *.yml) ;;
        *) fail "Nome de playbook inválido" 4 ;;
    esac

    PLAYBOOK_BASE=${PLAYBOOK_NAME%.yml}
    PLAYBOOK_LEN=${#PLAYBOOK_BASE}
    if [ "${PLAYBOOK_LEN}" -lt 1 ] || [ "${PLAYBOOK_LEN}" -gt 80 ]; then
        fail "Nome de playbook inválido" 4
    fi
    case "${PLAYBOOK_BASE}" in
        *[!ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-]*)
            fail "Nome de playbook inválido" 4
            ;;
    esac
}

validate_registry_slug() {
    PLAYBOOK_SLUG="$1"
    PLAYBOOK_LEN=${#PLAYBOOK_SLUG}
    if [ "${PLAYBOOK_LEN}" -lt 1 ] || [ "${PLAYBOOK_LEN}" -gt 80 ]; then
        fail "Nome de playbook inválido" 4
    fi
    case "${PLAYBOOK_SLUG}" in
        *[!abcdefghijklmnopqrstuvwxyz0123456789_-]*)
            fail "Nome de playbook inválido" 4
            ;;
    esac
}

now_epoch() {
    date '+%s'
}

now_iso8601() {
    date '+%Y-%m-%dT%H:%M:%S%z'
}

file_size() {
    wc -c < "$1" | tr -d '[:space:]'
}

truncate_file() {
    SRC_FILE="$1"
    DEST_FILE="$2"
    MAX_BYTES="$3"
    SIZE=$(file_size "${SRC_FILE}")

    if [ "${SIZE}" -gt "${MAX_BYTES}" ]; then
        if command -v head >/dev/null 2>&1; then
            head -c "${MAX_BYTES}" "${SRC_FILE}" > "${DEST_FILE}" 2>/dev/null \
                || dd if="${SRC_FILE}" of="${DEST_FILE}" bs="${MAX_BYTES}" count=1 2>/dev/null
        else
            dd if="${SRC_FILE}" of="${DEST_FILE}" bs="${MAX_BYTES}" count=1 2>/dev/null
        fi
        printf '\n[TRUNCADO]\n' >> "${DEST_FILE}"
    else
        cat "${SRC_FILE}" > "${DEST_FILE}"
    fi
}

first_bytes_file() {
    SRC_FILE="$1"
    MAX_BYTES="$2"
    DEST_FILE="$3"

    if command -v head >/dev/null 2>&1; then
        head -c "${MAX_BYTES}" "${SRC_FILE}" > "${DEST_FILE}" 2>/dev/null \
            || dd if="${SRC_FILE}" of="${DEST_FILE}" bs="${MAX_BYTES}" count=1 2>/dev/null
    else
        dd if="${SRC_FILE}" of="${DEST_FILE}" bs="${MAX_BYTES}" count=1 2>/dev/null
    fi
}

json_escape_file() {
    awk '
        BEGIN { first = 1 }
        {
            gsub(/\\/, "\\\\")
            gsub(/"/, "\\\"")
            gsub(/\t/, "\\t")
            gsub(/\r/, "\\r")
            if (first == 0) {
                printf "\\n"
            }
            printf "%s", $0
            first = 0
        }
    ' "$1"
}

count_inventory_hosts() {
    INVENTORY_FILE="$1"
    awk '
        /^[[:space:]]*$/ { next }
        /^[[:space:]]*#/ { next }
        /^\[/ { next }
        /^[A-Za-z0-9_.-]+([[:space:]]|$)/ { count++; next }
        /^[[:space:]]+[A-Za-z0-9_.-]+:[[:space:]]*$/ {
            line = $0
            sub(/^[[:space:]]+/, "", line)
            sub(/:.*/, "", line)
            if (line != "hosts" && line != "vars" && line != "children") {
                count++
            }
            next
        }
        END { print count + 0 }
    ' "${INVENTORY_FILE}" 2>/dev/null || printf '0\n'
}

rotate_log_if_needed() {
    [ -f "${ANSIBLE_LOG}" ] || return 0

    SIZE=$(file_size "${ANSIBLE_LOG}")
    if [ "${SIZE}" -gt "${ANSIBLE_LOG_ROTATE_BYTES}" ]; then
        mv "${ANSIBLE_LOG}" "${ANSIBLE_LOG}.1" 2>/dev/null \
            || warn "não foi possível rotacionar ${ANSIBLE_LOG}"
    fi
}

write_log_entry() {
    RUN_ID="$1"
    TIMESTAMP="$2"
    PLAYBOOK="$3"
    MODE="$4"
    INVENTORY_SOURCE="$5"
    RESULT="$6"
    EXIT_CODE="$7"
    DURATION="$8"
    ANSIBLE_VERSION="$9"
    HOSTS_TARGETED="${10}"
    STDERR_FILE="${11}"
    STDOUT_FILE="${12}"

    LOG_DIR=$(dirname "${ANSIBLE_LOG}")
    if ! mkdir -p "${LOG_DIR}" 2>/dev/null; then
        warn "não foi possível criar diretório de log: ${LOG_DIR}"
        return 0
    fi

    rotate_log_if_needed

    STDERR_EXCERPT_FILE=$(mktemp "${TMPDIR:-/tmp}/pocket-ansible-stderr.XXXXXX") || return 0
    first_bytes_file "${STDERR_FILE}" 500 "${STDERR_EXCERPT_FILE}"
    STDERR_JSON=$(json_escape_file "${STDERR_EXCERPT_FILE}")
    STDOUT_JSON=$(json_escape_file "${STDOUT_FILE}")
    rm -f "${STDERR_EXCERPT_FILE}"

    if ! {
        printf '{'
        printf '"run_id":"%s",' "${RUN_ID}"
        printf '"timestamp":"%s",' "${TIMESTAMP}"
        printf '"playbook":"%s",' "${PLAYBOOK}"
        printf '"mode":"%s",' "${MODE}"
        printf '"inventory_source":"%s",' "${INVENTORY_SOURCE}"
        printf '"result":"%s",' "${RESULT}"
        printf '"exit_code":%s,' "${EXIT_CODE}"
        printf '"duration_seconds":%s,' "${DURATION}"
        printf '"ansible_version":"%s",' "${ANSIBLE_VERSION}"
        printf '"hosts_targeted":%s,' "${HOSTS_TARGETED}"
        printf '"stderr_excerpt":"%s",' "${STDERR_JSON}"
        printf '"stdout_excerpt":"%s"' "${STDOUT_JSON}"
        printf '}\n'
    } >> "${ANSIBLE_LOG}" 2>/dev/null; then
        warn "falha ao gravar log ansible em ${ANSIBLE_LOG}"
    fi
}

run_wiki_hook() {
    RUN_ID="$1"
    WIKI_HOOK="${POCKET_DIR}/lib/ansible_wiki_hook.sh"
    [ -f "${WIKI_HOOK}" ] || return 0
    if ! sh "${WIKI_HOOK}" generate "${RUN_ID}"; then
        warn "falha ao gerar entrada PocketWiki para ${RUN_ID}"
    fi
}

run_with_timeout() {
    TIMEOUT_SECONDS="$1"
    shift

    if command -v timeout >/dev/null 2>&1; then
        timeout "${TIMEOUT_SECONDS}" "$@"
        return $?
    fi

    TIMEOUT_MARKER=$(mktemp "${TMPDIR:-/tmp}/pocket-ansible-timeout.XXXXXX") || return 1
    rm -f "${TIMEOUT_MARKER}"

    "$@" &
    CHILD_PID=$!
    (
        sleep "${TIMEOUT_SECONDS}"
        if kill -0 "${CHILD_PID}" 2>/dev/null; then
            : > "${TIMEOUT_MARKER}"
            kill "${CHILD_PID}" 2>/dev/null || true
        fi
    ) &
    GUARD_PID=$!

    wait "${CHILD_PID}" 2>/dev/null
    CHILD_RC=$?
    kill "${GUARD_PID}" 2>/dev/null || true
    wait "${GUARD_PID}" 2>/dev/null || true

    if [ -f "${TIMEOUT_MARKER}" ]; then
        rm -f "${TIMEOUT_MARKER}"
        return 124
    fi

    rm -f "${TIMEOUT_MARKER}"
    return "${CHILD_RC}"
}

run_playbook() {
    [ $# -ge 1 ] || fail "Usage: pocket ansible run <playbook.yml> [--run] [--diff] [--inventory <file>]" 1

    PLAYBOOK="$1"
    PLAYBOOK_DISPLAY="${POCKET_ANSIBLE_DISPLAY_NAME:-${PLAYBOOK}}"
    shift
    validate_playbook_name "${PLAYBOOK}"

    MODE="check"
    RUN_FLAG=0
    DIFF_FLAG=0
    CUSTOM_INVENTORY=0
    INVENTORY_FILE="${ANSIBLE_INVENTORY:-}"
    INVENTORY_SOURCE="${ANSIBLE_INVENTORY_SOURCE:-}"
    LIMIT_VALUE=""

    while [ $# -gt 0 ]; do
        case "$1" in
            --run)
                RUN_FLAG=1
                shift
                ;;
            --diff)
                DIFF_FLAG=1
                shift
                ;;
            --inventory)
                [ $# -ge 2 ] || fail "Uso inválido: --inventory requer um arquivo" 1
                INVENTORY_FILE="$2"
                CUSTOM_INVENTORY=1
                shift 2
                ;;
            --source)
                [ $# -ge 2 ] || fail "Uso inválido: --source requer static, tailscale ou merged" 1
                case "$2" in
                    static|tailscale|merged) INVENTORY_SOURCE="$2" ;;
                    *) fail "Origem de inventário inválida: $2" 1 ;;
                esac
                shift 2
                ;;
            --limit)
                [ $# -ge 2 ] || fail "Uso inválido: --limit requer um padrão" 1
                LIMIT_VALUE="$2"
                shift 2
                ;;
            *)
                fail "Argumento ansible inválido: $1" 1
                ;;
        esac
    done

    if [ "${RUN_FLAG}" -eq 1 ]; then
        MODE="run"
    elif [ "${DIFF_FLAG}" -eq 1 ]; then
        MODE="diff"
    fi

    require_ansible
    require_ansible_tree

    PLAYBOOK_PATH="${PLAYBOOKS_DIR}/${PLAYBOOK}"
    [ -f "${PLAYBOOK_PATH}" ] || fail "Playbook não encontrado: ${PLAYBOOK}" 3

    if [ "${CUSTOM_INVENTORY}" -eq 0 ] && [ -z "${INVENTORY_FILE}" ]; then
        INVENTORY_SCRIPT="${POCKET_DIR}/lib/ansible_inventory.sh"
        [ -f "${INVENTORY_SCRIPT}" ] || fail "Script de inventário não encontrado: ${INVENTORY_SCRIPT}" 1
        if [ -n "${INVENTORY_SOURCE}" ] && [ -n "${LIMIT_VALUE}" ]; then
            RESOLVED_INVENTORY=$(sh "${INVENTORY_SCRIPT}" resolve --source "${INVENTORY_SOURCE}" --limit "${LIMIT_VALUE}")
            RESOLVE_RC=$?
        elif [ -n "${INVENTORY_SOURCE}" ]; then
            RESOLVED_INVENTORY=$(sh "${INVENTORY_SCRIPT}" resolve --source "${INVENTORY_SOURCE}")
            RESOLVE_RC=$?
        elif [ -n "${LIMIT_VALUE}" ]; then
            RESOLVED_INVENTORY=$(sh "${INVENTORY_SCRIPT}" resolve --limit "${LIMIT_VALUE}")
            RESOLVE_RC=$?
        else
            RESOLVED_INVENTORY=$(sh "${INVENTORY_SCRIPT}" resolve)
            RESOLVE_RC=$?
        fi
        [ "${RESOLVE_RC}" -eq 0 ] || exit "${RESOLVE_RC}"
        INVENTORY_FILE=$(printf '%s\n' "${RESOLVED_INVENTORY}" | sed -n 's/^inventory_path=//p' | sed -n '1p')
        INVENTORY_SOURCE=$(printf '%s\n' "${RESOLVED_INVENTORY}" | sed -n 's/^inventory_source=//p' | sed -n '1p')
        HOSTS_TARGETED=$(printf '%s\n' "${RESOLVED_INVENTORY}" | sed -n 's/^hosts_count=//p' | sed -n '1p')
    else
        [ -n "${INVENTORY_FILE}" ] || INVENTORY_FILE="${INVENTORY_DIR}/hosts.yml"
        [ -n "${INVENTORY_SOURCE}" ] || INVENTORY_SOURCE="custom"
        HOSTS_TARGETED=$(count_inventory_hosts "${INVENTORY_FILE}")
        if [ -z "${LIMIT_VALUE}" ] && [ "${HOSTS_TARGETED}" -gt "${ANSIBLE_MAX_HOSTS}" ]; then
            fail "Limite de 50 hosts excedido (${HOSTS_TARGETED} encontrados). Use --limit para restringir." 1
        fi
    fi

    [ -f "${INVENTORY_FILE}" ] || fail "Inventário não encontrado: ${INVENTORY_FILE}" 3

    STDOUT_RAW=$(mktemp "${TMPDIR:-/tmp}/pocket-ansible-stdout.XXXXXX") || exit 1
    STDERR_RAW=$(mktemp "${TMPDIR:-/tmp}/pocket-ansible-stderr.XXXXXX") || exit 1
    STDOUT_TRUNC=$(mktemp "${TMPDIR:-/tmp}/pocket-ansible-stdout-trunc.XXXXXX") || exit 1
    STDERR_TRUNC=$(mktemp "${TMPDIR:-/tmp}/pocket-ansible-stderr-trunc.XXXXXX") || exit 1
    trap 'rm -f "${STDOUT_RAW:-}" "${STDERR_RAW:-}" "${STDOUT_TRUNC:-}" "${STDERR_TRUNC:-}"' EXIT INT TERM HUP

    ANSIBLE_VERSION=$(ansible --version 2>/dev/null | sed -n '1p')
    TIMESTAMP=$(now_iso8601)
    RUN_ID="${TIMESTAMP}_${PLAYBOOK%.yml}"

    set -- ansible-playbook -i "${INVENTORY_FILE}"
    if [ -n "${LIMIT_VALUE}" ]; then
        set -- "$@" --limit "${LIMIT_VALUE}"
    fi
    case "${MODE}:${DIFF_FLAG}" in
        check:*) set -- "$@" --check ;;
        diff:*) set -- "$@" --check --diff ;;
        run:1) set -- "$@" --diff ;;
    esac
    set -- "$@" "${PLAYBOOK_PATH}"

    START_TS=$(now_epoch)
    ANSIBLE_TASK_TIMEOUT="${ANSIBLE_TASK_TIMEOUT:-${ANSIBLE_TASK_TIMEOUT_SECONDS}}"
    export ANSIBLE_TASK_TIMEOUT
    run_with_timeout "${ANSIBLE_TIMEOUT_SECONDS}" "$@" > "${STDOUT_RAW}" 2> "${STDERR_RAW}"
    ANSIBLE_RC=$?
    END_TS=$(now_epoch)
    DURATION=$((END_TS - START_TS))

    truncate_file "${STDOUT_RAW}" "${STDOUT_TRUNC}" "${ANSIBLE_MAX_OUTPUT_BYTES}"
    truncate_file "${STDERR_RAW}" "${STDERR_TRUNC}" "${ANSIBLE_MAX_OUTPUT_BYTES}"

    if [ "${MODE}" = "run" ]; then
        printf '[EXECUÇÃO REAL] %s\n' "${PLAYBOOK_DISPLAY}"
    else
        printf '[DRY-RUN] %s\n' "${PLAYBOOK_DISPLAY}"
    fi
    cat "${STDOUT_TRUNC}"
    cat "${STDERR_TRUNC}" >&2

    if [ "${ANSIBLE_RC}" -eq 124 ]; then
        printf 'Execução encerrada por timeout (%ss)\n' "${ANSIBLE_TIMEOUT_SECONDS}" >&2
        write_log_entry "${RUN_ID}" "${TIMESTAMP}" "${PLAYBOOK}" "${MODE}" "${INVENTORY_SOURCE}" "aborted" 124 "${DURATION}" "${ANSIBLE_VERSION}" "${HOSTS_TARGETED}" "${STDERR_TRUNC}" "${STDOUT_TRUNC}"
        run_wiki_hook "${RUN_ID}"
        exit 124
    fi

    if [ "${ANSIBLE_RC}" -eq 0 ]; then
        RESULT="success"
        ADAPTER_RC=0
    else
        RESULT="failure"
        ADAPTER_RC=2
    fi

    write_log_entry "${RUN_ID}" "${TIMESTAMP}" "${PLAYBOOK}" "${MODE}" "${INVENTORY_SOURCE}" "${RESULT}" "${ANSIBLE_RC}" "${DURATION}" "${ANSIBLE_VERSION}" "${HOSTS_TARGETED}" "${STDERR_TRUNC}" "${STDOUT_TRUNC}"
    run_wiki_hook "${RUN_ID}"
    exit "${ADAPTER_RC}"
}

main() {
    COMMAND="${1:-help}"
    case "${COMMAND}" in
        help|-h|--help)
            usage
            ;;
        run)
            shift
            case "${1:-}" in
                *.yml) ;;
                *)
                    validate_registry_slug "${1:-}"
                    REGISTRY_SCRIPT="${POCKET_DIR}/lib/ansible_registry.sh"
                    [ -f "${REGISTRY_SCRIPT}" ] || fail "Script registry não encontrado: ${REGISTRY_SCRIPT}" 1
                    exec sh "${REGISTRY_SCRIPT}" run "$@"
                    ;;
            esac
            run_playbook "$@"
            ;;
        inventory)
            shift
            INVENTORY_SCRIPT="${POCKET_DIR}/lib/ansible_inventory.sh"
            [ -f "${INVENTORY_SCRIPT}" ] || fail "Script de inventário não encontrado: ${INVENTORY_SCRIPT}" 1
            exec sh "${INVENTORY_SCRIPT}" inventory "$@"
            ;;
        list)
            shift
            REGISTRY_SCRIPT="${POCKET_DIR}/lib/ansible_registry.sh"
            [ -f "${REGISTRY_SCRIPT}" ] || fail "Script registry não encontrado: ${REGISTRY_SCRIPT}" 1
            exec sh "${REGISTRY_SCRIPT}" list "$@"
            ;;
        log)
            shift
            WIKI_HOOK="${POCKET_DIR}/lib/ansible_wiki_hook.sh"
            [ -f "${WIKI_HOOK}" ] || fail "Script wiki hook não encontrado: ${WIKI_HOOK}" 1
            exec sh "${WIKI_HOOK}" log "$@"
            ;;
        init)
            shift
            INIT_SCRIPT="${POCKET_DIR}/lib/ansible_init.sh"
            [ -f "${INIT_SCRIPT}" ] || fail "Script init não encontrado: ${INIT_SCRIPT}" 1
            exec sh "${INIT_SCRIPT}" init "$@"
            ;;
        *)
            require_ansible
            require_ansible_tree
            usage >&2
            exit 1
            ;;
    esac
}

main "$@"

#!/usr/bin/env sh
# PocketCli — Ansible PocketWiki hook.
# POSIX sh only; this file is installed as ~/.pocketcli/lib/ansible_wiki_hook.sh.

set -u

POCKET_DIR="${POCKET_DIR:-${POCKETCLI_DIR:-${HOME}/.pocketcli}}"
ANSIBLE_LOG="${ANSIBLE_LOG:-${POCKET_DIR}/logs/ansible.log}"
WIKI_ANSIBLE_DIR="${WIKI_ANSIBLE_DIR:-${POCKET_DIR}/wiki/ansible}"
WIKI_INDEX="${WIKI_ANSIBLE_DIR}/_index.md"
WIKI_LOCK_DIR="${WIKI_ANSIBLE_DIR}/_index.lock"
WIKI_INDEX_LIMIT="${WIKI_INDEX_LIMIT:-500}"
WIKI_LOCK_TIMEOUT_SECONDS="${WIKI_LOCK_TIMEOUT_SECONDS:-10}"

usage() {
    cat <<'EOF'
Usage:
  pocket ansible wiki generate <run_id>
  pocket ansible log [--last N]
EOF
}

warn() {
    printf '%s\n' "$1" >&2
}

fail() {
    printf '%s\n' "$1" >&2
    exit "${2:-1}"
}

now_iso8601() {
    date '+%Y-%m-%dT%H:%M:%S%z'
}

safe_run_id() {
    CLEAN=$(printf '%s' "$1" | tr -cd 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.:+-')
    [ -n "${CLEAN}" ] || CLEAN="unknown_run"
    printf '%s' "${CLEAN}"
}

json_string_field() {
    FIELD="$1"
    LINE="$2"
    VALUE=$(printf '%s\n' "${LINE}" | sed -n "s/.*\"${FIELD}\":\"\([^\"]*\)\".*/\1/p" | sed -n '1p')
    [ -n "${VALUE}" ] || VALUE="N/A"
    printf '%s' "${VALUE}"
}

json_number_field() {
    FIELD="$1"
    LINE="$2"
    VALUE=$(printf '%s\n' "${LINE}" | sed -n "s/.*\"${FIELD}\":\([0-9][0-9]*\).*/\1/p" | sed -n '1p')
    [ -n "${VALUE}" ] || VALUE="N/A"
    printf '%s' "${VALUE}"
}

sanitize_text() {
    sed 's/\\u001b\[[0-9;?]*[A-Za-z]//g' \
        | awk '{ gsub(/\033\[[0-9;?]*[ -\/]*[@-~]/, ""); print }' \
        | LC_ALL=C tr -d '\000-\010\013\014\016-\037\177'
}

clean_value() {
    printf '%s' "$1" | sanitize_text | sed 's/|/\//g'
}

display_date() {
    TS="$1"
    if [ "${TS}" = "N/A" ]; then
        printf 'N/A'
        return 0
    fi
    printf '%s' "${TS}" | sed 's/T/ /;s/[+-][0-9][0-9][0-9][0-9]$//'
}

result_label() {
    case "$1" in
        success) printf '✅ sucesso' ;;
        failure) printf '❌ falha' ;;
        aborted) printf '⚠️ abortado' ;;
        skipped) printf '⏭️ skipped' ;;
        *) printf 'N/A' ;;
    esac
}

result_heading() {
    case "$1" in
        success) printf '✅ SUCESSO' ;;
        failure) printf '❌ FALHA' ;;
        aborted) printf '⚠️ ABORTADO' ;;
        skipped) printf '⏭️ SKIPPED' ;;
        *) printf '⚠️ DADOS INDISPONÍVEIS' ;;
    esac
}

result_detail() {
    case "$1" in
        success) printf 'Execução concluída com sucesso.' ;;
        failure) printf 'Execução concluída com falha.' ;;
        aborted) printf '⚠️ ABORTADO POR TIMEOUT (%ss)' "${ANSIBLE_TIMEOUT_SECONDS:-300}" ;;
        skipped) printf 'Execução marcada como skipped.' ;;
        *) printf '⚠️ Dados de log indisponíveis para este run_id' ;;
    esac
}

ensure_wiki_dir() {
    if ! mkdir -p "${WIKI_ANSIBLE_DIR}" 2>/dev/null; then
        fail "Não foi possível criar ${WIKI_ANSIBLE_DIR}" 1
    fi
}

load_log_line() {
    RUN_ID="$1"
    if [ ! -f "${ANSIBLE_LOG}" ]; then
        return 1
    fi
    grep -F "\"run_id\":\"${RUN_ID}\"" "${ANSIBLE_LOG}" 2>/dev/null | tail -n 1
}

write_wiki_file() {
    WIKI_FILE="$1"
    RUN_ID="$2"
    LOG_AVAILABLE="$3"
    TIMESTAMP="$4"
    PLAYBOOK="$5"
    MODE="$6"
    RESULT="$7"
    EXIT_CODE="$8"
    DURATION="$9"
    ANSIBLE_VERSION="${10}"
    HOSTS_TARGETED="${11}"
    INVENTORY_SOURCE="${12}"
    STDERR_TEXT="${13}"

    if [ -f "${WIKI_FILE}" ]; then
        return 0
    fi

    TMP_FILE=$(mktemp "${TMPDIR:-/tmp}/pocket-wiki-entry.XXXXXX") || exit 1
    {
        printf '# %s %s — %s\n\n' "$(result_heading "${RESULT}")" "$(clean_value "${PLAYBOOK}")" "$(display_date "${TIMESTAMP}")"
        printf '## Resumo\n\n'
        printf '| Campo | Valor |\n'
        printf '|---|---|\n'
        printf '| Run ID | %s |\n' "$(clean_value "${RUN_ID}")"
        printf '| Playbook | %s |\n' "$(clean_value "${PLAYBOOK}")"
        printf '| Modo | %s |\n' "$(clean_value "${MODE}")"
        printf '| Resultado | %s |\n' "$(clean_value "${RESULT}")"
        printf '| Exit code | %s |\n' "$(clean_value "${EXIT_CODE}")"
        printf '| Duração | %ss |\n' "$(clean_value "${DURATION}")"
        printf '| Hosts alvo | %s |\n' "$(clean_value "${HOSTS_TARGETED}")"
        printf '| Ansible | %s |\n' "$(clean_value "${ANSIBLE_VERSION}")"
        printf '| Timestamp | %s |\n\n' "$(clean_value "${TIMESTAMP}")"

        printf '## Playbook\n\n'
        printf '%s%s%s\n\n' '`' "$(clean_value "${PLAYBOOK}")" '`'

        printf '## Inventário\n\n'
        printf 'Fonte: %s%s%s\n\n' '`' "$(clean_value "${INVENTORY_SOURCE}")" '`'

        printf '## Resultado\n\n'
        printf '%s\n\n' "$(result_detail "${RESULT}")"

        printf '## Duração\n\n'
        printf '%ss\n\n' "$(clean_value "${DURATION}")"

        printf '## Modo\n\n'
        printf '%s%s%s\n\n' '`' "$(clean_value "${MODE}")" '`'

        printf '## Timestamp\n\n'
        printf '%s%s%s\n\n' '`' "$(clean_value "${TIMESTAMP}")" '`'

        if [ "${LOG_AVAILABLE}" -ne 1 ]; then
            printf '## Notas\n\n'
            printf '> ⚠️ Dados de log indisponíveis para este run_id\n\n'
        elif [ "${MODE}" = "check" ]; then
            printf '## Notas\n\n'
            printf '> ⚠️ Esta execução foi em modo **check** — nenhuma alteração foi aplicada aos hosts.\n\n'
        fi

        case "${RESULT}" in
            failure|aborted)
                printf '## Erros\n\n'
                printf '```text\n'
                printf '%s\n' "${STDERR_TEXT}"
                printf '```\n'
                ;;
        esac
    } > "${TMP_FILE}" || {
        rm -f "${TMP_FILE}"
        fail "Não foi possível escrever entrada wiki temporária" 1
    }

    if ! mv "${TMP_FILE}" "${WIKI_FILE}" 2>/dev/null; then
        rm -f "${TMP_FILE}"
        fail "Não foi possível escrever ${WIKI_FILE}" 1
    fi
}

index_header() {
    printf '# Ansible Runs — PocketWiki Index\n\n'
    printf '_Última atualização: %s_\n\n' "$(now_iso8601)"
    printf '| Data | Playbook | Resultado | Modo | Duração | Arquivo |\n'
    printf '|---|---|---|---|---|---|\n'
}

update_index_locked() {
    RUN_ID="$1"
    TIMESTAMP="$2"
    PLAYBOOK="$3"
    RESULT="$4"
    MODE="$5"
    DURATION="$6"

    TMP_INDEX=$(mktemp "${TMPDIR:-/tmp}/pocket-wiki-index.XXXXXX") || exit 1
    DATA=$(display_date "${TIMESTAMP}")
    RESULT_TEXT=$(result_label "${RESULT}")
    ENTRY=$(printf '| %s | %s | %s | %s | %ss | [ver](./%s.md) |' \
        "$(clean_value "${DATA}")" \
        "$(clean_value "${PLAYBOOK}")" \
        "${RESULT_TEXT}" \
        "$(clean_value "${MODE}")" \
        "$(clean_value "${DURATION}")" \
        "$(clean_value "${RUN_ID}")")

    {
        index_header
        printf '%s\n' "${ENTRY}"
        if [ -f "${WIKI_INDEX}" ]; then
            awk -v limit="$((WIKI_INDEX_LIMIT - 1))" -v run_id="${RUN_ID}" '
                BEGIN { count = 0 }
                /^\| [^|]+ \|/ && $0 !~ /^\| Data / && $0 !~ /^\|---/ {
                    if (index($0, "./" run_id ".md") > 0) {
                        next
                    }
                    if (count < limit) {
                        print
                        count++
                    }
                }
            ' "${WIKI_INDEX}"
        fi
    } > "${TMP_INDEX}" || {
        rm -f "${TMP_INDEX}"
        return 1
    }

    mv "${TMP_INDEX}" "${WIKI_INDEX}"
}

update_index_with_lock() {
    RUN_ID="$1"
    TIMESTAMP="$2"
    PLAYBOOK="$3"
    RESULT="$4"
    MODE="$5"
    DURATION="$6"

    WAITED=0
    while ! mkdir "${WIKI_LOCK_DIR}" 2>/dev/null; do
        if [ "${WAITED}" -ge "${WIKI_LOCK_TIMEOUT_SECONDS}" ]; then
            warn "Aviso: timeout aguardando lock do índice PocketWiki; entrada wiki criada sem atualizar índice"
            return 0
        fi
        sleep 1
        WAITED=$((WAITED + 1))
    done

    trap 'rmdir "${WIKI_LOCK_DIR}" 2>/dev/null || true' EXIT INT TERM HUP
    update_index_locked "${RUN_ID}" "${TIMESTAMP}" "${PLAYBOOK}" "${RESULT}" "${MODE}" "${DURATION}" \
        || warn "Aviso: falha ao atualizar índice PocketWiki"
    rmdir "${WIKI_LOCK_DIR}" 2>/dev/null || true
    trap - EXIT INT TERM HUP
}

generate_entry() {
    [ $# -eq 1 ] || fail "Uso inválido: pocket ansible wiki generate <run_id>" 1
    RUN_ID=$(safe_run_id "$1")
    ensure_wiki_dir

    LOG_AVAILABLE=1
    if LOG_LINE=$(load_log_line "${RUN_ID}"); then
        :
    else
        LOG_AVAILABLE=0
        LOG_LINE=""
    fi

    TIMESTAMP=$(json_string_field timestamp "${LOG_LINE}")
    PLAYBOOK=$(json_string_field playbook "${LOG_LINE}")
    MODE=$(json_string_field mode "${LOG_LINE}")
    INVENTORY_SOURCE=$(json_string_field inventory_source "${LOG_LINE}")
    RESULT=$(json_string_field result "${LOG_LINE}")
    EXIT_CODE=$(json_number_field exit_code "${LOG_LINE}")
    DURATION=$(json_number_field duration_seconds "${LOG_LINE}")
    ANSIBLE_VERSION=$(json_string_field ansible_version "${LOG_LINE}")
    HOSTS_TARGETED=$(json_number_field hosts_targeted "${LOG_LINE}")
    STDERR_EXCERPT=$(json_string_field stderr_excerpt "${LOG_LINE}" | sanitize_text)

    if [ "${LOG_AVAILABLE}" -ne 1 ]; then
        TIMESTAMP="N/A"
        PLAYBOOK="N/A"
        MODE="N/A"
        INVENTORY_SOURCE="N/A"
        RESULT="N/A"
        EXIT_CODE="N/A"
        DURATION="N/A"
        ANSIBLE_VERSION="N/A"
        HOSTS_TARGETED="N/A"
        STDERR_EXCERPT=""
    fi

    WIKI_FILE="${WIKI_ANSIBLE_DIR}/${RUN_ID}.md"
    write_wiki_file "${WIKI_FILE}" "${RUN_ID}" "${LOG_AVAILABLE}" "${TIMESTAMP}" "${PLAYBOOK}" "${MODE}" "${RESULT}" "${EXIT_CODE}" "${DURATION}" "${ANSIBLE_VERSION}" "${HOSTS_TARGETED}" "${INVENTORY_SOURCE}" "${STDERR_EXCERPT}"
    update_index_with_lock "${RUN_ID}" "${TIMESTAMP}" "${PLAYBOOK}" "${RESULT}" "${MODE}" "${DURATION}"
    printf 'PocketWiki: %s\n' "${WIKI_FILE}"
}

show_log() {
    LAST=10
    while [ $# -gt 0 ]; do
        case "$1" in
            --last)
                [ $# -ge 2 ] || fail "Uso inválido: --last requer um número" 1
                LAST="$2"
                case "${LAST}" in
                    ''|*[!0-9]*) fail "Uso inválido: --last requer um número" 1 ;;
                esac
                shift 2
                ;;
            *)
                fail "Argumento inválido para log: $1" 1
                ;;
        esac
    done

    if [ ! -f "${WIKI_INDEX}" ] || ! awk '/^\| [^|]+ \|/ && $0 !~ /^\| Data / && $0 !~ /^\|---/ { found = 1 } END { exit found == 1 ? 0 : 1 }' "${WIKI_INDEX}"; then
        printf 'Nenhuma execução registrada ainda.\n'
        return 0
    fi

    printf '%-18s %-24s %-14s %-8s %s\n' "data" "playbook" "resultado" "duração" "wiki"
    awk -v last="${LAST}" -F '|' '
        /^\| [^|]+ \|/ && $0 !~ /^\| Data / && $0 !~ /^\|---/ {
            if (count >= last) {
                next
            }
            data = $2
            playbook = $3
            result = $4
            duration = $6
            wiki = $7
            gsub(/^[[:space:]]+|[[:space:]]+$/, "", data)
            gsub(/^[[:space:]]+|[[:space:]]+$/, "", playbook)
            gsub(/^[[:space:]]+|[[:space:]]+$/, "", result)
            gsub(/^[[:space:]]+|[[:space:]]+$/, "", duration)
            gsub(/^[[:space:]]+|[[:space:]]+$/, "", wiki)
            printf "%-18s %-24s %-14s %-8s %s\n", data, playbook, result, duration, wiki
            count++
        }
    ' "${WIKI_INDEX}"
}

main() {
    COMMAND="${1:-help}"
    case "${COMMAND}" in
        generate)
            shift
            generate_entry "$@"
            ;;
        log)
            shift
            show_log "$@"
            ;;
        help|-h|--help)
            usage
            ;;
        *)
            fail "Subcomando wiki inválido: ${COMMAND}" 1
            ;;
    esac
}

main "$@"

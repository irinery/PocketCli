#!/usr/bin/env sh
# PocketCli — Ansible playbook registry.
# POSIX sh only; this file is installed as ~/.pocketcli/lib/ansible_registry.sh.

set -u

POCKET_DIR="${POCKET_DIR:-${POCKETCLI_DIR:-${HOME}/.pocketcli}}"
ANSIBLE_DIR="${ANSIBLE_DIR:-${POCKET_DIR}/ansible}"
INVENTORY_DIR="${INVENTORY_DIR:-${ANSIBLE_DIR}/inventory}"
PLAYBOOKS_DIR="${PLAYBOOKS_DIR:-${ANSIBLE_DIR}/playbooks}"
TAILSCALE_INVENTORY="${TAILSCALE_INVENTORY:-${INVENTORY_DIR}/tailscale_generated.yml}"
ANSIBLE_REGISTRY_MAX_PLAYBOOKS="${ANSIBLE_REGISTRY_MAX_PLAYBOOKS:-200}"
TAB=$(printf '\t')

usage() {
    cat <<'EOF'
Usage:
  pocket ansible list
  pocket ansible run <playbook_slug> [--run] [--diff] [--source <src>] [--inventory <file>] [--limit <pattern>]
EOF
}

fail() {
    printf '%s\n' "$1" >&2
    exit "${2:-1}"
}

warn() {
    printf '%s\n' "$1" >&2
}

strip_quotes() {
    VALUE="$1"
    VALUE=$(printf '%s' "${VALUE}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    case "${VALUE}" in
        \"*\") VALUE=${VALUE#\"}; VALUE=${VALUE%\"} ;;
        \'*\') VALUE=${VALUE#\'}; VALUE=${VALUE%\'} ;;
    esac
    printf '%s' "${VALUE}"
}

valid_slug() {
    VALUE="$1"
    LEN=${#VALUE}
    [ "${LEN}" -ge 1 ] && [ "${LEN}" -le 80 ] || return 1
    case "${VALUE}" in
        *[!abcdefghijklmnopqrstuvwxyz0123456789_-]*) return 1 ;;
    esac
    return 0
}

valid_category() {
    case "$1" in
        diagnostic|setup|deploy|maintenance|security|network|utility) return 0 ;;
        *) return 1 ;;
    esac
}

valid_inventory_source() {
    case "$1" in
        static|tailscale|merged) return 0 ;;
        *) return 1 ;;
    esac
}

valid_safe_mode() {
    case "$1" in
        check|run|diff) return 0 ;;
        *) return 1 ;;
    esac
}

safe_modes_contains() {
    MODES="$1"
    NEEDLE="$2"
    OLD_IFS=${IFS}
    IFS=,
    for SAFE_MODE_ITEM in ${MODES}; do
        if [ "${SAFE_MODE_ITEM}" = "${NEEDLE}" ]; then
            IFS=${OLD_IFS}
            return 0
        fi
    done
    IFS=${OLD_IFS}
    return 1
}

default_mode_for() {
    MODES="$1"
    if safe_modes_contains "${MODES}" "check"; then
        printf 'check'
        return 0
    fi
    printf '%s' "${MODES}" | awk -F ',' '{ print $1 }'
}

safe_modes_display() {
    printf '[%s]' "$(printf '%s' "$1" | sed 's/,/, /g')"
}

extract_yaml_line() {
    sed -n 's/.*line \([0-9][0-9]*\).*/\1/p' | sed -n '1p'
}

basic_yaml_check() {
    FILE="$1"
    awk '
        index($0, "[") && !index($0, "]") {
            print NR ": sequência ou mapa não fechado"
            exit 1
        }
        /^[[:space:]]*[^#[:space:]][^:]*$/ && $0 !~ /^[[:space:]]*-[[:space:]]/ {
            print NR ": linha sem chave YAML"
            exit 1
        }
    ' "${FILE}"
}

yaml_syntax_detail() {
    FILE="$1"

    BASIC_OUTPUT=$(basic_yaml_check "${FILE}" 2>/dev/null || true)
    if [ -n "${BASIC_OUTPUT}" ]; then
        printf '%s\n' "${BASIC_OUTPUT}"
        return 1
    fi

    [ "${POCKET_ANSIBLE_STRICT_YAML:-0}" = "1" ] || return 0

    if command -v python3 >/dev/null 2>&1 \
        && python3 -c 'import yaml' >/dev/null 2>&1; then
        python3 - "${FILE}" 2>&1 <<'PY'
import sys
import yaml

try:
    with open(sys.argv[1], "r", encoding="utf-8") as handle:
        yaml.safe_load(handle)
except yaml.YAMLError as exc:
    mark = getattr(exc, "problem_mark", None)
    if mark is not None:
        print(f"linha {mark.line + 1}: {getattr(exc, 'problem', exc)}")
    else:
        print(str(exc))
    raise SystemExit(1)
PY
        return $?
    fi

    if command -v ruby >/dev/null 2>&1; then
        ERROR_OUTPUT=$(ruby -e 'require "yaml"; YAML.load_file(ARGV[0])' "${FILE}" 2>&1 || true)
        if [ -n "${ERROR_OUTPUT}" ]; then
            LINE=$(printf '%s\n' "${ERROR_OUTPUT}" | extract_yaml_line)
            [ -n "${LINE}" ] || LINE="desconhecida"
            printf 'linha %s: YAML inválido\n' "${LINE}"
            return 1
        fi
        return 0
    fi

    return 0
}

parse_pocket_meta() {
    FILE="$1"
    awk '
        function trim(value) {
            gsub(/^[[:space:]]+/, "", value)
            gsub(/[[:space:]]+$/, "", value)
            return value
        }
        function strip(value) {
            value = trim(value)
            if (value ~ /^".*"$/ || value ~ /^\047.*\047$/) {
                value = substr(value, 2, length(value) - 2)
            }
            return value
        }
        /^- name:[[:space:]]*/ && first_play_seen != 1 {
            first_play_seen = 1
            first_name = $0
            sub(/^- name:[[:space:]]*/, "", first_name)
            if (strip(first_name) != "pocket_meta") {
                print "missing_meta\tpocket_meta ausente"
                invalid = 1
                exit
            }
            next
        }
        first_play_seen == 1 && /^  vars:[[:space:]]*$/ {
            in_vars = 1
            next
        }
        in_vars == 1 && /^    pocket_meta:[[:space:]]*$/ {
            in_meta = 1
            next
        }
        in_meta == 1 && /^      [A-Za-z_][A-Za-z0-9_]*:[[:space:]]*/ {
            key = $0
            sub(/^      /, "", key)
            sub(/:.*/, "", key)
            value = $0
            sub(/^      [A-Za-z_][A-Za-z0-9_]*:[[:space:]]*/, "", value)
            active_list = ""
            if (key == "safe_modes") {
                active_list = "safe_modes"
                next
            }
            if (key == "tags") {
                active_list = "tags"
                next
            }
            meta[key] = strip(value)
            next
        }
        in_meta == 1 && active_list == "safe_modes" && /^        -[[:space:]]*/ {
            value = $0
            sub(/^        -[[:space:]]*/, "", value)
            if (safe_modes == "") {
                safe_modes = strip(value)
            } else {
                safe_modes = safe_modes "," strip(value)
            }
            next
        }
        in_meta == 1 && /^  tasks:/ {
            in_meta = 0
            next
        }
        END {
            if (invalid == 1) {
                exit
            }
            if (first_play_seen != 1 || in_meta != 0 && meta["name"] == "" && safe_modes == "") {
                print "missing_meta\tpocket_meta ausente"
                exit
            }
            printf "ok\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", \
                meta["name"], meta["description"], meta["author"], meta["version"], \
                meta["category"], safe_modes, meta["inventory_source"], \
                meta["created_at"], meta["updated_at"]
        }
    ' "${FILE}"
}

validate_meta_record() {
    FILE_SLUG="$1"
    PARSED="$2"

    STATUS=${PARSED%%"${TAB}"*}
    PARSED=${PARSED#*"${TAB}"}
    if [ "${STATUS}" != "ok" ]; then
        DETAIL=${PARSED%%"${TAB}"*}
        printf 'invalid\t%s\t%s\n' "missing_meta" "${DETAIL}"
        return 0
    fi

    META_NAME=${PARSED%%"${TAB}"*}
    PARSED=${PARSED#*"${TAB}"}
    DESCRIPTION=${PARSED%%"${TAB}"*}
    PARSED=${PARSED#*"${TAB}"}
    AUTHOR=${PARSED%%"${TAB}"*}
    PARSED=${PARSED#*"${TAB}"}
    VERSION=${PARSED%%"${TAB}"*}
    PARSED=${PARSED#*"${TAB}"}
    CATEGORY=${PARSED%%"${TAB}"*}
    PARSED=${PARSED#*"${TAB}"}
    SAFE_MODES=${PARSED%%"${TAB}"*}
    PARSED=${PARSED#*"${TAB}"}
    INVENTORY_SOURCE=${PARSED%%"${TAB}"*}
    PARSED=${PARSED#*"${TAB}"}
    CREATED_AT=${PARSED%%"${TAB}"*}
    UPDATED_AT=${PARSED#*"${TAB}"}

    if ! valid_slug "${META_NAME}"; then
        printf 'invalid\tinvalid_meta\tname inválido: %s\n' "${META_NAME}"
        return 0
    fi
    if [ "${META_NAME}" != "${FILE_SLUG}" ]; then
        printf 'invalid\tinvalid_meta\tname não corresponde ao arquivo: %s\n' "${META_NAME}"
        return 0
    fi
    if [ -z "${DESCRIPTION}" ] || [ "${#DESCRIPTION}" -gt 120 ]; then
        printf 'invalid\tinvalid_meta\tdescription inválida\n'
        return 0
    fi
    if [ -z "${AUTHOR}" ]; then
        printf 'invalid\tinvalid_meta\tauthor obrigatório\n'
        return 0
    fi
    case "${VERSION}" in
        [0-9]*.[0-9]*.[0-9]*) ;;
        *) printf 'invalid\tinvalid_meta\tversion inválida: %s\n' "${VERSION}"; return 0 ;;
    esac
    if ! valid_category "${CATEGORY}"; then
        printf 'invalid\tinvalid_category\tcategoria desconhecida: %s\n' "${CATEGORY}"
        return 0
    fi
    if [ -z "${SAFE_MODES}" ]; then
        printf 'invalid\tinvalid_safe_modes\tsafe_modes vazio\n'
        return 0
    fi
    OLD_IFS=${IFS}
    IFS=,
    for MODE in ${SAFE_MODES}; do
        if ! valid_safe_mode "${MODE}"; then
            IFS=${OLD_IFS}
            printf 'invalid\tinvalid_safe_modes\tmodo desconhecido: %s\n' "${MODE}"
            return 0
        fi
    done
    IFS=${OLD_IFS}
    if ! valid_inventory_source "${INVENTORY_SOURCE}"; then
        printf 'invalid\tinvalid_meta\tinventory_source inválido: %s\n' "${INVENTORY_SOURCE}"
        return 0
    fi
    if [ -z "${CREATED_AT}" ] || [ -z "${UPDATED_AT}" ]; then
        printf 'invalid\tinvalid_meta\tcreated_at e updated_at obrigatórios\n'
        return 0
    fi

    printf 'valid\t%s\t%s\t%s\t%s\t%s\n' "${META_NAME}" "${DESCRIPTION}" "${CATEGORY}" "${SAFE_MODES}" "${INVENTORY_SOURCE}"
}

ensure_playbooks_dir() {
    if [ ! -d "${PLAYBOOKS_DIR}" ]; then
        if ! mkdir -p "${PLAYBOOKS_DIR}" 2>/dev/null; then
            fail "Não foi possível criar ${PLAYBOOKS_DIR}" 1
        fi
        printf 'Playbooks inicializados em ~/.pocketcli/ansible/playbooks/\n'
    fi
}

index_playbooks() {
    VALID_FILE="$1"
    INVALID_FILE="$2"
    LIMIT_WARN_FILE="$3"

    ensure_playbooks_dir
    COUNT=0
    FOUND=0

    for PLAYBOOK_PATH in "${PLAYBOOKS_DIR}"/*.yml; do
        [ -e "${PLAYBOOK_PATH}" ] || continue
        [ -L "${PLAYBOOK_PATH}" ] && continue
        [ -f "${PLAYBOOK_PATH}" ] || continue

        FOUND=1
        COUNT=$((COUNT + 1))
        if [ "${COUNT}" -gt "${ANSIBLE_REGISTRY_MAX_PLAYBOOKS}" ]; then
            printf '1\n' > "${LIMIT_WARN_FILE}"
            continue
        fi

        FILE_NAME=$(basename "${PLAYBOOK_PATH}")
        PB_SLUG=${FILE_NAME%.yml}

        if ! YAML_DETAIL=$(yaml_syntax_detail "${PLAYBOOK_PATH}"); then
            printf '%s\t%s\t%s\t%s\n' "${PB_SLUG}" "${PLAYBOOK_PATH}" "invalid_yaml" "Erro de sintaxe YAML em ${FILE_NAME}: ${YAML_DETAIL}" >> "${INVALID_FILE}"
            continue
        fi

        PARSED=$(parse_pocket_meta "${PLAYBOOK_PATH}")
        VALIDATION=$(validate_meta_record "${PB_SLUG}" "${PARSED}")
        STATUS=${VALIDATION%%"${TAB}"*}
        VALIDATION=${VALIDATION#*"${TAB}"}
        if [ "${STATUS}" = "valid" ]; then
            META_NAME=${VALIDATION%%"${TAB}"*}
            VALIDATION=${VALIDATION#*"${TAB}"}
            DESCRIPTION=${VALIDATION%%"${TAB}"*}
            VALIDATION=${VALIDATION#*"${TAB}"}
            CATEGORY=${VALIDATION%%"${TAB}"*}
            VALIDATION=${VALIDATION#*"${TAB}"}
            SAFE_MODES=${VALIDATION%%"${TAB}"*}
            INVENTORY_SOURCE=${VALIDATION#*"${TAB}"}
            printf '%s\t%s\t%s\t%s\t%s\t%s\n' "${META_NAME}" "${PLAYBOOK_PATH}" "${DESCRIPTION}" "${CATEGORY}" "${SAFE_MODES}" "${INVENTORY_SOURCE}" >> "${VALID_FILE}"
        else
            ERROR_TYPE=${VALIDATION%%"${TAB}"*}
            DETAIL=${VALIDATION#*"${TAB}"}
            printf '%s\t%s\t%s\t%s\n' "${PB_SLUG}" "${PLAYBOOK_PATH}" "${ERROR_TYPE}" "${DETAIL}" >> "${INVALID_FILE}"
        fi
    done

    [ "${FOUND}" -eq 1 ] || return 0
}

list_playbooks() {
    VALID_FILE=$(mktemp "${TMPDIR:-/tmp}/pocket-registry-valid.XXXXXX") || exit 1
    INVALID_FILE=$(mktemp "${TMPDIR:-/tmp}/pocket-registry-invalid.XXXXXX") || exit 1
    LIMIT_WARN_FILE=$(mktemp "${TMPDIR:-/tmp}/pocket-registry-limit.XXXXXX") || exit 1
    : > "${LIMIT_WARN_FILE}"
    trap 'rm -f "${VALID_FILE:-}" "${INVALID_FILE:-}" "${LIMIT_WARN_FILE:-}"' EXIT INT TERM HUP

    index_playbooks "${VALID_FILE}" "${INVALID_FILE}" "${LIMIT_WARN_FILE}"

    if [ -s "${LIMIT_WARN_FILE}" ]; then
        warn "Limite de 200 playbooks atingido — os demais foram ignorados"
    fi

    if [ ! -s "${VALID_FILE}" ] && [ ! -s "${INVALID_FILE}" ]; then
        printf 'Nenhum playbook encontrado em ~/.pocketcli/ansible/playbooks/\n'
        return 0
    fi

    if [ -s "${VALID_FILE}" ]; then
        printf '%-20s %-36s %-13s %-12s %s\n' "nome" "descrição" "categoria" "modo_padrão" "safe_modes"
        awk -F '\t' '{
            split($5, modes, ",")
            default_mode = "check"
            has_check = 0
            for (i in modes) {
                if (modes[i] == "check") {
                    has_check = 1
                }
            }
            if (has_check != 1) {
                default_mode = modes[1]
            }
            safe = "[" $5 "]"
            gsub(",", ", ", safe)
            printf "%-20s %-36s %-13s %-12s %s\n", $1, $3, $4, default_mode, safe
        }' "${VALID_FILE}"
    fi

    if [ -s "${INVALID_FILE}" ]; then
        INVALID_COUNT=$(wc -l < "${INVALID_FILE}" | tr -d '[:space:]')
        printf '\nPlaybooks com erro de metadados: %s\n' "${INVALID_COUNT}"
        awk -F '\t' '{ printf "%s: %s\n", $1, $4 }' "${INVALID_FILE}"
    fi
}

find_valid_playbook() {
    VALID_FILE="$1"
    FIND_SLUG="$2"
    awk -F '\t' -v slug="${FIND_SLUG}" '$1 == slug { print; found = 1; exit } END { if (found != 1) exit 1 }' "${VALID_FILE}"
}

find_invalid_playbook() {
    INVALID_FILE="$1"
    FIND_SLUG="$2"
    awk -F '\t' -v slug="${FIND_SLUG}" '$1 == slug { print; found = 1; exit } END { if (found != 1) exit 1 }' "${INVALID_FILE}"
}

run_playbook() {
    [ $# -ge 1 ] || fail "Usage: pocket ansible run <playbook_slug> [--run] [--diff]" 1

    REQUESTED="$1"
    shift
    case "${REQUESTED}" in
        *.yml) SLUG=${REQUESTED%.yml} ;;
        *) SLUG=${REQUESTED} ;;
    esac

    MODE="check"
    RUN_FLAG=0
    DIFF_FLAG=0
    EXPLICIT_SOURCE=""
    LIMIT_VALUE=""
    CUSTOM_INVENTORY=""

    while [ $# -gt 0 ]; do
        case "$1" in
            --run)
                MODE="run"
                RUN_FLAG=1
                shift
                ;;
            --diff)
                if [ "${MODE}" != "run" ]; then
                    MODE="diff"
                fi
                DIFF_FLAG=1
                shift
                ;;
            --source)
                [ $# -ge 2 ] || fail "Uso inválido: --source requer static, tailscale ou merged" 1
                EXPLICIT_SOURCE="$2"
                shift 2
                ;;
            --inventory)
                [ $# -ge 2 ] || fail "Uso inválido: --inventory requer um arquivo" 1
                CUSTOM_INVENTORY="$2"
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

    VALID_FILE=$(mktemp "${TMPDIR:-/tmp}/pocket-registry-valid.XXXXXX") || exit 1
    INVALID_FILE=$(mktemp "${TMPDIR:-/tmp}/pocket-registry-invalid.XXXXXX") || exit 1
    LIMIT_WARN_FILE=$(mktemp "${TMPDIR:-/tmp}/pocket-registry-limit.XXXXXX") || exit 1
    : > "${LIMIT_WARN_FILE}"
    trap 'rm -f "${VALID_FILE:-}" "${INVALID_FILE:-}" "${LIMIT_WARN_FILE:-}"' EXIT INT TERM HUP

    index_playbooks "${VALID_FILE}" "${INVALID_FILE}" "${LIMIT_WARN_FILE}"

    if INVALID_ROW=$(find_invalid_playbook "${INVALID_FILE}" "${SLUG}"); then
        ERROR_TYPE=$(printf '%s\n' "${INVALID_ROW}" | awk -F '\t' '{ print $3 }')
        DETAIL=$(printf '%s\n' "${INVALID_ROW}" | awk -F '\t' '{ print $4 }')
        if [ "${ERROR_TYPE}" = "invalid_yaml" ]; then
            fail "${DETAIL}" 3
        fi
        fail "Playbook '${SLUG}' inválido: ${DETAIL}" 3
    fi

    if ! ROW=$(find_valid_playbook "${VALID_FILE}" "${SLUG}"); then
        fail "Playbook '${SLUG}' não encontrado. Use: pocket ansible list" 3
    fi

    SAFE_MODES=$(printf '%s\n' "${ROW}" | awk -F '\t' '{ print $5 }')
    META_SOURCE=$(printf '%s\n' "${ROW}" | awk -F '\t' '{ print $6 }')
    SOURCE_TO_USE="${EXPLICIT_SOURCE:-${META_SOURCE}}"

    if ! safe_modes_contains "${SAFE_MODES}" "${MODE}"; then
        fail "Playbook ${SLUG} não permite modo ${MODE}. safe_modes: $(safe_modes_display "${SAFE_MODES}")" 1
    fi

    if [ -z "${CUSTOM_INVENTORY}" ] && [ "${SOURCE_TO_USE}" = "tailscale" ] && [ ! -f "${TAILSCALE_INVENTORY}" ]; then
        fail "Playbook requer fonte 'tailscale' mas inventário não foi gerado. Execute: pocket ansible inventory refresh" 2
    fi

    ADAPTER_SCRIPT="${POCKET_DIR}/lib/ansible_adapter.sh"
    [ -f "${ADAPTER_SCRIPT}" ] || fail "Script adapter não encontrado: ${ADAPTER_SCRIPT}" 1

    set -- run "${SLUG}.yml"
    [ "${RUN_FLAG}" -eq 1 ] && set -- "$@" --run
    [ "${DIFF_FLAG}" -eq 1 ] && set -- "$@" --diff
    if [ -n "${CUSTOM_INVENTORY}" ]; then
        set -- "$@" --inventory "${CUSTOM_INVENTORY}"
    elif [ -n "${EXPLICIT_SOURCE}" ]; then
        set -- "$@" --source "${EXPLICIT_SOURCE}"
    else
        set -- "$@" --source "${SOURCE_TO_USE}"
    fi
    [ -n "${LIMIT_VALUE}" ] && set -- "$@" --limit "${LIMIT_VALUE}"

    POCKET_ANSIBLE_DISPLAY_NAME="${SLUG}" exec sh "${ADAPTER_SCRIPT}" "$@"
}

main() {
    COMMAND="${1:-list}"
    case "${COMMAND}" in
        list)
            shift
            [ $# -eq 0 ] || fail "Uso inválido: pocket ansible list" 1
            list_playbooks
            ;;
        run)
            shift
            run_playbook "$@"
            ;;
        help|-h|--help)
            usage
            ;;
        *)
            fail "Subcomando de registry inválido: ${COMMAND}" 1
            ;;
    esac
}

main "$@"

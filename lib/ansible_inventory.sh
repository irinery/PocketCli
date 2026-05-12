#!/usr/bin/env sh
# PocketCli — Ansible inventory management.
# POSIX sh only; this file is installed as ~/.pocketcli/lib/ansible_inventory.sh.

set -u

POCKET_DIR="${POCKET_DIR:-${POCKETCLI_DIR:-${HOME}/.pocketcli}}"
ANSIBLE_DIR="${ANSIBLE_DIR:-${POCKET_DIR}/ansible}"
INVENTORY_DIR="${INVENTORY_DIR:-${ANSIBLE_DIR}/inventory}"
STATIC_INVENTORY="${STATIC_INVENTORY:-${INVENTORY_DIR}/hosts.yml}"
TAILSCALE_INVENTORY="${TAILSCALE_INVENTORY:-${INVENTORY_DIR}/tailscale_generated.yml}"
MERGED_INVENTORY="${MERGED_INVENTORY:-${INVENTORY_DIR}/merged.yml}"
ANSIBLE_MAX_HOSTS="${ANSIBLE_MAX_HOSTS:-50}"

usage() {
    cat <<'EOF'
Usage:
  pocket ansible inventory [--source static|tailscale|merged]
  pocket ansible inventory refresh
EOF
}

fail() {
    printf '%s\n' "$1" >&2
    exit "${2:-1}"
}

warn() {
    printf '%s\n' "$1" >&2
}

now_iso8601() {
    date '+%Y-%m-%dT%H:%M:%S%z'
}

validate_source() {
    case "$1" in
        static|tailscale|merged) return 0 ;;
        *) fail "Origem de inventário inválida: $1" 1 ;;
    esac
}

inventory_label() {
    case "$1" in
        static) printf 'estático' ;;
        tailscale) printf 'Tailscale' ;;
        merged) printf 'mesclado' ;;
        *) printf '%s' "$1" ;;
    esac
}

extract_yaml_line() {
    sed -n 's/.*line \([0-9][0-9]*\).*/\1/p' | sed -n '1p'
}

basic_yaml_check() {
    FILE="$1"
    awk '
        /^[[:space:]]*$/ {
            next
        }
        /^[[:space:]]*#/ {
            next
        }
        /^[[:space:]]*---[[:space:]]*$/ {
            next
        }
        /^[[:space:]]*-[[:space:]]/ {
            next
        }
        index($0, "[") && !index($0, "]") {
            print NR
            exit 1
        }
        /^[[:space:]]*[^#[:space:]][^:]*$/ {
            print NR
            exit 1
        }
    ' "${FILE}"
}

validate_yaml_syntax() {
    FILE="$1"
    YAML_SOURCE="$2"
    LABEL=$(inventory_label "${YAML_SOURCE}")

    [ -f "${FILE}" ] || fail "Inventário ${LABEL} não encontrado: ${FILE}" 3

    if command -v python3 >/dev/null 2>&1 \
        && python3 -c 'import yaml' >/dev/null 2>&1; then
        ERROR_OUTPUT=$(python3 - "${FILE}" 2>&1 <<'PY' || true
import sys
import yaml

try:
    with open(sys.argv[1], "r", encoding="utf-8") as handle:
        yaml.safe_load(handle)
except yaml.YAMLError as exc:
    mark = getattr(exc, "problem_mark", None)
    if mark is not None:
        print(f"line {mark.line + 1}", file=sys.stderr)
    else:
        print(str(exc), file=sys.stderr)
    raise SystemExit(1)
PY
        )
        if [ -n "${ERROR_OUTPUT}" ]; then
            LINE=$(printf '%s\n' "${ERROR_OUTPUT}" | extract_yaml_line)
            [ -n "${LINE}" ] || LINE="desconhecida"
            fail "Inventário ${LABEL} com erro de sintaxe YAML: linha ${LINE}" 3
        fi
        return 0
    fi

    if command -v ruby >/dev/null 2>&1; then
        ERROR_OUTPUT=$(ruby -e 'require "yaml"; YAML.load_file(ARGV[0])' "${FILE}" 2>&1 || true)
        if [ -n "${ERROR_OUTPUT}" ]; then
            LINE=$(printf '%s\n' "${ERROR_OUTPUT}" | extract_yaml_line)
            [ -n "${LINE}" ] || LINE="desconhecida"
            fail "Inventário ${LABEL} com erro de sintaxe YAML: linha ${LINE}" 3
        fi
        return 0
    fi

    LINE=$(basic_yaml_check "${FILE}" 2>/dev/null || true)
    if [ -n "${LINE}" ]; then
        fail "Inventário ${LABEL} com erro de sintaxe YAML: linha ${LINE}" 3
    fi
}

ensure_inventory_dir() {
    if [ ! -d "${INVENTORY_DIR}" ]; then
        if ! mkdir -p "${INVENTORY_DIR}" 2>/dev/null; then
            fail "Não foi possível criar ${INVENTORY_DIR}" 1
        fi
        INVENTORY_CREATED=1
    fi
}

write_empty_static_inventory() {
    [ -f "${STATIC_INVENTORY}" ] && return 0
    if ! {
        printf 'all:\n'
        printf '  vars: {}\n'
        printf '  hosts: {}\n'
        printf '  children: {}\n'
    } > "${STATIC_INVENTORY}" 2>/dev/null; then
        fail "Não foi possível escrever ${STATIC_INVENTORY}" 1
    fi
}

init_inventory_if_missing() {
    INVENTORY_CREATED=0
    ensure_inventory_dir
    write_empty_static_inventory
    if [ "${INVENTORY_CREATED}" -eq 1 ]; then
        printf 'Inventário inicializado em ~/.pocketcli/ansible/inventory/\n'
    fi
}

sanitize_hostname() {
    RAW="$1"
    CLEAN=$(printf '%s' "${RAW}" | tr -cd 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.-' | sed 's/^[^ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789]*//' | cut -c 1-63)
    if [ -z "${CLEAN}" ]; then
        CLEAN="host"
    fi
    printf '%s' "${CLEAN}"
}

parse_inventory_records() {
    PARSE_FILE="$1"
    PARSE_SOURCE="$2"
    awk -v source="${PARSE_SOURCE}" '
        function trim(value) {
            gsub(/^[[:space:]]+/, "", value)
            gsub(/[[:space:]]+$/, "", value)
            return value
        }
        function strip_quotes(value) {
            value = trim(value)
            if (value ~ /^".*"$/ || value ~ /^\047.*\047$/) {
                value = substr(value, 2, length(value) - 2)
            }
            return value
        }
        function valid_host(value) {
            return value ~ /^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$/
        }
        function add_group(host, group) {
            if (host == "" || group == "") {
                return
            }
            if (groups[host] == "") {
                groups[host] = group
            } else {
                groups[host] = groups[host] "," group
            }
        }
        function reject_host(value) {
            if (rejected[value] != 1) {
                print "Hostname inválido rejeitado: " value > "/dev/stderr"
                rejected[value] = 1
            }
        }
        /^[[:space:]]*#/ { next }
        /^  hosts:[[:space:]]*(\{\})?[[:space:]]*$/ {
            section = "hosts"
            current = ""
            next
        }
        /^  children:[[:space:]]*(\{\})?[[:space:]]*$/ {
            section = "children"
            current = ""
            current_group = ""
            next
        }
        section == "hosts" && /^    [^ ][^:]*:[[:space:]]*(\{\})?[[:space:]]*$/ {
            name = $0
            sub(/^    /, "", name)
            sub(/:.*/, "", name)
            name = strip_quotes(name)
            current = ""
            in_tags = 0
            if (!valid_host(name)) {
                reject_host(name)
                next
            }
            current = name
            order[++order_count] = current
            host_seen[current] = 1
            port[current] = 22
            src[current] = source
            next
        }
        section == "hosts" && current != "" && /^      ansible_host:[[:space:]]*/ {
            value = $0
            sub(/^      ansible_host:[[:space:]]*/, "", value)
            host_ip[current] = strip_quotes(value)
            in_tags = 0
            next
        }
        section == "hosts" && current != "" && /^      ansible_port:[[:space:]]*/ {
            value = $0
            sub(/^      ansible_port:[[:space:]]*/, "", value)
            port[current] = trim(value)
            in_tags = 0
            next
        }
        section == "hosts" && current != "" && /^      pocket_source:[[:space:]]*/ {
            value = $0
            sub(/^      pocket_source:[[:space:]]*/, "", value)
            src[current] = strip_quotes(value)
            in_tags = 0
            next
        }
        section == "hosts" && current != "" && /^      pocket_tailscale_id:[[:space:]]*/ {
            value = $0
            sub(/^      pocket_tailscale_id:[[:space:]]*/, "", value)
            tail_id[current] = strip_quotes(value)
            in_tags = 0
            next
        }
        section == "hosts" && current != "" && /^      pocket_tags:[[:space:]]*$/ {
            in_tags = 1
            next
        }
        section == "hosts" && current != "" && in_tags == 1 && /^        -[[:space:]]*/ {
            value = $0
            sub(/^        -[[:space:]]*/, "", value)
            add_group(current, strip_quotes(value))
            next
        }
        section == "children" && /^    [^ ][^:]*:[[:space:]]*$/ {
            current_group = $0
            sub(/^    /, "", current_group)
            sub(/:.*/, "", current_group)
            current_group = strip_quotes(current_group)
            next
        }
        section == "children" && current_group != "" && /^        [^ ][^:]*:[[:space:]]*(\{\})?[[:space:]]*$/ {
            name = $0
            sub(/^        /, "", name)
            sub(/:.*/, "", name)
            name = strip_quotes(name)
            if (!valid_host(name)) {
                reject_host(name)
                next
            }
            add_group(name, current_group)
            next
        }
        END {
            for (i = 1; i <= order_count; i++) {
                host = order[i]
                if (host_seen[host] != 1) {
                    continue
                }
                if (host_ip[host] == "") {
                    print "Host sem ansible_host rejeitado: " host > "/dev/stderr"
                    continue
                }
                printf "%s\t%s\t%s\t%s\t%s\t%s\n", host, host_ip[host], port[host], src[host], groups[host], tail_id[host]
            }
        }
    ' "${PARSE_FILE}"
}

records_count() {
    FILE="$1"
    if [ ! -s "${FILE}" ]; then
        printf '0'
        return 0
    fi
    wc -l < "${FILE}" | tr -d '[:space:]'
}

show_records_table() {
    RECORDS_FILE="$1"
    SOURCE="$2"
    printf 'source: %s\n' "${SOURCE}"
    printf '%-24s %-18s %s\n' "name" "ansible_host" "groups"
    awk -F '\t' '{
        groups = $5
        if (groups == "") {
            groups = "-"
        }
        printf "%-24s %-18s %s\n", $1, $2, groups
    }' "${RECORDS_FILE}"
}

write_inventory_from_records() {
    RECORDS_FILE="$1"
    DEST_FILE="$2"
    WRITE_SOURCE="$3"

    TMP_FILE=$(mktemp "${TMPDIR:-/tmp}/pocket-inventory-write.XXXXXX") || exit 1
    {
        if [ "${WRITE_SOURCE}" = "tailscale" ]; then
            printf '# Gerado automaticamente — não editar manualmente\n'
            printf '# generated_at: %s\n' "$(now_iso8601)"
            printf '# source: tailscale\n'
        elif [ "${WRITE_SOURCE}" = "merged" ]; then
            printf '# Gerado automaticamente — não editar manualmente\n'
            printf '# generated_at: %s\n' "$(now_iso8601)"
            printf '# source: merged\n'
        fi
        printf 'all:\n'
        if [ -s "${RECORDS_FILE}" ]; then
            printf '  hosts:\n'
            awk -F '\t' '{
                printf "    %s:\n", $1
                printf "      ansible_host: %s\n", $2
                printf "      ansible_port: %s\n", ($3 == "" ? 22 : $3)
                if ($4 != "" && $4 != "static") {
                    printf "      pocket_source: %s\n", $4
                }
                if ($6 != "") {
                    printf "      pocket_tailscale_id: %s\n", $6
                }
            }' "${RECORDS_FILE}"
        else
            printf '  hosts: {}\n'
        fi
        if [ "${WRITE_SOURCE}" = "tailscale" ]; then
            printf '  children:\n'
            printf '    tailscale:\n'
            if [ -s "${RECORDS_FILE}" ]; then
                printf '      hosts:\n'
                awk -F '\t' '{ printf "        %s: {}\n", $1 }' "${RECORDS_FILE}"
            else
                printf '      hosts: {}\n'
            fi
        else
            awk -F '\t' '
                $5 != "" {
                    split($5, parts, ",")
                    for (i in parts) {
                        group = parts[i]
                        if (group == "") {
                            continue
                        }
                        key = group SUBSEP $1
                        group_seen[group] = 1
                        group_host[key] = 1
                    }
                }
                END {
                    emitted = 0
                    for (group in group_seen) {
                        if (emitted == 0) {
                            printf "  children:\n"
                        }
                        emitted = 1
                        printf "    %s:\n", group
                        printf "      hosts:\n"
                        for (key in group_host) {
                            split(key, parts, SUBSEP)
                            if (parts[1] == group) {
                                printf "        %s: {}\n", parts[2]
                            }
                        }
                    }
                    if (emitted == 0) {
                        printf "  children: {}\n"
                    }
                }
            ' "${RECORDS_FILE}"
        fi
    } > "${TMP_FILE}" || {
        rm -f "${TMP_FILE}"
        fail "Não foi possível gerar inventário ${DEST_FILE}" 1
    }

    if ! mv "${TMP_FILE}" "${DEST_FILE}" 2>/dev/null; then
        rm -f "${TMP_FILE}"
        fail "Não foi possível escrever ${DEST_FILE}" 1
    fi
}

tailscale_cache_timestamp() {
    if [ -f "${TAILSCALE_INVENTORY}" ]; then
        sed -n 's/^# generated_at: //p' "${TAILSCALE_INVENTORY}" | sed -n '1p'
    fi
}

tailscale_is_online() {
    command -v tailscale >/dev/null 2>&1 || return 1
    tailscale status --json >/dev/null 2>&1
}

tailscale_status_json() {
    command -v tailscale >/dev/null 2>&1 || return 1
    tailscale status --json
}

generate_tailscale_records() {
    JSON_FILE="$1"
    RAW_RECORDS="$2"

    if command -v jq >/dev/null 2>&1; then
        jq -r '
            (.Peer // {}) | to_entries[] |
            [
              (.value.HostName // .value.DNSName // .key),
              ((.value.TailscaleIPs // [])[0] // ""),
              (.value.ID // .key)
            ] | @tsv
        ' "${JSON_FILE}" > "${RAW_RECORDS}"
    elif command -v python3 >/dev/null 2>&1; then
        python3 - "${JSON_FILE}" > "${RAW_RECORDS}" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    data = json.load(handle)

for key, value in (data.get("Peer") or {}).items():
    name = value.get("HostName") or value.get("DNSName") or key
    ips = value.get("TailscaleIPs") or []
    ip = ips[0] if ips else ""
    node_id = value.get("ID") or key
    print(f"{name}\t{ip}\t{node_id}")
PY
    else
        fail "jq ou python3 é necessário para ler tailscale status --json" 1
    fi
}

refresh_tailscale_inventory() {
    ensure_inventory_dir

    JSON_FILE=$(mktemp "${TMPDIR:-/tmp}/pocket-tailscale-json.XXXXXX") || exit 1
    RAW_RECORDS=$(mktemp "${TMPDIR:-/tmp}/pocket-tailscale-raw.XXXXXX") || exit 1
    RECORDS_FILE=$(mktemp "${TMPDIR:-/tmp}/pocket-tailscale-records.XXXXXX") || exit 1
    trap 'rm -f "${JSON_FILE:-}" "${RAW_RECORDS:-}" "${RECORDS_FILE:-}"' EXIT INT TERM HUP

    if ! tailscale_status_json > "${JSON_FILE}" 2>/dev/null; then
        fail "Tailscale não está ativo — inventário dinâmico não gerado" 2
    fi

    generate_tailscale_records "${JSON_FILE}" "${RAW_RECORDS}"
    while IFS='	' read -r RAW_HOST IP NODE_ID || [ -n "${RAW_HOST:-}" ]; do
        [ -n "${RAW_HOST}" ] || continue
        [ -n "${IP}" ] || continue
        HOST=$(sanitize_hostname "${RAW_HOST}")
        if [ "${HOST}" != "${RAW_HOST}" ]; then
            warn "Hostname Tailscale sanitizado: ${RAW_HOST} -> ${HOST}"
        fi
        printf '%s\t%s\t22\ttailscale\ttailscale\t%s\n' "${HOST}" "${IP}" "${NODE_ID}" >> "${RECORDS_FILE}"
    done < "${RAW_RECORDS}"

    write_inventory_from_records "${RECORDS_FILE}" "${TAILSCALE_INVENTORY}" "tailscale"
    COUNT=$(records_count "${RECORDS_FILE}")
    printf 'Inventário Tailscale gerado em %s (%s hosts)\n' "${TAILSCALE_INVENTORY}" "${COUNT}"
}

resolve_source() {
    REQUESTED_SOURCE="$1"
    EXPLICIT_SOURCE="$2"

    if [ "${EXPLICIT_SOURCE}" -eq 1 ]; then
        validate_source "${REQUESTED_SOURCE}"
        printf '%s' "${REQUESTED_SOURCE}"
        return 0
    fi

    if [ -f "${STATIC_INVENTORY}" ]; then
        printf 'static'
        return 0
    fi
    if [ -f "${TAILSCALE_INVENTORY}" ]; then
        warn "Inventário estático ausente — usando inventário Tailscale"
        printf 'tailscale'
        return 0
    fi

    fail "Nenhuma fonte de inventário disponível" 2
}

records_for_source() {
    RECORD_SOURCE="$1"
    RECORDS_FILE="$2"

    case "${RECORD_SOURCE}" in
        static)
            [ -f "${STATIC_INVENTORY}" ] || fail "Inventário estático não encontrado: ${STATIC_INVENTORY}" 3
            validate_yaml_syntax "${STATIC_INVENTORY}" "static"
            parse_inventory_records "${STATIC_INVENTORY}" "static" > "${RECORDS_FILE}"
            ;;
        tailscale)
            [ -f "${TAILSCALE_INVENTORY}" ] || fail "Inventário Tailscale não encontrado. Execute: pocket ansible inventory refresh" 3
            validate_yaml_syntax "${TAILSCALE_INVENTORY}" "tailscale"
            parse_inventory_records "${TAILSCALE_INVENTORY}" "tailscale" > "${RECORDS_FILE}"
            ;;
        merged)
            records_for_merged "${RECORDS_FILE}"
            ;;
    esac
}

records_for_merged() {
    RECORDS_FILE="$1"
    STATIC_RECORDS=$(mktemp "${TMPDIR:-/tmp}/pocket-static-records.XXXXXX") || exit 1
    TS_RECORDS=$(mktemp "${TMPDIR:-/tmp}/pocket-ts-records.XXXXXX") || exit 1

    HAVE_STATIC=0
    HAVE_TS=0

    if [ -f "${STATIC_INVENTORY}" ]; then
        validate_yaml_syntax "${STATIC_INVENTORY}" "static"
        parse_inventory_records "${STATIC_INVENTORY}" "static" > "${STATIC_RECORDS}"
        [ -s "${STATIC_RECORDS}" ] && HAVE_STATIC=1
    fi

    if [ -f "${TAILSCALE_INVENTORY}" ]; then
        if ! tailscale_is_online; then
            warn "Usando inventário Tailscale em cache (tailscale offline)"
            TS_GENERATED_AT=$(tailscale_cache_timestamp)
            [ -n "${TS_GENERATED_AT}" ] && warn "generated_at: ${TS_GENERATED_AT}"
        fi
        validate_yaml_syntax "${TAILSCALE_INVENTORY}" "tailscale"
        parse_inventory_records "${TAILSCALE_INVENTORY}" "tailscale" > "${TS_RECORDS}"
        [ -s "${TS_RECORDS}" ] && HAVE_TS=1
    fi

    if [ "${HAVE_STATIC}" -eq 0 ] && [ "${HAVE_TS}" -eq 0 ]; then
        rm -f "${STATIC_RECORDS}" "${TS_RECORDS}"
        fail "Nenhuma fonte de inventário disponível" 2
    fi

    if [ "${HAVE_STATIC}" -eq 1 ]; then
        cat "${STATIC_RECORDS}" >> "${RECORDS_FILE}"
    else
        warn "Inventário estático ausente — usando apenas Tailscale"
    fi

    if [ "${HAVE_TS}" -eq 1 ] && [ "${HAVE_STATIC}" -eq 1 ]; then
        awk -F '\t' 'FNR == NR { static[$1] = 1; next }
            static[$1] == 1 {
                print "Conflito resolvido: " $1 " — estático prevalece" > "/dev/stderr"
                next
            }
            {
                print
            }
        ' "${STATIC_RECORDS}" "${TS_RECORDS}" >> "${RECORDS_FILE}"
    elif [ "${HAVE_TS}" -eq 1 ]; then
        cat "${TS_RECORDS}" >> "${RECORDS_FILE}"
    elif [ "${HAVE_STATIC}" -eq 1 ]; then
        warn "Inventário Tailscale ausente — usando apenas estático"
    fi

    rm -f "${STATIC_RECORDS}" "${TS_RECORDS}"
}

resolve_inventory_path() {
    REQUESTED_SOURCE="$1"
    EXPLICIT_SOURCE="$2"
    LIMIT_APPLIED="$3"

    SOURCE_OUTPUT=$(resolve_source "${REQUESTED_SOURCE}" "${EXPLICIT_SOURCE}")
    SOURCE_RC=$?
    [ "${SOURCE_RC}" -eq 0 ] || exit "${SOURCE_RC}"
    SOURCE="${SOURCE_OUTPUT}"
    RECORDS_FILE=$(mktemp "${TMPDIR:-/tmp}/pocket-inventory-records.XXXXXX") || exit 1
    trap 'rm -f "${RECORDS_FILE:-}"' EXIT INT TERM HUP

    records_for_source "${SOURCE}" "${RECORDS_FILE}"
    COUNT=$(records_count "${RECORDS_FILE}")
    if [ "${COUNT}" -gt "${ANSIBLE_MAX_HOSTS}" ] && [ "${LIMIT_APPLIED}" -eq 0 ]; then
        fail "Limite de 50 hosts excedido (${COUNT} encontrados). Use --limit para restringir." 1
    fi

    case "${SOURCE}" in
        static)
            RESOLVED_PATH="${STATIC_INVENTORY}"
            ;;
        tailscale)
            RESOLVED_PATH="${TAILSCALE_INVENTORY}"
            ;;
        merged)
            ensure_inventory_dir
            write_inventory_from_records "${RECORDS_FILE}" "${MERGED_INVENTORY}" "merged"
            RESOLVED_PATH="${MERGED_INVENTORY}"
            ;;
    esac

    printf 'inventory_path=%s\n' "${RESOLVED_PATH}"
    printf 'inventory_source=%s\n' "${SOURCE}"
    printf 'hosts_count=%s\n' "${COUNT}"
}

inventory_command() {
    if [ "${1:-}" = "refresh" ]; then
        shift
        [ $# -eq 0 ] || fail "Uso inválido: pocket ansible inventory refresh" 1
        refresh_tailscale_inventory
        return 0
    fi

    SOURCE="static"
    EXPLICIT_SOURCE=0

    while [ $# -gt 0 ]; do
        case "$1" in
            --source)
                [ $# -ge 2 ] || fail "Uso inválido: --source requer static, tailscale ou merged" 1
                SOURCE="$2"
                validate_source "${SOURCE}"
                EXPLICIT_SOURCE=1
                shift 2
                ;;
            *)
                fail "Argumento inválido para inventory: $1" 1
                ;;
        esac
    done

    if [ "${EXPLICIT_SOURCE}" -eq 0 ] && [ ! -d "${INVENTORY_DIR}" ]; then
        init_inventory_if_missing
    fi

    RECORDS_FILE=$(mktemp "${TMPDIR:-/tmp}/pocket-inventory-show.XXXXXX") || exit 1
    trap 'rm -f "${RECORDS_FILE:-}"' EXIT INT TERM HUP

    SOURCE_OUTPUT=$(resolve_source "${SOURCE}" "${EXPLICIT_SOURCE}")
    SOURCE_RC=$?
    [ "${SOURCE_RC}" -eq 0 ] || exit "${SOURCE_RC}"
    SOURCE="${SOURCE_OUTPUT}"
    records_for_source "${SOURCE}" "${RECORDS_FILE}"

    if [ "${SOURCE}" = "merged" ]; then
        ensure_inventory_dir
        write_inventory_from_records "${RECORDS_FILE}" "${MERGED_INVENTORY}" "merged"
    fi

    show_records_table "${RECORDS_FILE}" "${SOURCE}"
}

resolve_command() {
    SOURCE="static"
    EXPLICIT_SOURCE=0
    LIMIT_APPLIED=0

    while [ $# -gt 0 ]; do
        case "$1" in
            --source)
                [ $# -ge 2 ] || fail "Uso inválido: --source requer static, tailscale ou merged" 1
                SOURCE="$2"
                validate_source "${SOURCE}"
                EXPLICIT_SOURCE=1
                shift 2
                ;;
            --limit)
                [ $# -ge 2 ] || fail "Uso inválido: --limit requer um padrão" 1
                LIMIT_APPLIED=1
                shift 2
                ;;
            *)
                fail "Argumento inválido para resolve: $1" 1
                ;;
        esac
    done

    resolve_inventory_path "${SOURCE}" "${EXPLICIT_SOURCE}" "${LIMIT_APPLIED}"
}

main() {
    COMMAND="${1:-inventory}"
    case "${COMMAND}" in
        inventory)
            shift
            inventory_command "$@"
            ;;
        refresh)
            shift
            refresh_tailscale_inventory "$@"
            ;;
        resolve)
            shift
            resolve_command "$@"
            ;;
        help|-h|--help)
            usage
            ;;
        *)
            fail "Subcomando de inventário inválido: ${COMMAND}" 1
            ;;
    esac
}

main "$@"

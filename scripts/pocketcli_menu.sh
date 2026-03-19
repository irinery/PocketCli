#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/pocketcli_menu.sh
# Lightweight TUI with POSIX sh + stty. Works without fzf and keeps old systems
# compatible, including iSH / Alpine-style environments.
# =============================================================================

set -eu

POCKETCLI_DIR="${HOME}/.pocketcli"
. "${POCKETCLI_DIR}/lib/common.sh"

# Ensure pocket is in PATH for this session
export PATH="${POCKETCLI_DIR}:${PATH}"

HOSTS_FILE="${POCKETCLI_DIR}/hosts"
CURRENT_INDEX=1
MENU_ACTION=""
LAST_MESSAGE="Use j/k para navegar, Enter para abrir, q para sair."
INPUT_BUFFER=""

_menu_items() {
    cat <<'ITEMS'
connect|Conectar em servidor|SSH rápido com hosts salvos ou peers online
hosts|Gerenciar hosts|Adicionar e remover atalhos locais
radar|Radar Tailscale|Ver máquinas disponíveis agora
update|Atualizar PocketCli|Fazer git pull com feedback simples
exit|Sair|Fechar o PocketCli com elegância
ITEMS
}

_menu_count() {
    _menu_items | awk 'END { print NR }'
}

_menu_line() {
    IDX="$1"
    _menu_items | sed -n "${IDX}p"
}

_screen_clear() {
    if command -v clear >/dev/null 2>&1; then
        clear
    else
        printf '\033[2J\033[H'
    fi
}

_supports_utf8() {
    case "${LC_ALL:-${LC_CTYPE:-${LANG:-}}}" in
        *UTF-8*|*utf8*|*utf-8*) return 0 ;;
        *) return 1 ;;
    esac
}

_header() {
    _screen_clear
    echo ""
    if _supports_utf8; then
        printf "  ${C_CYAN}╭──────────────────────────────────────────────╮${C_NC}\n"
        printf "  ${C_CYAN}│${C_NC} ${C_BOLD}PocketCli${C_NC} ${C_DIM}· terminal portátil, leve e veloz${C_NC} ${C_CYAN}│${C_NC}\n"
        printf "  ${C_CYAN}╰──────────────────────────────────────────────╯${C_NC}\n"
    else
        printf "  ${C_CYAN}+----------------------------------------------+${C_NC}\n"
        printf "  ${C_CYAN}|${C_NC} ${C_BOLD}PocketCli${C_NC} ${C_DIM}- terminal portatil, leve e veloz${C_NC} ${C_CYAN}|${C_NC}\n"
        printf "  ${C_CYAN}+----------------------------------------------+${C_NC}\n"
    fi
    echo ""
}

_draw_menu() {
    TOTAL=$(_menu_count)
    I=1
    while [ "${I}" -le "${TOTAL}" ]; do
        LINE=$(_menu_line "${I}")
        KEY=$(printf '%s' "${LINE}" | cut -d'|' -f1)
        TITLE=$(printf '%s' "${LINE}" | cut -d'|' -f2)
        DESC=$(printf '%s' "${LINE}" | cut -d'|' -f3)

        if [ "${I}" -eq "${CURRENT_INDEX}" ]; then
            if _supports_utf8; then POINTER="›"; else POINTER=">"; fi
            printf "  ${C_GREEN}%s${C_NC} ${C_BOLD}%d.${C_NC} %-21s ${C_DIM}%s${C_NC}\n" "${POINTER}" "${I}" "${TITLE}" "${DESC}"
            MENU_ACTION="${KEY}"
        else
            printf "    ${C_DIM}%d.${C_NC} %-21s ${C_DIM}%s${C_NC}\n" "${I}" "${TITLE}" "${DESC}"
        fi
        I=$((I + 1))
    done
    echo ""
    printf "  ${C_DIM}%s${C_NC}\n" "${LAST_MESSAGE}"
    [ -n "${INPUT_BUFFER}" ] && printf "  ${C_DIM}Atalho digitado:${C_NC} %s\n" "${INPUT_BUFFER}"
}

_list_hosts() {
    if command -v tailscale >/dev/null 2>&1 && command -v jq >/dev/null 2>&1; then
        tailscale status --json 2>/dev/null \
            | jq -r '.Peer | to_entries[] | .value | select(.Online) | .HostName' \
            | sort \
            | while IFS= read -r h; do
                printf '%s' "${h}" | tr -cd 'a-zA-Z0-9._-'; printf '\n'
              done
        return
    fi

    if [ -f "${HOSTS_FILE}" ]; then
        grep -v '^[[:space:]]*#' "${HOSTS_FILE}" | grep -v '^[[:space:]]*$' \
            | while IFS= read -r h; do
                printf '%s' "${h}" | tr -cd 'a-zA-Z0-9._-'; printf '\n'
              done
        return
    fi

    return 1
}

_pick_host() {
    HOSTS=$(_list_hosts 2>/dev/null || true)

    echo ""
    if [ -n "${HOSTS}" ]; then
        printf "  ${C_BOLD}Hosts disponíveis${C_NC}\n\n"
        I=1
        printf '%s\n' "${HOSTS}" | while IFS= read -r h; do
            printf "    %d)  %s\n" "${I}" "${h}"
            I=$((I + 1))
        done
        echo ""
        printf "  Número ou hostname: "
    else
        printf "  Hostname (ex: server-01): "
    fi

    read -r INPUT < /dev/tty
    [ -z "${INPUT}" ] && return 1

    case "${INPUT}" in
        ''|*[!0-9]*) printf '%s' "${INPUT}" | tr -cd 'a-zA-Z0-9._-' ;;
        *) printf '%s\n' "${HOSTS}" | sed -n "${INPUT}p" | tr -cd 'a-zA-Z0-9._-' ;;
    esac
}

_manage_hosts() {
    while true; do
        _header
        printf "  ${C_BOLD}Hosts salvos${C_NC}\n\n"
        if [ -f "${HOSTS_FILE}" ]; then
            grep -v '^[[:space:]]*#' "${HOSTS_FILE}" | grep -v '^[[:space:]]*$' \
                | awk '{printf "    %d)  %s\n", NR, $0}' || echo "    (none)"
        else
            echo "    (none)"
        fi
        echo ""
        echo "    a)  Adicionar host"
        echo "    d)  Remover host"
        echo "    b)  Voltar"
        echo ""
        printf "  Escolha: "
        read -r A < /dev/tty
        case "${A}" in
            a|A)
                printf "  Hostname para adicionar: "
                read -r NH < /dev/tty
                NH=$(printf '%s' "${NH}" | tr -cd 'a-zA-Z0-9._-')
                [ -z "${NH}" ] && continue
                touch "${HOSTS_FILE}"
                printf '%s\n' "${NH}" >> "${HOSTS_FILE}"
                LAST_MESSAGE="Host ${NH} adicionado."
            ;;
            d|D)
                if [ ! -f "${HOSTS_FILE}" ]; then
                    LAST_MESSAGE="Nenhum host salvo para remover."
                    continue
                fi
                printf "  Linha para remover: "
                read -r DN < /dev/tty
                case "${DN}" in
                    ''|*[!0-9]*) LAST_MESSAGE="Linha inválida."; continue ;;
                esac
                sed -i "${DN}d" "${HOSTS_FILE}" 2>/dev/null || true
                LAST_MESSAGE="Linha ${DN} removida."
            ;;
            b|B|'') return ;;
            *) LAST_MESSAGE="Escolha inválida em hosts." ;;
        esac
    done
}

_run_action() {
    case "${1}" in
        connect)
            _header
            HOST=$(_pick_host) || { LAST_MESSAGE="Conexão cancelada."; return; }
            [ -z "${HOST}" ] && { LAST_MESSAGE="Nenhum host selecionado."; return; }
            printf '\n  Conectando em %s...\n\n' "${HOST}"
            ssh "${HOST}" || LAST_MESSAGE="Falha ao conectar em ${HOST}."
            printf '\n  Pressione Enter para voltar...'
            read -r _D < /dev/tty
            LAST_MESSAGE="Pronto para a próxima conexão."
        ;;
        hosts)
            _manage_hosts
        ;;
        radar)
            _header
            if ! command -v tailscale >/dev/null 2>&1; then
                printf '\n  Tailscale não instalado. Rode: pocket tailscale-setup\n'
                LAST_MESSAGE="Radar indisponível sem tailscale."
            elif ! is_tailscale_daemon_running 2>/dev/null; then
                printf '\n  tailscaled não está rodando. Rode: pocket tailscale-start\n'
                LAST_MESSAGE="Inicie o daemon para usar o radar."
            else
                sh "${HOME}/.pocketcli/radar.sh" 2>/dev/null || printf '\n  Radar indisponível.\n'
                LAST_MESSAGE="Radar executado."
            fi
            printf '\n  Pressione Enter para voltar...'
            read -r _D < /dev/tty
        ;;
        update)
            _header
            printf '\n  Atualizando PocketCli...\n\n'
            git -C "${HOME}/.pocketcli" pull --ff-only \
                && LAST_MESSAGE="PocketCli atualizado com sucesso." \
                || LAST_MESSAGE="Falha ao atualizar PocketCli."
            printf '\n  Pressione Enter para voltar...'
            read -r _D < /dev/tty
        ;;
        exit)
            _screen_clear
            printf '\n  Até logo.\n\n'
            exit 0
        ;;
    esac
}

_read_key() {
    stty -echo -icanon min 1 time 0 < /dev/tty
    KEY=$(dd bs=1 count=1 2>/dev/null < /dev/tty || true)
    stty sane < /dev/tty 2>/dev/null || true
    printf '%s' "${KEY}"
}

trap 'stty sane < /dev/tty 2>/dev/null || true' EXIT INT TERM

while true; do
    _header
    _draw_menu

    KEY=$(_read_key)
    TOTAL=$(_menu_count)

    case "${KEY}" in
        j)
            INPUT_BUFFER=""
            CURRENT_INDEX=$((CURRENT_INDEX + 1))
            [ "${CURRENT_INDEX}" -gt "${TOTAL}" ] && CURRENT_INDEX=1
        ;;
        k)
            INPUT_BUFFER=""
            CURRENT_INDEX=$((CURRENT_INDEX - 1))
            [ "${CURRENT_INDEX}" -lt 1 ] && CURRENT_INDEX=${TOTAL}
        ;;
        g)
            if [ "${INPUT_BUFFER}" = "g" ]; then
                CURRENT_INDEX=1
                INPUT_BUFFER=""
            else
                INPUT_BUFFER="g"
                LAST_MESSAGE="Pressione g novamente para ir ao topo."
            fi
        ;;
        G)
            INPUT_BUFFER=""
            CURRENT_INDEX=${TOTAL}
        ;;
        h)
            INPUT_BUFFER=""
            CURRENT_INDEX=1
        ;;
        l|'')
            INPUT_BUFFER=""
            _run_action "${MENU_ACTION}"
        ;;
        q)
            INPUT_BUFFER=""
            _run_action exit
        ;;
        [1-9])
            INPUT_BUFFER="${KEY}"
            if [ "${KEY}" -le "${TOTAL}" ]; then
                CURRENT_INDEX=${KEY}
                _run_action "$(_menu_line "${CURRENT_INDEX}" | cut -d'|' -f1)"
                INPUT_BUFFER=""
            else
                LAST_MESSAGE="Atalho ${KEY} não existe."
            fi
        ;;
        *)
            INPUT_BUFFER=""
            LAST_MESSAGE="Tecla ${KEY} não mapeada. Use j/k, Enter, q, gg ou G."
        ;;
    esac

done

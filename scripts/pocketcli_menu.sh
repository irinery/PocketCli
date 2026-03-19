#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/pocketcli_menu.sh
# Lightweight dashboard/TUI for SSH-first workflows on constrained devices.
# Pure POSIX sh + stty, tuned for iPad/iSH and tmux-heavy usage.
# =============================================================================

set -eu

POCKETCLI_DIR="${HOME}/.pocketcli"
. "${POCKETCLI_DIR}/lib/common.sh"

export PATH="${POCKETCLI_DIR}:${PATH}"

HOSTS_FILE="${POCKETCLI_DIR}/hosts"
CURRENT_INDEX=1
MENU_ACTION=""
LAST_MESSAGE="Use j/k para navegar, Enter para abrir, h/l para panes, q para sair."
INPUT_BUFFER=""
TERM_WIDTH=$( (stty size < /dev/tty 2>/dev/null || printf '24 80') | awk '{print $2}' )
[ -z "${TERM_WIDTH}" ] && TERM_WIDTH=80
PANEL_WIDTH=34
[ "${TERM_WIDTH}" -lt 76 ] && PANEL_WIDTH=30

_menu_items() {
    cat <<'ITEMS'
connect|Conectar agora|Escolha host salvo ou peer online e abra SSH
radar|Radar da malha|Lista peers Tailscale e disponibilidade atual
status|Status local|Resumo confiável de disco, memória e rede
hosts|Hosts favoritos|Adicione ou remova atalhos rápidos
update|Atualizar PocketCli|Git pull com feedback e retorno rápido
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

_trim() {
    printf '%s' "$1" | awk '{ sub(/^[[:space:]]+/, ""); sub(/[[:space:]]+$/, ""); print }'
}

_fit() {
    TEXT=$(printf '%s' "$1" | tr '\n' ' ')
    WIDTH="$2"
    printf '%s' "${TEXT}" | awk -v width="${WIDTH}" '
        {
            text=$0
            if (length(text) <= width) {
                printf "%s", text
            } else if (width > 1) {
                printf "%s…", substr(text, 1, width - 1)
            }
        }
    '
}

_repeat_char() {
    CHAR="$1"
    COUNT="$2"
    i=0
    while [ "${i}" -lt "${COUNT}" ]; do
        printf '%s' "${CHAR}"
        i=$((i + 1))
    done
}

_box_top() {
    TITLE="$1"
    WIDTH="$2"
    INNER=$((WIDTH - 2))
    TITLE_FMT=" $( _fit "${TITLE}" $((INNER - 1)) )"
    TITLE_LEN=$(printf '%s' "${TITLE_FMT}" | awk '{ print length }')
    FILL=$((INNER - TITLE_LEN))
    [ "${FILL}" -lt 0 ] && FILL=0
    if _supports_utf8; then
        printf '╭%s%s╮\n' "${TITLE_FMT}" "$( _repeat_char '─' "${FILL}" )"
    else
        printf '+%s%s+\n' "${TITLE_FMT}" "$( _repeat_char '-' "${FILL}" )"
    fi
}

_box_sep() {
    WIDTH="$1"
    INNER=$((WIDTH - 2))
    if _supports_utf8; then
        printf '├%s┤\n' "$( _repeat_char '─' "${INNER}" )"
    else
        printf '+%s+\n' "$( _repeat_char '-' "${INNER}" )"
    fi
}

_box_bottom() {
    WIDTH="$1"
    INNER=$((WIDTH - 2))
    if _supports_utf8; then
        printf '╰%s╯\n' "$( _repeat_char '─' "${INNER}" )"
    else
        printf '+%s+\n' "$( _repeat_char '-' "${INNER}" )"
    fi
}

_box_line() {
    WIDTH="$1"
    LABEL="$2"
    VALUE="$3"
    INNER=$((WIDTH - 2))

    if [ -n "${LABEL}" ]; then
        LABEL_FMT=$( _fit "${LABEL}" 11 )
        VALUE_FMT=$( _fit "${VALUE}" $((INNER - 13)) )
        CONTENT=$(printf '%-11s %s' "${LABEL_FMT}" "${VALUE_FMT}")
    else
        CONTENT=$( _fit "${VALUE}" "${INNER}" )
    fi

    CONTENT=$(printf '%-*s' "${INNER}" "${CONTENT}")
    if _supports_utf8; then
        printf '│%s│\n' "${CONTENT}"
    else
        printf '|%s|\n' "${CONTENT}"
    fi
}

_collect_hostname() {
    hostname 2>/dev/null | tr -cd '[:alnum:]-. '
}

_collect_mem() {
    if command -v free >/dev/null 2>&1; then
        free -m | awk '/^Mem:/ { printf "%sMB/%sMB", $3, $2 }'
    elif command -v vm_stat >/dev/null 2>&1; then
        PAGES_ACTIVE=$(vm_stat 2>/dev/null | awk '/Pages active/ {gsub(/\./,"",$3); print $3}')
        PAGES_FREE=$(vm_stat 2>/dev/null | awk '/Pages free/ {gsub(/\./,"",$3); print $3}')
        if [ -n "${PAGES_ACTIVE:-}" ] && [ -n "${PAGES_FREE:-}" ]; then
            ACTIVE_MB=$((PAGES_ACTIVE * 4096 / 1024 / 1024))
            FREE_MB=$((PAGES_FREE * 4096 / 1024 / 1024))
            printf '%sMB ativo %sMB livre' "${ACTIVE_MB}" "${FREE_MB}"
        else
            printf 'n/a'
        fi
    else
        printf 'n/a'
    fi
}

_collect_disk() {
    df -h / 2>/dev/null | awk 'NR==2 { print $4 " livre / " $2 " total" }'
}

_collect_load() {
    uptime 2>/dev/null | awk -F'load average[s]*:' '{print $2}' | cut -d',' -f1 | tr -d ' '
}

_collect_ts_ip() {
    get_tailscale_ip 2>/dev/null | head -1
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

_collect_peer_count() {
    if command -v tailscale >/dev/null 2>&1 && command -v jq >/dev/null 2>&1; then
        tailscale status --json 2>/dev/null | jq -r '.Peer | length' 2>/dev/null || printf '0'
    else
        printf '0'
    fi
}

_collect_online_count() {
    if command -v tailscale >/dev/null 2>&1 && command -v jq >/dev/null 2>&1; then
        tailscale status --json 2>/dev/null | jq -r '[.Peer | to_entries[] | .value | select(.Online)] | length' 2>/dev/null || printf '0'
    else
        COUNT=$(_list_hosts 2>/dev/null | awk 'NF {count += 1} END {print count + 0}')
        printf '%s' "${COUNT}"
    fi
}

_collect_focus_host() {
    _list_hosts 2>/dev/null | head -1
}

_probe_focus_host() {
    HOST="$(_collect_focus_host)"
    [ -z "${HOST}" ] && { printf 'sem host salvo'; return; }

    if ping_host "${HOST}" 2; then
        printf '%s OK' "${HOST}"
    else
        printf '%s lento/offline' "${HOST}"
    fi
}

_tmux_prefix() {
    if [ -n "${TMUX:-}" ]; then
        printf 'Ctrl+S ativo'
    else
        printf 'Ctrl+S tmux'
    fi
}

_render_header() {
    _screen_clear
    printf '\n'
    if _supports_utf8; then
        printf '  %b╭──────────────────────────────────────────────────────────────╮%b\n' "${C_CYAN}" "${C_NC}"
        printf '  %b│%b %bPocketCli Control Deck%b %b· SSH rápido para iPad e tmux%b %b│%b\n' "${C_CYAN}" "${C_NC}" "${C_BOLD}" "${C_NC}" "${C_DIM}" "${C_NC}" "${C_CYAN}" "${C_NC}"
        printf '  %b╰──────────────────────────────────────────────────────────────╯%b\n' "${C_CYAN}" "${C_NC}"
    else
        printf '  %b+--------------------------------------------------------------+%b\n' "${C_CYAN}" "${C_NC}"
        printf '  %b|%b %bPocketCli Control Deck%b %b- SSH rapido para iPad e tmux%b %b|%b\n' "${C_CYAN}" "${C_NC}" "${C_BOLD}" "${C_NC}" "${C_DIM}" "${C_NC}" "${C_CYAN}" "${C_NC}"
        printf '  %b+--------------------------------------------------------------+%b\n' "${C_CYAN}" "${C_NC}"
    fi
    printf '\n'
}

_render_dashboard() {
    HOSTNAME=$(_collect_hostname)
    TS_IP=$(_collect_ts_ip)
    [ -z "${TS_IP}" ] && TS_IP='offline'
    MEM=$(_collect_mem)
    DISK=$(_collect_disk)
    [ -z "${DISK}" ] && DISK='n/a'
    LOAD=$(_collect_load)
    [ -z "${LOAD}" ] && LOAD='n/a'
    ONLINE=$(_collect_online_count)
    TOTAL=$(_collect_peer_count)
    PROBE=$(_probe_focus_host)
    TMUX_HINT=$(_tmux_prefix)

    LEFT_FILE=$(mktemp)
    RIGHT_FILE=$(mktemp)

    {
        _box_top 'nó local' "${PANEL_WIDTH}"
        _box_line "${PANEL_WIDTH}" 'hostname' "${HOSTNAME:-desconhecido}"
        _box_line "${PANEL_WIDTH}" 'tailscale' "${TS_IP}"
        _box_line "${PANEL_WIDTH}" 'memória' "${MEM}"
        _box_line "${PANEL_WIDTH}" 'disco' "${DISK}"
        _box_line "${PANEL_WIDTH}" 'load 1m' "${LOAD}"
        _box_bottom "${PANEL_WIDTH}"
    } > "${LEFT_FILE}"

    {
        _box_top 'fluxo ssh/tmux' "${PANEL_WIDTH}"
        _box_line "${PANEL_WIDTH}" 'peer online' "${ONLINE}/${TOTAL} visíveis"
        _box_line "${PANEL_WIDTH}" 'host foco' "${PROBE}"
        _box_line "${PANEL_WIDTH}" 'pane nav' 'h j k l'
        _box_line "${PANEL_WIDTH}" 'split' '| e -'
        _box_line "${PANEL_WIDTH}" 'prefixo' "${TMUX_HINT}"
        _box_bottom "${PANEL_WIDTH}"
    } > "${RIGHT_FILE}"

    paste -d ' ' "${LEFT_FILE}" "${RIGHT_FILE}" | sed 's/^/  /'
    rm -f "${LEFT_FILE}" "${RIGHT_FILE}"
    printf '\n'
}

_draw_menu() {
    TOTAL=$(_menu_count)
    I=1
    printf '  %bAções rápidas%b\n\n' "${C_BOLD}" "${C_NC}"
    while [ "${I}" -le "${TOTAL}" ]; do
        LINE=$(_menu_line "${I}")
        KEY=$(printf '%s' "${LINE}" | cut -d'|' -f1)
        TITLE=$(printf '%s' "${LINE}" | cut -d'|' -f2)
        DESC=$(printf '%s' "${LINE}" | cut -d'|' -f3)

        if [ "${I}" -eq "${CURRENT_INDEX}" ]; then
            if _supports_utf8; then POINTER='›'; else POINTER='>'; fi
            printf '  %b%s%b %b%d.%b %-20s %b%s%b\n' "${C_GREEN}" "${POINTER}" "${C_NC}" "${C_BOLD}" "${I}" "${C_NC}" "${TITLE}" "${C_DIM}" "${DESC}" "${C_NC}"
            MENU_ACTION="${KEY}"
        else
            printf '    %b%d.%b %-20s %b%s%b\n' "${C_DIM}" "${I}" "${C_NC}" "${TITLE}" "${C_DIM}" "${DESC}" "${C_NC}"
        fi
        I=$((I + 1))
    done
    printf '\n'
    printf '  %bAtalhos úteis%b\n' "${C_BOLD}" "${C_NC}"
    printf '    Enter/l abrir  ·  j/k mover  ·  gg topo  ·  G fim  ·  h foco panes  ·  q sair\n\n'
    printf '  %b%s%b\n' "${C_DIM}" "${LAST_MESSAGE}" "${C_NC}"
    [ -n "${INPUT_BUFFER}" ] && printf '  %bSequência:%b %s\n' "${C_DIM}" "${C_NC}" "${INPUT_BUFFER}"
}

_pick_host() {
    HOSTS=$(_list_hosts 2>/dev/null || true)

    echo ""
    if [ -n "${HOSTS}" ]; then
        printf '  %bHosts disponíveis%b\n\n' "${C_BOLD}" "${C_NC}"
        I=1
        printf '%s\n' "${HOSTS}" | while IFS= read -r h; do
            printf '    %d)  %s\n' "${I}" "${h}"
            I=$((I + 1))
        done
        echo ""
        printf '  Número ou hostname: '
    else
        printf '  Hostname (ex: server-01): '
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
        _render_header
        printf '  %bHosts favoritos%b\n\n' "${C_BOLD}" "${C_NC}"
        if [ -f "${HOSTS_FILE}" ]; then
            grep -v '^[[:space:]]*#' "${HOSTS_FILE}" | grep -v '^[[:space:]]*$' \
                | awk '{printf "    %d)  %s\n", NR, $0}' || echo '    (none)'
        else
            echo '    (none)'
        fi
        echo ""
        echo '    a)  Adicionar host'
        echo '    d)  Remover host'
        echo '    b)  Voltar'
        echo ""
        printf '  Escolha: '
        read -r A < /dev/tty
        case "${A}" in
            a|A)
                printf '  Hostname para adicionar: '
                read -r NH < /dev/tty
                NH=$(printf '%s' "${NH}" | tr -cd 'a-zA-Z0-9._-')
                [ -z "${NH}" ] && continue
                touch "${HOSTS_FILE}"
                if ! grep -qx "${NH}" "${HOSTS_FILE}" 2>/dev/null; then
                    printf '%s\n' "${NH}" >> "${HOSTS_FILE}"
                    LAST_MESSAGE="Host ${NH} adicionado ao acesso rápido."
                else
                    LAST_MESSAGE="Host ${NH} já existia na lista."
                fi
            ;;
            d|D)
                if [ ! -f "${HOSTS_FILE}" ]; then
                    LAST_MESSAGE='Nenhum host salvo para remover.'
                    continue
                fi
                printf '  Linha para remover: '
                read -r DN < /dev/tty
                case "${DN}" in
                    ''|*[!0-9]*) LAST_MESSAGE='Linha inválida.'; continue ;;
                esac
                sed -i "${DN}d" "${HOSTS_FILE}" 2>/dev/null || true
                LAST_MESSAGE="Linha ${DN} removida."
            ;;
            b|B|'') return ;;
            *) LAST_MESSAGE='Escolha inválida em hosts.' ;;
        esac
    done
}

_run_action() {
    case "$1" in
        connect)
            _render_header
            HOST=$(_pick_host) || { LAST_MESSAGE='Conexão cancelada.'; return; }
            [ -z "${HOST}" ] && { LAST_MESSAGE='Nenhum host selecionado.'; return; }
            printf '\n  Conectando em %s...\n\n' "${HOST}"
            ssh "${HOST}" || LAST_MESSAGE="Falha ao conectar em ${HOST}."
            printf '\n  Pressione Enter para voltar...'
            read -r _D < /dev/tty
            LAST_MESSAGE="Sessão ${HOST} encerrada. Pronto para a próxima conexão."
        ;;
        radar)
            _render_header
            if ! command -v tailscale >/dev/null 2>&1; then
                printf '\n  Tailscale não instalado. Rode: pocket tailscale-setup\n'
                LAST_MESSAGE='Radar indisponível sem tailscale.'
            elif ! is_tailscale_daemon_running 2>/dev/null && ! is_ish; then
                printf '\n  tailscaled não está rodando. Rode: pocket tailscale-start\n'
                LAST_MESSAGE='Inicie o daemon para usar o radar.'
            else
                sh "${HOME}/.pocketcli/radar.sh" 2>/dev/null || printf '\n  Radar indisponível.\n'
                LAST_MESSAGE='Radar executado.'
            fi
            printf '\n  Pressione Enter para voltar...'
            read -r _D < /dev/tty
        ;;
        status)
            _render_header
            sh "${POCKETCLI_DIR}/scripts/pocket-status.sh"
            LAST_MESSAGE='Status local renderizado.'
            printf '  Pressione Enter para voltar...'
            read -r _D < /dev/tty
        ;;
        hosts)
            _manage_hosts
        ;;
        update)
            _render_header
            printf '\n  Atualizando PocketCli...\n\n'
            git -C "${HOME}/.pocketcli" pull --ff-only \
                && LAST_MESSAGE='PocketCli atualizado com sucesso.' \
                || LAST_MESSAGE='Falha ao atualizar PocketCli.'
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
    _render_header
    _render_dashboard
    _draw_menu

    KEY=$(_read_key)
    TOTAL=$(_menu_count)

    case "${KEY}" in
        j)
            INPUT_BUFFER=''
            CURRENT_INDEX=$((CURRENT_INDEX + 1))
            [ "${CURRENT_INDEX}" -gt "${TOTAL}" ] && CURRENT_INDEX=1
        ;;
        k)
            INPUT_BUFFER=''
            CURRENT_INDEX=$((CURRENT_INDEX - 1))
            [ "${CURRENT_INDEX}" -lt 1 ] && CURRENT_INDEX=${TOTAL}
        ;;
        g)
            if [ "${INPUT_BUFFER}" = 'g' ]; then
                CURRENT_INDEX=1
                INPUT_BUFFER=''
            else
                INPUT_BUFFER='g'
                LAST_MESSAGE='Pressione g novamente para ir ao topo.'
            fi
        ;;
        G)
            INPUT_BUFFER=''
            CURRENT_INDEX=${TOTAL}
        ;;
        h)
            INPUT_BUFFER=''
            LAST_MESSAGE='No tmux use Ctrl+S + h/j/k/l para alternar panes sem tocar na tela.'
        ;;
        l|'')
            INPUT_BUFFER=''
            _run_action "${MENU_ACTION}"
        ;;
        q)
            INPUT_BUFFER=''
            _run_action exit
        ;;
        [1-9])
            INPUT_BUFFER="${KEY}"
            if [ "${KEY}" -le "${TOTAL}" ]; then
                CURRENT_INDEX=${KEY}
                _run_action "$( _menu_line "${CURRENT_INDEX}" | cut -d'|' -f1 )"
                INPUT_BUFFER=''
            else
                LAST_MESSAGE="Atalho ${KEY} não existe."
            fi
        ;;
        *)
            INPUT_BUFFER=''
            LAST_MESSAGE='Tecla não mapeada. Use j/k, Enter, q, gg, G ou h.'
        ;;
    esac
done

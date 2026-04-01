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

CURRENT_INDEX=1
MENU_ACTION=""
LAST_MESSAGE="Use j/k para navegar, Enter para abrir, h/l para panes, q para sair."
INPUT_BUFFER=""
TERM_WIDTH=80
TERM_HEIGHT=24
PANEL_WIDTH=34
DASHBOARD_LAYOUT="split"
MESH_TOTAL=""
MESH_ONLINE=""
MESH_LOADED=0
PROBE_CACHE=""
PROBE_CACHE_TS=0

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
    if command -v clear >/dev/null 2>&1 && clear >/dev/null 2>&1; then
        printf ''
    else
        printf '\033[2J\033[H'
    fi
}

_refresh_terminal_size() {
    SIZE=$(stty size 2>/dev/null || true)
    [ -n "${SIZE}" ] || SIZE='24 80'
    TERM_HEIGHT=$(printf '%s' "${SIZE}" | awk '{print $1}')
    TERM_WIDTH=$(printf '%s' "${SIZE}" | awk '{print $2}')

    [ -n "${TERM_HEIGHT}" ] || TERM_HEIGHT=24
    [ -n "${TERM_WIDTH}" ] || TERM_WIDTH=80

    if [ "${TERM_WIDTH}" -ge 92 ]; then
        DASHBOARD_LAYOUT="split"
        PANEL_WIDTH=$(( (TERM_WIDTH - 6) / 2 ))
        [ "${PANEL_WIDTH}" -gt 44 ] && PANEL_WIDTH=44
        [ "${PANEL_WIDTH}" -lt 28 ] && PANEL_WIDTH=28
    elif [ "${TERM_WIDTH}" -ge 60 ]; then
        DASHBOARD_LAYOUT="stack"
        PANEL_WIDTH=$((TERM_WIDTH - 4))
    else
        DASHBOARD_LAYOUT="compact"
        PANEL_WIDTH=$((TERM_WIDTH - 2))
    fi

    if [ "${PANEL_WIDTH}" -lt 22 ]; then
        PANEL_WIDTH=22
    fi

    return 0
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
    list_known_hosts
}

_load_mesh_counts() {
    if [ "${MESH_LOADED}" -eq 1 ]; then
        return
    fi

    MESH_TOTAL=""
    MESH_ONLINE=""

    if command -v tailscale >/dev/null 2>&1 && command -v jq >/dev/null 2>&1; then
        TS_STATUS=$(with_timeout 3 tailscale status --json 2>/dev/null || true)
        if [ -n "${TS_STATUS}" ]; then
            MESH_TOTAL=$(printf '%s\n' "${TS_STATUS}" | jq -r '.Peer | length' 2>/dev/null || true)
            MESH_ONLINE=$(printf '%s\n' "${TS_STATUS}" | jq -r '[.Peer | to_entries[] | .value | select(.Online)] | length' 2>/dev/null || true)
        fi
    fi

    if [ -z "${MESH_TOTAL}" ] || [ -z "${MESH_ONLINE}" ]; then
        HOST_COUNT=$(_list_hosts 2>/dev/null | awk 'NF {count += 1} END {print count + 0}')
        MESH_TOTAL="${HOST_COUNT}"
        MESH_ONLINE="${HOST_COUNT}"
    fi

    MESH_LOADED=1
}

_collect_peer_count() {
    _load_mesh_counts
    printf '%s' "${MESH_TOTAL}"
}

_collect_online_count() {
    _load_mesh_counts
    printf '%s' "${MESH_ONLINE}"
}

_collect_focus_host() {
    _list_hosts 2>/dev/null | head -1 || true
}

_now_epoch() {
    date '+%s' 2>/dev/null || printf '0'
}

_probe_focus_host() {
    MODE="$1"
    HOST="$(_collect_focus_host || true)"
    [ -z "${HOST}" ] && { printf 'sem host salvo'; return; }

    if [ "${MODE}" = "compact" ]; then
        printf '%s' "${HOST}"
        return
    fi

    NOW=$(_now_epoch)
    if [ "${PROBE_CACHE_TS}" -gt 0 ] && [ $((NOW - PROBE_CACHE_TS)) -lt 15 ] && [ -n "${PROBE_CACHE}" ]; then
        printf '%s' "${PROBE_CACHE}"
        return
    fi

    if ping_host "${HOST}" 2; then
        PROBE_CACHE="${HOST} OK"
    else
        PROBE_CACHE="${HOST} lento/offline"
    fi
    PROBE_CACHE_TS="${NOW}"
    printf '%s' "${PROBE_CACHE}"
}

_tmux_prefix() {
    if [ -n "${TMUX:-}" ]; then
        printf 'Ctrl+S ativo'
    else
        printf 'Ctrl+S tmux'
    fi
}

_render_header() {
    _refresh_terminal_size
    _screen_clear
    printf '\n'
    if [ "${TERM_WIDTH}" -lt 62 ]; then
        printf '  %bPocketCli%b %bSSH/tmux%b\n' "${C_BOLD}" "${C_NC}" "${C_DIM}" "${C_NC}"
    elif _supports_utf8; then
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
    MESH_LOADED=0
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
    PROBE=$(_probe_focus_host "${DASHBOARD_LAYOUT}")
    TMUX_HINT=$(_tmux_prefix)

    if [ "${DASHBOARD_LAYOUT}" = "split" ]; then
        TMP_LEFT_FILE=$(mktemp "${TMPDIR:-/tmp}/pocketcli-left.XXXXXX")
        TMP_RIGHT_FILE=$(mktemp "${TMPDIR:-/tmp}/pocketcli-right.XXXXXX")

        {
            _box_top 'nó local' "${PANEL_WIDTH}"
            _box_line "${PANEL_WIDTH}" 'hostname' "${HOSTNAME:-desconhecido}"
            _box_line "${PANEL_WIDTH}" 'tailscale' "${TS_IP}"
            _box_line "${PANEL_WIDTH}" 'memória' "${MEM}"
            _box_line "${PANEL_WIDTH}" 'disco' "${DISK}"
            _box_line "${PANEL_WIDTH}" 'load 1m' "${LOAD}"
            _box_bottom "${PANEL_WIDTH}"
        } > "${TMP_LEFT_FILE}"

        {
            _box_top 'fluxo ssh/tmux' "${PANEL_WIDTH}"
            _box_line "${PANEL_WIDTH}" 'peer online' "${ONLINE}/${TOTAL} visíveis"
            _box_line "${PANEL_WIDTH}" 'host foco' "${PROBE}"
            _box_line "${PANEL_WIDTH}" 'pane nav' 'h j k l'
            _box_line "${PANEL_WIDTH}" 'split' '| e -'
            _box_line "${PANEL_WIDTH}" 'prefixo' "${TMUX_HINT}"
            _box_bottom "${PANEL_WIDTH}"
        } > "${TMP_RIGHT_FILE}"

        paste -d ' ' "${TMP_LEFT_FILE}" "${TMP_RIGHT_FILE}" | sed 's/^/  /'
        rm -f "${TMP_LEFT_FILE}" "${TMP_RIGHT_FILE}"
    else
        _box_top 'nó local' "${PANEL_WIDTH}" | sed 's/^/  /'
        _box_line "${PANEL_WIDTH}" 'hostname' "${HOSTNAME:-desconhecido}" | sed 's/^/  /'
        _box_line "${PANEL_WIDTH}" 'tailscale' "${TS_IP}" | sed 's/^/  /'
        _box_line "${PANEL_WIDTH}" 'memória' "${MEM}" | sed 's/^/  /'
        _box_line "${PANEL_WIDTH}" 'disco' "${DISK}" | sed 's/^/  /'
        _box_line "${PANEL_WIDTH}" 'load 1m' "${LOAD}" | sed 's/^/  /'
        _box_bottom "${PANEL_WIDTH}" | sed 's/^/  /'
        printf '\n'
        _box_top 'fluxo ssh/tmux' "${PANEL_WIDTH}" | sed 's/^/  /'
        _box_line "${PANEL_WIDTH}" 'peer online' "${ONLINE}/${TOTAL} visíveis" | sed 's/^/  /'
        _box_line "${PANEL_WIDTH}" 'host foco' "${PROBE}" | sed 's/^/  /'
        _box_line "${PANEL_WIDTH}" 'pane nav' 'h j k l' | sed 's/^/  /'
        _box_line "${PANEL_WIDTH}" 'split' '| e -' | sed 's/^/  /'
        _box_line "${PANEL_WIDTH}" 'prefixo' "${TMUX_HINT}" | sed 's/^/  /'
        _box_bottom "${PANEL_WIDTH}" | sed 's/^/  /'
    fi
    printf '\n'
}

_draw_menu() {
    TOTAL=$(_menu_count)
    I=1
    TITLE_WIDTH=20
    [ "${TERM_WIDTH}" -lt 72 ] && TITLE_WIDTH=14
    DESC_WIDTH=$((TERM_WIDTH - TITLE_WIDTH - 18))
    [ "${DESC_WIDTH}" -lt 12 ] && DESC_WIDTH=12
    printf '  %bAções rápidas%b\n\n' "${C_BOLD}" "${C_NC}"
    while [ "${I}" -le "${TOTAL}" ]; do
        LINE=$(_menu_line "${I}")
        KEY=$(printf '%s' "${LINE}" | cut -d'|' -f1)
        TITLE=$(printf '%s' "${LINE}" | cut -d'|' -f2)
        DESC=$(printf '%s' "${LINE}" | cut -d'|' -f3)

        if [ "${I}" -eq "${CURRENT_INDEX}" ]; then
            if _supports_utf8; then POINTER='›'; else POINTER='>'; fi
            printf '  %b%s%b %b%d.%b %-*s %b%s%b\n' "${C_GREEN}" "${POINTER}" "${C_NC}" "${C_BOLD}" "${I}" "${C_NC}" "${TITLE_WIDTH}" "$(_fit "${TITLE}" "${TITLE_WIDTH}")" "${C_DIM}" "$(_fit "${DESC}" "${DESC_WIDTH}")" "${C_NC}"
            MENU_ACTION="${KEY}"
        else
            printf '    %b%d.%b %-*s %b%s%b\n' "${C_DIM}" "${I}" "${C_NC}" "${TITLE_WIDTH}" "$(_fit "${TITLE}" "${TITLE_WIDTH}")" "${C_DIM}" "$(_fit "${DESC}" "${DESC_WIDTH}")" "${C_NC}"
        fi
        I=$((I + 1))
    done
    printf '\n'
    printf '  %bAtalhos úteis%b\n' "${C_BOLD}" "${C_NC}"
    if [ "${TERM_WIDTH}" -lt 72 ]; then
        printf '    Enter/l abrir  ·  j/k mover  ·  q sair\n'
        printf '    gg topo  ·  G fim  ·  h foco panes\n\n'
    else
        printf '    Enter/l abrir  ·  j/k mover  ·  gg topo  ·  G fim  ·  h foco panes  ·  q sair\n\n'
    fi
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
        HOSTS_FILE="${POCKETCLI_DIR}/hosts"
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
                if _remove_host_line "${HOSTS_FILE}" "${DN}"; then
                    LAST_MESSAGE="Linha ${DN} removida."
                else
                    case "$?" in
                        2) LAST_MESSAGE='Nenhum host salvo para remover.' ;;
                        3) LAST_MESSAGE="Linha ${DN} não existe." ;;
                        *) LAST_MESSAGE='Falha ao salvar a remoção do host.' ;;
                    esac
                fi
            ;;
            b|B|'') return ;;
            *) LAST_MESSAGE='Escolha inválida em hosts.' ;;
        esac
    done
}

_remove_host_line() {
    HOSTS_FILE="$1"
    LINE_NO="$2"

    [ -f "${HOSTS_FILE}" ] || return 2

    TOTAL_LINES=$(grep -v '^[[:space:]]*#' "${HOSTS_FILE}" | grep -v '^[[:space:]]*$' | awk 'END { print NR + 0 }')
    [ "${TOTAL_LINES}" -gt 0 ] || return 2
    [ "${LINE_NO}" -ge 1 ] && [ "${LINE_NO}" -le "${TOTAL_LINES}" ] || return 3

    TMP_FILE=$(mktemp "${HOSTS_FILE}.tmp.XXXXXX") || return 4
    if awk -v target="${LINE_NO}" '
        /^[[:space:]]*#/ { next }
        /^[[:space:]]*$/ { next }
        { count += 1 }
        count != target { print }
    ' "${HOSTS_FILE}" > "${TMP_FILE}" && mv "${TMP_FILE}" "${HOSTS_FILE}"; then
        return 0
    fi

    rm -f "${TMP_FILE}"
    return 4
}

_pause_for_user() {
    MESSAGE=${1:-'  Pressione Enter para voltar...'}
    printf '%b' "${MESSAGE}"
    stty sane < /dev/tty 2>/dev/null || true
    read -r _D < /dev/tty || true
}

_run_with_pause() {
    SUCCESS_MESSAGE=${1:-}
    FAILURE_MESSAGE=${2:-}
    PAUSE_MESSAGE=${3:-'\n  Pressione Enter para voltar...'}
    shift 3

    _render_header

    set +e
    "$@"
    RC=$?
    set -e

    if [ "${RC}" -eq 0 ]; then
        [ -n "${SUCCESS_MESSAGE}" ] && LAST_MESSAGE="${SUCCESS_MESSAGE}"
    else
        if [ -n "${FAILURE_MESSAGE}" ]; then
            LAST_MESSAGE=$(printf '%s' "${FAILURE_MESSAGE}" | sed "s/%s/${RC}/")
        else
            LAST_MESSAGE="Ação finalizada com falha (exit ${RC})."
        fi
    fi

    _pause_for_user "${PAUSE_MESSAGE}"
    return 0
}

_run_action() {
    case "$1" in
        connect)
            HOST=$(_pick_host) || { LAST_MESSAGE='Conexão cancelada.'; return; }
            [ -z "${HOST}" ] && { LAST_MESSAGE='Nenhum host selecionado.'; return; }
            _run_with_pause \
                "Sessão ${HOST} encerrada. Pronto para a próxima conexão." \
                "Falha ao conectar em ${HOST} (exit %s)." \
                '\n  Pressione Enter para voltar...' \
                sh -c 'printf "\n  Conectando em %s...\n\n" "$1"; exec ssh "$1"' sh "${HOST}"
        ;;
        radar)
            if ! command -v tailscale >/dev/null 2>&1; then
                _render_header
                printf '\n  Tailscale não instalado. Rode: pocket tailscale-setup\n'
                LAST_MESSAGE='Radar indisponível sem tailscale.'
                _pause_for_user '\n  Pressione Enter para voltar...'
            elif ! is_tailscale_daemon_running 2>/dev/null && ! is_ish; then
                _render_header
                printf '\n  tailscaled não está rodando. Rode: pocket tailscale-start\n'
                LAST_MESSAGE='Inicie o daemon para usar o radar.'
                _pause_for_user '\n  Pressione Enter para voltar...'
            else
                _run_with_pause \
                    'Radar executado.' \
                    'Radar indisponível (exit %s).' \
                    '\n  Pressione Enter para voltar...' \
                    sh "${HOME}/.pocketcli/radar.sh"
            fi
        ;;
        status)
            _run_with_pause \
                'Status local renderizado.' \
                'Falha ao renderizar status local (exit %s).' \
                '  Pressione Enter para voltar...' \
                sh "${POCKETCLI_DIR}/scripts/pocket-status.sh"
        ;;
        hosts)
            _manage_hosts
        ;;
        update)
            _run_with_pause \
                'PocketCli atualizado com sucesso.' \
                'Falha ao atualizar PocketCli (exit %s).' \
                '\n  Pressione Enter para voltar...' \
                sh -c 'printf "\n  Atualizando PocketCli...\n\n"; exec "$1/pocket" update' sh "${HOME}/.pocketcli"
        ;;
        exit)
            _screen_clear
            printf '\n  Até logo.\n\n'
            exit 0
        ;;
    esac
}

_read_key() {
    stty -echo -icanon min 1 time 0 < /dev/tty 2>/dev/null || true
    KEY=$(dd bs=1 count=1 2>/dev/null < /dev/tty || true)
    stty sane < /dev/tty 2>/dev/null || true
    printf '%s' "${KEY}"
}

trap 'stty sane 2>/dev/null || true' EXIT INT TERM

if [ "${POCKETCLI_MENU_RENDER_ONCE:-0}" = "1" ]; then
    _render_header
    _render_dashboard
    _draw_menu
    exit 0
fi

if [ ! -r /dev/tty ]; then
    printf '[PocketCli] interactive terminal not available for menu mode.\n' >&2
    exit 0
fi

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
        l)
            INPUT_BUFFER=''
            _run_action "${MENU_ACTION}"
        ;;
        "$(printf '\r')")
            INPUT_BUFFER=''
            _run_action "${MENU_ACTION}"
        ;;
        '')
            true
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

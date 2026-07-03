#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/pocketcli_menu.sh
# Lightweight dashboard/TUI for SSH-first workflows on constrained devices.
# Pure POSIX sh + stty, tuned for iPad/iSH and tmux-heavy usage.
# =============================================================================

set -eu

POCKETCLI_DIR="${HOME}/.pocketcli"
. "${POCKETCLI_DIR}/lib/common.sh"

TUI_ENGINE_FILE="${POCKETCLI_DIR}/scripts/layout/tui_engine.sh"
if [ -r "${TUI_ENGINE_FILE}" ]; then
    # shellcheck disable=SC1090 # Installed path is resolved from POCKETCLI_DIR.
    . "${TUI_ENGINE_FILE}"
    HAVE_TUI_ENGINE=1
else
    HAVE_TUI_ENGINE=0
fi

export PATH="${POCKETCLI_DIR}:${PATH}"

CURRENT_INDEX=1
CURRENT_SCREEN="main"
SUB_INDEX=1
MENU_ACTION=""
SUB_ACTION=""
LAST_MESSAGE="Use j/k para navegar, Enter para abrir, h/l para panes, q para sair."
INPUT_BUFFER=""
TUI_INPUT_HISTORY_LIMIT=50
TERM_WIDTH=80
TERM_HEIGHT=24
PANEL_WIDTH=34
DASHBOARD_LAYOUT="split"
MESH_TOTAL=""
MESH_ONLINE=""
MESH_LOADED=0
PROBE_CACHE=""
PROBE_CACHE_TS=0
MENU_RENDERED=0
MENU_START_ROW=1
MENU_STATE_FILE=""
MENU_PREFIX_STATE_FILE=""
LAST_TERM_WIDTH=""
LAST_TERM_HEIGHT=""
LAST_PANEL_WIDTH=""
LAST_DASHBOARD_LAYOUT=""

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
        if clear >/dev/null 2>&1; then
            printf ''
            return 0
        fi
    fi
    printf '\033[2J\033[H'
}

_cursor_move() {
    printf '\033[%s;%sH' "$1" "$2"
}

_clear_line() {
    printf '\033[2K'
}

_tui_input_history_file() {
    printf '%s/state/tui-input-history' "${POCKETCLI_DIR}"
}

_tui_input_sanitize() {
    printf '%s' "$1" | tr -cd 'a-zA-Z0-9._-'
}

_tui_string_chop_last() {
    printf '%s\n' "$1" | awk '{ print substr($0, 1, length($0) - 1); exit }'
}

_tui_suggestion_for() {
    CURRENT=$(_tui_input_sanitize "$1")
    CANDIDATES_FILE="$2"
    HISTORY_FILE=$(_tui_input_history_file)

    {
        [ -f "${HISTORY_FILE}" ] && cat "${HISTORY_FILE}"
        [ -f "${CANDIDATES_FILE}" ] && cat "${CANDIDATES_FILE}"
    } 2>/dev/null | awk -v current="${CURRENT}" '
        {
            value = $0
            gsub(/[^A-Za-z0-9._-]/, "", value)
            if (value == "") {
                next
            }
            if (seen[value]++) {
                next
            }
            if (current == "") {
                print value
                exit
            }
            if (index(value, current) == 1 && value != current) {
                print value
                exit
            }
        }
    '
}

_tui_record_input() {
    VALUE=$(_tui_input_sanitize "$1")
    [ -n "${VALUE}" ] || return 0

    HISTORY_FILE=$(_tui_input_history_file)
    HISTORY_DIR=$(dirname "${HISTORY_FILE}")
    mkdir -p "${HISTORY_DIR}" 2>/dev/null || return 0
    chmod 700 "${HISTORY_DIR}" 2>/dev/null || true

    TMP_FILE=$(mktemp "${HISTORY_FILE}.tmp.XXXXXX") || return 0
    {
        printf '%s\n' "${VALUE}"
        if [ -f "${HISTORY_FILE}" ]; then
            awk -v value="${VALUE}" '($0 != value) { print }' "${HISTORY_FILE}"
        fi
    } | awk -v limit="${TUI_INPUT_HISTORY_LIMIT}" '
        NF && !seen[$0]++ {
            print
            count += 1
            if (count >= limit) {
                exit
            }
        }
    ' > "${TMP_FILE}"

    chmod 600 "${TMP_FILE}" 2>/dev/null || true
    mv "${TMP_FILE}" "${HISTORY_FILE}" 2>/dev/null || rm -f "${TMP_FILE}"
}

_line_count_file() {
    awk 'END { print NR + 0 }' "$1"
}

_ensure_menu_state_file() {
    if [ -n "${MENU_STATE_FILE}" ] && [ -f "${MENU_STATE_FILE}" ]; then
        return 0
    fi

    MENU_STATE_FILE=$(mktemp "${TMPDIR:-/tmp}/pocketcli-menu-state.XXXXXX")
    : > "${MENU_STATE_FILE}"
}

_ensure_menu_prefix_file() {
    if [ -n "${MENU_PREFIX_STATE_FILE}" ] && [ -f "${MENU_PREFIX_STATE_FILE}" ]; then
        return 0
    fi

    MENU_PREFIX_STATE_FILE=$(mktemp "${TMPDIR:-/tmp}/pocketcli-menu-prefix.XXXXXX")
    : > "${MENU_PREFIX_STATE_FILE}"
}

_store_block_file() {
    SRC_FILE="$1"
    DST_FILE="$2"
    cat "${SRC_FILE}" > "${DST_FILE}"
}

_emit_block_diff() {
    OLD_FILE="$1"
    NEW_FILE="$2"
    START_ROW="$3"

    OLD_LINES=$(_line_count_file "${OLD_FILE}")
    NEW_LINES=$(_line_count_file "${NEW_FILE}")
    MAX_LINES="${OLD_LINES}"
    [ "${NEW_LINES}" -gt "${MAX_LINES}" ] && MAX_LINES="${NEW_LINES}"

    I=1
    while [ "${I}" -le "${MAX_LINES}" ]; do
        OLD_LINE=$(sed -n "${I}p" "${OLD_FILE}" 2>/dev/null || true)
        NEW_LINE=$(sed -n "${I}p" "${NEW_FILE}" 2>/dev/null || true)

        if [ "${OLD_LINE}" != "${NEW_LINE}" ]; then
            _cursor_move $((START_ROW + I - 1)) 1
            _clear_line
            printf '%s' "${NEW_LINE}"
        fi

        I=$((I + 1))
    done
}

_refresh_terminal_size() {
    if [ "${HAVE_TUI_ENGINE:-0}" -eq 1 ] && [ "${TUI_INITIALIZED:-0}" -eq 1 ]; then
        tui_refresh_size
        TERM_HEIGHT="${TUI_ROWS}"
        TERM_WIDTH="${TUI_COLS}"
    else
        SIZE=$(stty size < /dev/tty 2>/dev/null || stty size 2>/dev/null || true)
        [ -n "${SIZE}" ] || SIZE='24 80'
        TERM_HEIGHT=$(printf '%s' "${SIZE}" | awk '{print $1}')
        TERM_WIDTH=$(printf '%s' "${SIZE}" | awk '{print $2}')
    fi

    [ -n "${TERM_HEIGHT}" ] || TERM_HEIGHT=24
    [ -n "${TERM_WIDTH}" ] || TERM_WIDTH=80

    if [ "${TERM_WIDTH}" -ge 72 ]; then
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
    [ "${POCKETCLI_TUI_TEST_ASCII:-0}" = "1" ] && return 1
    [ "${POCKETCLI_TUI_TEST_UTF8:-0}" = "1" ] && return 0
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
    ELLIPSIS='…'
    [ "${POCKETCLI_TUI_TEST_ASCII:-0}" = "1" ] && ELLIPSIS='.'
    printf '%s' "${TEXT}" | awk -v width="${WIDTH}" -v ellipsis="${ELLIPSIS}" '
        {
            text=$0
            if (length(text) <= width) {
                printf "%s", text
            } else if (width > 1) {
                printf "%s%s", substr(text, 1, width - 1), ellipsis
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
    if [ "${POCKETCLI_TUI_TEST_MODE:-0}" = "1" ]; then
        printf 'test-node.local'
        return
    fi
    hostname 2>/dev/null | tr -cd '[:alnum:]-. '
}

_collect_mem() {
    if [ "${POCKETCLI_TUI_TEST_MODE:-0}" = "1" ]; then
        printf '512MB/1024MB'
        return
    fi
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
    if [ "${POCKETCLI_TUI_TEST_MODE:-0}" = "1" ]; then
        printf '10G livre / 20G total'
        return
    fi
    df -h / 2>/dev/null | awk 'NR==2 { print $4 " livre / " $2 " total" }'
}

_collect_load() {
    if [ "${POCKETCLI_TUI_TEST_MODE:-0}" = "1" ]; then
        printf '0.00'
        return
    fi
    uptime 2>/dev/null | awk -F'load average[s]*:' '{print $2}' | cut -d',' -f1 | tr -d ' '
}

_collect_ts_ip() {
    if [ "${POCKETCLI_TUI_TEST_MODE:-0}" = "1" ]; then
        printf '100.64.0.1'
        return
    fi
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

    if [ "${POCKETCLI_TUI_TEST_MODE:-0}" = "1" ]; then
        MESH_TOTAL=5
        MESH_ONLINE=3
        MESH_LOADED=1
        return
    fi

    if has_tailscale_cli; then
        if command -v jq >/dev/null 2>&1; then
            TS_STATUS=$(tailscale_status_json 2>/dev/null || true)
            if [ -n "${TS_STATUS}" ]; then
                MESH_TOTAL=$(printf '%s\n' "${TS_STATUS}" | jq -r '.Peer | length' 2>/dev/null || true)
                MESH_ONLINE=$(printf '%s\n' "${TS_STATUS}" | jq -r '[.Peer | to_entries[] | .value | select(.Online)] | length' 2>/dev/null || true)
            fi
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
    if [ "${POCKETCLI_TUI_TEST_MODE:-0}" = "1" ]; then
        printf 'apple-tv'
        return
    fi
    _list_hosts 2>/dev/null | head -1 || true
}

_now_epoch() {
    date '+%s' 2>/dev/null || printf '0'
}

_probe_focus_host() {
    MODE="$1"
    HOST="$(_collect_focus_host || true)"
    [ -z "${HOST}" ] && { printf 'sem host salvo'; return; }

    if [ "${POCKETCLI_TUI_TEST_MODE:-0}" = "1" ]; then
        printf '%s OK' "${HOST}"
        return
    fi

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
    if [ "${POCKETCLI_TUI_TEST_MODE:-0}" = "1" ]; then
        printf 'Ctrl+S tmux'
        return
    fi
    if [ -n "${TMUX:-}" ]; then
        printf 'Ctrl+S ativo'
    else
        printf 'Ctrl+S tmux'
    fi
}

_render_header() {
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
}

_render_header_screen() {
    _refresh_terminal_size
    _screen_clear
    _render_header
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
    if [ "${TERM_WIDTH}" -lt 88 ]; then
        printf '    Enter/l abrir  ·  j/k mover  ·  q sair\n'
        printf '    gg topo  ·  G fim  ·  h foco panes\n\n'
    else
        printf '    Enter/l abrir  ·  j/k mover  ·  gg topo  ·  G fim  ·  h foco panes  ·  q sair\n\n'
    fi
    printf '  %b%s%b\n' "${C_DIM}" "${LAST_MESSAGE}" "${C_NC}"
    if [ -n "${INPUT_BUFFER}" ]; then
        printf '  %bSequência:%b %s\n' "${C_DIM}" "${C_NC}" "${INPUT_BUFFER}"
    fi
    return 0
}

_radar_submenu_items() {
    cat <<'ITEMS'
radar-run|Executar radar|Abrir listagem completa de peers
back|Voltar|Retornar às ações rápidas
ITEMS
}

_radar_submenu_count() {
    _radar_submenu_items | awk 'END { print NR }'
}

_radar_submenu_line() {
    IDX="$1"
    _radar_submenu_items | sed -n "${IDX}p"
}

_draw_radar_submenu() {
    ONLINE=$(_collect_online_count)
    TOTAL=$(_collect_peer_count)
    TS_IP=$(_collect_ts_ip)
    [ -z "${TS_IP}" ] && TS_IP='offline'
    FOCUS=$(_probe_focus_host "${DASHBOARD_LAYOUT}")
    TITLE_WIDTH=18
    DESC_WIDTH=$((TERM_WIDTH - TITLE_WIDTH - 18))
    [ "${DESC_WIDTH}" -lt 12 ] && DESC_WIDTH=12

    printf '  %bRadar da malha%b\n\n' "${C_BOLD}" "${C_NC}"
    printf '    peers       %s/%s visíveis\n' "${ONLINE}" "${TOTAL}"
    printf '    tailscale   %s\n' "${TS_IP}"
    printf '    foco        %s\n\n' "${FOCUS}"

    TOTAL_ITEMS=$(_radar_submenu_count)
    I=1
    while [ "${I}" -le "${TOTAL_ITEMS}" ]; do
        LINE=$(_radar_submenu_line "${I}")
        KEY=$(printf '%s' "${LINE}" | cut -d'|' -f1)
        TITLE=$(printf '%s' "${LINE}" | cut -d'|' -f2)
        DESC=$(printf '%s' "${LINE}" | cut -d'|' -f3)

        if [ "${I}" -eq "${SUB_INDEX}" ]; then
            if _supports_utf8; then POINTER='›'; else POINTER='>'; fi
            printf '  %b%s%b %b%d.%b %-*s %b%s%b\n' "${C_GREEN}" "${POINTER}" "${C_NC}" "${C_BOLD}" "${I}" "${C_NC}" "${TITLE_WIDTH}" "$(_fit "${TITLE}" "${TITLE_WIDTH}")" "${C_DIM}" "$(_fit "${DESC}" "${DESC_WIDTH}")" "${C_NC}"
            SUB_ACTION="${KEY}"
        else
            printf '    %b%d.%b %-*s %b%s%b\n' "${C_DIM}" "${I}" "${C_NC}" "${TITLE_WIDTH}" "$(_fit "${TITLE}" "${TITLE_WIDTH}")" "${C_DIM}" "$(_fit "${DESC}" "${DESC_WIDTH}")" "${C_NC}"
        fi
        I=$((I + 1))
    done

    printf '\n'
    printf '  %bAtalhos úteis%b\n' "${C_BOLD}" "${C_NC}"
    printf '    Enter/l abrir  ·  j/k mover  ·  q/Backspace voltar\n'
    printf '  %b%s%b\n' "${C_DIM}" "Use q para voltar às ações rápidas." "${C_NC}"
}

_draw_current_screen() {
    case "${CURRENT_SCREEN}" in
        radar) _draw_radar_submenu ;;
        *) _draw_menu ;;
    esac
}

_position_menu_cursor() {
    if [ "${HAVE_TUI_ENGINE:-0}" -eq 1 ] && [ "${TUI_INITIALIZED:-0}" -eq 1 ]; then
        tui_cursor_move $((MENU_START_ROW + CURRENT_INDEX + 1)) 3
    else
        _cursor_move $((MENU_START_ROW + CURRENT_INDEX + 1)) 3
    fi
}

_render_menu_block_to_file() {
    TARGET_FILE="$1"
    _draw_current_screen > "${TARGET_FILE}"
}

_render_frame_prefix_to_file() {
    TARGET_FILE="$1"
    {
        _render_header
        _render_dashboard
    } > "${TARGET_FILE}"
}

_remember_frame_geometry() {
    LAST_TERM_WIDTH="${TERM_WIDTH}"
    LAST_TERM_HEIGHT="${TERM_HEIGHT}"
    LAST_PANEL_WIDTH="${PANEL_WIDTH}"
    LAST_DASHBOARD_LAYOUT="${DASHBOARD_LAYOUT}"
}

_frame_geometry_changed() {
    [ "${MENU_RENDERED}" -eq 1 ] || return 0
    [ "${TERM_WIDTH}" = "${LAST_TERM_WIDTH}" ] || return 0
    [ "${TERM_HEIGHT}" = "${LAST_TERM_HEIGHT}" ] || return 0
    [ "${PANEL_WIDTH}" = "${LAST_PANEL_WIDTH}" ] || return 0
    [ "${DASHBOARD_LAYOUT}" = "${LAST_DASHBOARD_LAYOUT}" ] || return 0
    return 1
}

_render_full_frame() {
    _refresh_terminal_size
    _ensure_menu_state_file
    _ensure_menu_prefix_file

    PREFIX_FILE=$(mktemp "${TMPDIR:-/tmp}/pocketcli-frame.XXXXXX")
    MENU_FILE=$(mktemp "${TMPDIR:-/tmp}/pocketcli-menu.XXXXXX")
    FRAME_FILE=$(mktemp "${TMPDIR:-/tmp}/pocketcli-full-frame.XXXXXX")

    _render_frame_prefix_to_file "${PREFIX_FILE}"
    _render_menu_block_to_file "${MENU_FILE}"

    MENU_START_ROW=$(( $(_line_count_file "${PREFIX_FILE}") + 1 ))

    cat "${PREFIX_FILE}" "${MENU_FILE}" > "${FRAME_FILE}"

    if [ "${HAVE_TUI_ENGINE:-0}" -eq 1 ] && [ "${TUI_INITIALIZED:-0}" -eq 1 ]; then
        tui_render_frame "${FRAME_FILE}"
    else
        _screen_clear
        cat "${FRAME_FILE}"
    fi

    _store_block_file "${PREFIX_FILE}" "${MENU_PREFIX_STATE_FILE}"
    _store_block_file "${MENU_FILE}" "${MENU_STATE_FILE}"
    _remember_frame_geometry
    MENU_RENDERED=1
    _position_menu_cursor

    rm -f "${PREFIX_FILE}" "${MENU_FILE}" "${FRAME_FILE}"
}

_render_menu_incremental() {
    _refresh_terminal_size
    _ensure_menu_state_file

    if [ "${CURRENT_SCREEN}" != "main" ] || _frame_geometry_changed; then
        _render_full_frame
        return 0
    fi

    MENU_FILE=$(mktemp "${TMPDIR:-/tmp}/pocketcli-menu.XXXXXX")
    _render_menu_block_to_file "${MENU_FILE}"
    if [ "${HAVE_TUI_ENGINE:-0}" -eq 1 ] && [ "${TUI_INITIALIZED:-0}" -eq 1 ]; then
        tui_render_diff "${MENU_FILE}" "${MENU_START_ROW}" "${MENU_STATE_FILE}"
    else
        _emit_block_diff "${MENU_STATE_FILE}" "${MENU_FILE}" "${MENU_START_ROW}"
    fi
    _store_block_file "${MENU_FILE}" "${MENU_STATE_FILE}"
    _position_menu_cursor
    rm -f "${MENU_FILE}"
}

_cleanup_menu_state() {
    [ -n "${MENU_STATE_FILE}" ] && rm -f "${MENU_STATE_FILE}" 2>/dev/null || true
    [ -n "${MENU_PREFIX_STATE_FILE}" ] && rm -f "${MENU_PREFIX_STATE_FILE}" 2>/dev/null || true
}

tui_app_cleanup() {
    _cleanup_menu_state
}

_pick_host() {
    HOSTS=$(_list_hosts 2>/dev/null || true)
    CANDIDATES_FILE=$(mktemp "${TMPDIR:-/tmp}/pocketcli-host-candidates.XXXXXX")
    [ -n "${HOSTS}" ] && printf '%s\n' "${HOSTS}" > "${CANDIDATES_FILE}"

    printf '\n' > /dev/tty
    if [ -n "${HOSTS}" ]; then
        printf '  %bHosts disponíveis%b\n\n' "${C_BOLD}" "${C_NC}" > /dev/tty
        I=1
        printf '%s\n' "${HOSTS}" | while IFS= read -r h; do
            printf '    %d)  %s\n' "${I}" "${h}" > /dev/tty
            I=$((I + 1))
        done
        printf '\n' > /dev/tty
        PROMPT='  Número ou hostname: '
    else
        PROMPT='  Hostname (ex: server-01): '
    fi

    INPUT=$(_tui_prompt_with_suggestions "${PROMPT}" "${CANDIDATES_FILE}") || {
        rm -f "${CANDIDATES_FILE}"
        return 1
    }
    rm -f "${CANDIDATES_FILE}"
    [ -z "${INPUT}" ] && return 1

    case "${INPUT}" in
        ''|*[!0-9]*) HOST=$(_tui_input_sanitize "${INPUT}") ;;
        *) HOST=$(printf '%s\n' "${HOSTS}" | sed -n "${INPUT}p" | tr -cd 'a-zA-Z0-9._-') ;;
    esac
    [ -n "${HOST}" ] || return 1
    _tui_record_input "${HOST}"
    printf '%s' "${HOST}"
}

_manage_hosts() {
    _leave_tui_for_action
    while true; do
        _render_header_screen
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
            b|B|'') _enter_tui || return 1; return ;;
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

_enter_tui() {
    if [ "${HAVE_TUI_ENGINE:-0}" -ne 1 ]; then
        printf '[PocketCli] TUI engine not found at %s.\n' "${TUI_ENGINE_FILE}" >&2
        return 1
    fi
    [ "${TUI_INITIALIZED:-0}" -eq 1 ] && return 0
    tui_init
}

_leave_tui_for_action() {
    if [ "${HAVE_TUI_ENGINE:-0}" -eq 1 ] && [ "${TUI_INITIALIZED:-0}" -eq 1 ]; then
        tui_suspend
    fi
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

    _leave_tui_for_action
    _render_header_screen

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
    _enter_tui || return 1
    return 0
}

_run_action() {
    case "$1" in
        connect)
            _leave_tui_for_action
            _render_header_screen
            HOST=$(_pick_host) || { LAST_MESSAGE='Conexão cancelada.'; _enter_tui || return 1; return; }
            [ -z "${HOST}" ] && { LAST_MESSAGE='Nenhum host selecionado.'; _enter_tui || return 1; return; }
            _run_with_pause \
                "Sessão ${HOST} encerrada. Pronto para a próxima conexão." \
                "Falha ao conectar em ${HOST} (exit %s)." \
                '\n  Pressione Enter para voltar...' \
                sh -c "printf '\n  Conectando em %s...\n\n' \"\$1\"; exec ssh \"\$1\"" sh "${HOST}"
        ;;
        radar-run)
            _run_with_pause \
                'Radar executado.' \
                'Radar indisponível (exit %s).' \
                '\n  Pressione Enter para voltar...' \
                sh "${HOME}/.pocketcli/radar.sh"
        ;;
        radar)
            CURRENT_SCREEN="radar"
            SUB_INDEX=1
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
                sh -c "printf '\n  Atualizando PocketCli...\n\n'; exec \"\$1/pocket\" update" sh "${HOME}/.pocketcli"
        ;;
        exit)
            _leave_tui_for_action
            _screen_clear
            printf '\n  Até logo.\n\n'
            exit 0
        ;;
    esac
}

_read_key() {
    if [ "${HAVE_TUI_ENGINE:-0}" -eq 1 ] && [ "${TUI_INITIALIZED:-0}" -eq 1 ]; then
        tui_read_key
        return
    fi

    stty -echo -icanon min 1 time 0 < /dev/tty 2>/dev/null || true
    KEY=$(dd bs=1 count=1 2>/dev/null < /dev/tty || true)
    stty sane < /dev/tty 2>/dev/null || true
    printf '%s' "${KEY}"
}

_read_prompt_key() {
    PROMPT_TAB=$(printf '\011')
    PROMPT_CR=$(printf '\015')
    PROMPT_LF=$(printf '\012')
    PROMPT_DEL=$(printf '\177')
    PROMPT_BS=$(printf '\010')
    PROMPT_ESC=$(printf '\033')

    stty -echo -icanon min 1 time 0 < /dev/tty 2>/dev/null || true
    BYTE=$(dd bs=1 count=1 2>/dev/null < /dev/tty || true)
    stty sane < /dev/tty 2>/dev/null || true

    case "${BYTE}" in
        "${PROMPT_CR}"|"${PROMPT_LF}") printf 'ENTER' ;;
        "${PROMPT_TAB}") printf 'TAB' ;;
        "${PROMPT_DEL}"|"${PROMPT_BS}") printf 'BACKSPACE' ;;
        "${PROMPT_ESC}") printf 'ESC' ;;
        *) printf '%s' "${BYTE}" ;;
    esac
}

_tui_prompt_with_suggestions() {
    PROMPT="$1"
    CANDIDATES_FILE="$2"
    INPUT_VALUE=""

    while true; do
        SUGGESTION=$(_tui_suggestion_for "${INPUT_VALUE}" "${CANDIDATES_FILE}")
        printf '\r\033[2K%s%s' "${PROMPT}" "${INPUT_VALUE}" > /dev/tty
        if [ -n "${SUGGESTION}" ]; then
            printf '  %bTab:%b %s' "${C_DIM}" "${C_NC}" "${SUGGESTION}" > /dev/tty
        fi

        KEY=$(_read_prompt_key)
        case "${KEY}" in
            ENTER)
                printf '\n' > /dev/tty
                printf '%s' "${INPUT_VALUE}"
                return 0
            ;;
            TAB)
                [ -n "${SUGGESTION}" ] && INPUT_VALUE="${SUGGESTION}"
            ;;
            BACKSPACE)
                [ -n "${INPUT_VALUE}" ] && INPUT_VALUE=$(_tui_string_chop_last "${INPUT_VALUE}")
            ;;
            ESC)
                printf '\n' > /dev/tty
                return 1
            ;;
            '')
                true
            ;;
            *)
                CLEAN_KEY=$(_tui_input_sanitize "${KEY}")
                [ -n "${CLEAN_KEY}" ] && INPUT_VALUE="${INPUT_VALUE}${CLEAN_KEY}"
            ;;
        esac
    done
}

_handle_main_key() {
    KEY="$1"
    TOTAL=$(_menu_count)
    NEXT_RENDER=menu

    case "${KEY}" in
        j|DOWN)
            INPUT_BUFFER=''
            CURRENT_INDEX=$((CURRENT_INDEX + 1))
            [ "${CURRENT_INDEX}" -gt "${TOTAL}" ] && CURRENT_INDEX=1
        ;;
        k|UP)
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
        h|LEFT)
            INPUT_BUFFER=''
            LAST_MESSAGE='No tmux use Ctrl+S + h/j/k/l para alternar panes sem tocar na tela.'
        ;;
        l|RIGHT|ENTER)
            INPUT_BUFFER=''
            _run_action "${MENU_ACTION}"
            NEXT_RENDER=full
        ;;
        '')
            NEXT_RENDER=none
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
                NEXT_RENDER=full
            else
                LAST_MESSAGE="Atalho ${KEY} não existe."
            fi
        ;;
        *)
            INPUT_BUFFER=''
            NEXT_RENDER=none
        ;;
    esac
    return 0
}

_handle_radar_key() {
    KEY="$1"
    TOTAL=$(_radar_submenu_count)
    NEXT_RENDER=full

    case "${KEY}" in
        j|DOWN)
            SUB_INDEX=$((SUB_INDEX + 1))
            [ "${SUB_INDEX}" -gt "${TOTAL}" ] && SUB_INDEX=1
        ;;
        k|UP)
            SUB_INDEX=$((SUB_INDEX - 1))
            [ "${SUB_INDEX}" -lt 1 ] && SUB_INDEX=${TOTAL}
        ;;
        q|BACKSPACE|LEFT)
            CURRENT_SCREEN="main"
            NEXT_RENDER=full
        ;;
        l|RIGHT|ENTER)
            case "${SUB_ACTION}" in
                radar-run)
                    _run_action radar-run
                    NEXT_RENDER=full
                ;;
                back)
                    CURRENT_SCREEN="main"
                    NEXT_RENDER=full
                ;;
            esac
        ;;
        '')
            NEXT_RENDER=none
        ;;
        *)
            NEXT_RENDER=none
        ;;
    esac
    return 0
}

if [ "${POCKETCLI_MENU_RENDER_ONCE:-0}" = "1" ]; then
    _render_full_frame
    exit 0
fi

if { ! stty -g < /dev/tty >/dev/null 2>&1; } && { [ ! -t 0 ] || [ ! -t 1 ]; }; then
    printf '[PocketCli] interactive terminal not available for menu mode.\n' >&2
    exit 0
fi

_enter_tui || exit 1

RENDER_MODE=full

while true; do
    if [ "${TUI_RESIZE_PENDING:-0}" -eq 1 ]; then
        TUI_RESIZE_PENDING=0
        MENU_RENDERED=0
        RENDER_MODE=full
    fi

    case "${RENDER_MODE}" in
        full) _render_full_frame ;;
        menu) _render_menu_incremental ;;
        none) true ;;
        *) _render_full_frame ;;
    esac

    KEY=$(_read_key)

    case "${CURRENT_SCREEN}" in
        radar) _handle_radar_key "${KEY}" ;;
        *) _handle_main_key "${KEY}" ;;
    esac

    RENDER_MODE="${NEXT_RENDER}"
done

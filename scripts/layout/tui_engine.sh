#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/layout/tui_engine.sh
# Terminal render engine for pocket menu.
# POSIX sh, backed by tput/stty so it works on macOS, Linux, Alpine and iSH.
# =============================================================================

TUI_MIN_COLS="${TUI_MIN_COLS:-40}"
TUI_MIN_ROWS="${TUI_MIN_ROWS:-10}"
TUI_COLS="${TUI_COLS:-80}"
TUI_ROWS="${TUI_ROWS:-24}"
TUI_INITIALIZED=0
TUI_ALT_SCREEN=0
TUI_CURSOR_HIDDEN=0
TUI_RAW_MODE=0
TUI_RESIZE_PENDING=0
TUI_USE_DEV_TTY=1
TUI_STTY_ORIG=""
TUI_LAST_FRAME_FILE=""
TUI_ESC=$(printf '\033')
TUI_CR=$(printf '\r')
TUI_LF=$(printf '\n')
TUI_DEL=$(printf '\177')
TUI_BS=$(printf '\010')

tui_debug() {
    [ "${POCKET_TUI_DEBUG:-0}" = "1" ] || return 0
    TUI_LOG_DIR="${HOME}/.pocketcli/logs"
    mkdir -p "${TUI_LOG_DIR}" 2>/dev/null || return 0
    printf '%s %s\n' "$(date '+%Y-%m-%dT%H:%M:%S%z' 2>/dev/null || date)" "$*" >> "${TUI_LOG_DIR}/tui.log" 2>/dev/null || true
}

tui_line_count_file() {
    awk 'END { print NR + 0 }' "$1"
}

tui_require_tput() {
    if ! command -v tput >/dev/null 2>&1; then
        printf 'tput não encontrado — instale ncurses/terminfo antes de usar o menu TUI.\n' >&2
        return 1
    fi
    return 0
}

tui_detect_tty() {
    if stty -g < /dev/tty >/dev/null 2>&1; then
        TUI_USE_DEV_TTY=1
        return 0
    fi

    if [ -t 0 ] && [ -t 1 ]; then
        TUI_USE_DEV_TTY=0
        return 0
    fi

    printf '[PocketCli] interactive terminal not available for menu mode.\n' >&2
    return 1
}

tui_stty() {
    if [ "${TUI_USE_DEV_TTY}" -eq 1 ]; then
        stty "$@" < /dev/tty
    else
        stty "$@"
    fi
}

tui_tput() {
    if [ "${TUI_USE_DEV_TTY}" -eq 1 ]; then
        tput "$@" > /dev/tty
    else
        tput "$@"
    fi
}

tui_emit() {
    if [ "${TUI_USE_DEV_TTY}" -eq 1 ]; then
        printf '%s' "$1" > /dev/tty
    else
        printf '%s' "$1"
    fi
}

tui_emit_file() {
    if [ "${TUI_USE_DEV_TTY}" -eq 1 ]; then
        awk 'NR > 1 { printf "\r\n" } { printf "%s", $0 }' "$1" > /dev/tty
    else
        awk 'NR > 1 { printf "\r\n" } { printf "%s", $0 }' "$1"
    fi
}

tui_refresh_size() {
    if [ "${TUI_USE_DEV_TTY}" -eq 1 ]; then
        SIZE=$(stty size < /dev/tty 2>/dev/null || true)
    else
        SIZE=$(stty size 2>/dev/null || true)
    fi
    if [ -n "${SIZE}" ]; then
        TUI_ROWS=$(printf '%s' "${SIZE}" | awk '{ print $1 }')
        TUI_COLS=$(printf '%s' "${SIZE}" | awk '{ print $2 }')
    else
        TUI_COLS=$(tput cols 2>/dev/null || printf '80')
        TUI_ROWS=$(tput lines 2>/dev/null || printf '24')
    fi

    case "${TUI_COLS}" in ''|*[!0-9]*) TUI_COLS=80 ;; esac
    case "${TUI_ROWS}" in ''|*[!0-9]*) TUI_ROWS=24 ;; esac

    export TUI_COLS TUI_ROWS
    return 0
}

tui_validate_size() {
    if [ "${TUI_COLS}" -lt "${TUI_MIN_COLS}" ] || [ "${TUI_ROWS}" -lt "${TUI_MIN_ROWS}" ]; then
        printf 'Terminal muito pequeno: mínimo %sx%s, atual %sx%s.\n' \
            "${TUI_MIN_COLS}" "${TUI_MIN_ROWS}" "${TUI_COLS}" "${TUI_ROWS}" >&2
        return 1
    fi
    return 0
}

tui_normalize_frame() {
    SRC_FILE="$1"
    DST_FILE="$2"
    awk -v rows="${TUI_ROWS}" 'NR <= rows { print }' "${SRC_FILE}" > "${DST_FILE}"
}

tui_cursor_move() {
    ROW="$1"
    COL="$2"
    [ "${ROW}" -lt 1 ] && ROW=1
    [ "${COL}" -lt 1 ] && COL=1
    tui_tput cup $((ROW - 1)) $((COL - 1)) 2>/dev/null || true
}

tui_clear_line() {
    tui_tput el 2>/dev/null || tui_emit "$(printf '\033[2K')"
}

tui_clear_screen() {
    tui_tput clear 2>/dev/null || tui_emit "$(printf '\033[2J\033[H')"
}

tui_cleanup_files() {
    [ -n "${TUI_LAST_FRAME_FILE}" ] && rm -f "${TUI_LAST_FRAME_FILE}" 2>/dev/null || true
    TUI_LAST_FRAME_FILE=""
}

tui_restore() {
    [ "${TUI_INITIALIZED}" -eq 1 ] || return 0

    if [ "${TUI_RAW_MODE}" -eq 1 ]; then
        if [ -n "${TUI_STTY_ORIG}" ]; then
            tui_stty "${TUI_STTY_ORIG}" 2>/dev/null || tui_stty sane 2>/dev/null || true
        else
            tui_stty sane 2>/dev/null || true
        fi
        TUI_RAW_MODE=0
    fi

    if [ "${TUI_CURSOR_HIDDEN}" -eq 1 ]; then
        tui_tput cnorm 2>/dev/null || true
        TUI_CURSOR_HIDDEN=0
    fi

    if [ "${TUI_ALT_SCREEN}" -eq 1 ]; then
        tui_tput rmcup 2>/dev/null || true
        TUI_ALT_SCREEN=0
    fi

    TUI_INITIALIZED=0
    tui_debug 'restore terminal'
    return 0
}

tui_trap_exit() {
    if command -v tui_app_cleanup >/dev/null 2>&1; then
        tui_app_cleanup
    fi
    tui_restore
    tui_cleanup_files
}

tui_trap_signal() {
    tui_trap_exit
    exit 1
}

tui_trap_winch() {
    # shellcheck disable=SC2034 # Consumed by scripts/pocketcli_menu.sh after sourcing.
    TUI_RESIZE_PENDING=1
}

tui_init() {
    tui_detect_tty || return 1
    tui_require_tput || return 1
    tui_refresh_size
    tui_validate_size || return 1

    TUI_STTY_ORIG=$(tui_stty -g 2>/dev/null || true)
    if ! tui_tput smcup 2>/dev/null; then
        printf 'Falha ao entrar na tela alternativa do terminal.\n' >&2
        return 1
    fi
    TUI_ALT_SCREEN=1

    tui_tput civis 2>/dev/null || true
    TUI_CURSOR_HIDDEN=1

    if ! tui_stty raw -echo min 0 time 1 2>/dev/null; then
        tui_restore
        printf 'Falha ao ativar modo raw do terminal.\n' >&2
        return 1
    fi
    TUI_RAW_MODE=1
    TUI_INITIALIZED=1

    if [ -z "${TUI_LAST_FRAME_FILE}" ]; then
        TUI_LAST_FRAME_FILE=$(mktemp "${TMPDIR:-/tmp}/pocketcli-tui-frame.XXXXXX")
        : > "${TUI_LAST_FRAME_FILE}"
    fi

    trap 'tui_trap_exit' EXIT
    trap 'tui_trap_signal' INT TERM
    trap 'tui_trap_winch' WINCH

    tui_debug "init cols=${TUI_COLS} rows=${TUI_ROWS}"
    return 0
}

tui_suspend() {
    tui_restore
}

tui_resume() {
    tui_init
}

tui_render_frame() {
    FRAME_FILE="$1"
    NORMALIZED_FILE=$(mktemp "${TMPDIR:-/tmp}/pocketcli-tui-normalized.XXXXXX")
    tui_refresh_size
    tui_normalize_frame "${FRAME_FILE}" "${NORMALIZED_FILE}"

    tui_clear_screen
    tui_cursor_move 1 1
    tui_emit_file "${NORMALIZED_FILE}"
    cat "${NORMALIZED_FILE}" > "${TUI_LAST_FRAME_FILE}"
    tui_debug "render_frame lines=$(tui_line_count_file "${NORMALIZED_FILE}")"
    rm -f "${NORMALIZED_FILE}"
}

tui_render_diff() {
    NEW_FILE="$1"
    START_ROW="${2:-1}"
    OLD_FILE="${3:-${TUI_LAST_FRAME_FILE}}"

    NORMALIZED_NEW=$(mktemp "${TMPDIR:-/tmp}/pocketcli-tui-new.XXXXXX")
    tui_refresh_size
    tui_normalize_frame "${NEW_FILE}" "${NORMALIZED_NEW}"

    OLD_LINES=$(tui_line_count_file "${OLD_FILE}")
    NEW_LINES=$(tui_line_count_file "${NORMALIZED_NEW}")
    MAX_LINES="${OLD_LINES}"
    [ "${NEW_LINES}" -gt "${MAX_LINES}" ] && MAX_LINES="${NEW_LINES}"

    DIRTY=0
    I=1
    while [ "${I}" -le "${MAX_LINES}" ]; do
        ROW=$((START_ROW + I - 1))
        [ "${ROW}" -gt "${TUI_ROWS}" ] && break

        OLD_LINE=$(sed -n "${I}p" "${OLD_FILE}" 2>/dev/null || true)
        NEW_LINE=$(sed -n "${I}p" "${NORMALIZED_NEW}" 2>/dev/null || true)

        if [ "${OLD_LINE}" != "${NEW_LINE}" ]; then
            tui_cursor_move "${ROW}" 1
            tui_clear_line
            tui_emit "${NEW_LINE}"
            DIRTY=$((DIRTY + 1))
        fi

        I=$((I + 1))
    done

    if [ "${START_ROW}" -eq 1 ] && [ "${OLD_FILE}" = "${TUI_LAST_FRAME_FILE}" ]; then
        cat "${NORMALIZED_NEW}" > "${TUI_LAST_FRAME_FILE}"
    fi

    tui_debug "render_diff dirty=${DIRTY} start_row=${START_ROW}"
    rm -f "${NORMALIZED_NEW}"
}

tui_read_byte() {
    if [ "${TUI_USE_DEV_TTY}" -eq 1 ]; then
        dd bs=1 count=1 2>/dev/null < /dev/tty || true
    else
        dd bs=1 count=1 2>/dev/null || true
    fi
}

tui_read_key() {
    BYTE=$(tui_read_byte)
    if [ -z "${BYTE}" ]; then
        return 0
    fi

    if [ "${BYTE}" = "${TUI_CR}" ] || [ "${BYTE}" = "${TUI_LF}" ]; then
        printf 'ENTER'
        return 0
    fi

    if [ "${BYTE}" = "${TUI_DEL}" ] || [ "${BYTE}" = "${TUI_BS}" ]; then
        printf 'BACKSPACE'
        return 0
    fi

    if [ "${BYTE}" = "${TUI_ESC}" ]; then
        SEQ="${BYTE}"
        TUI_KEY='UNKNOWN'
        N=0

        # A lone Escape should not block the menu; arrow-key suffixes get 100 ms.
        tui_stty min 0 time 1 2>/dev/null || {
            printf 'UNKNOWN'
            return 0
        }
        while [ "${N}" -lt 8 ]; do
            NEXT=$(tui_read_byte)
            [ -z "${NEXT}" ] && break
            SEQ="${SEQ}${NEXT}"
            case "${SEQ}" in
                "${TUI_ESC}[A") TUI_KEY='UP'; break ;;
                "${TUI_ESC}[B") TUI_KEY='DOWN'; break ;;
                "${TUI_ESC}[C") TUI_KEY='RIGHT'; break ;;
                "${TUI_ESC}[D") TUI_KEY='LEFT'; break ;;
            esac
            N=$((N + 1))
        done

        tui_stty min 0 time 1 2>/dev/null || true
        printf '%s' "${TUI_KEY:-UNKNOWN}"
        return 0
    fi

    printf '%s' "${BYTE}"
}

tui_handle_key() {
    if command -v pocket_tui_handle_key >/dev/null 2>&1; then
        pocket_tui_handle_key "$1"
        return $?
    fi
    return 0
}

tui_loop() {
    while :; do
        KEY=$(tui_read_key)
        tui_handle_key "${KEY}" || break
    done
}

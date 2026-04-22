#!/usr/bin/env bash

vi_require_terminal() {
    if ! command -v tmux >/dev/null 2>&1; then
        printf 'tmux não encontrado — instale via apt/apk/brew para rodar a suíte visual.\n' >&2
        return 1
    fi

    if ! command -v tput >/dev/null 2>&1; then
        printf 'tput não encontrado — instale ncurses/terminfo para rodar a suíte visual.\n' >&2
        return 1
    fi
}

vi_setup_workspace() {
    VI_WORKDIR=$(mktemp -d "${TMPDIR:-/tmp}/pocketcli-visual.XXXXXX")
    VI_HOME="${VI_WORKDIR}/home"
    export TMUX_TMPDIR="${VI_WORKDIR}/tmux"
    mkdir -p "${TMUX_TMPDIR}"
    chmod 700 "${TMUX_TMPDIR}"
    mkdir -p "${VI_HOME}/.pocketcli"

    cp "${REPO_ROOT}/pocket" "${VI_HOME}/.pocketcli/pocket"
    cp "${REPO_ROOT}/radar.sh" "${VI_HOME}/.pocketcli/radar.sh"
    cp -R "${REPO_ROOT}/scripts" "${VI_HOME}/.pocketcli/scripts"
    cp -R "${REPO_ROOT}/lib" "${VI_HOME}/.pocketcli/lib"
    chmod +x "${VI_HOME}/.pocketcli/pocket" "${VI_HOME}/.pocketcli/scripts/pocketcli_menu.sh"
    printf 'apple-tv\nnas\n' > "${VI_HOME}/.pocketcli/hosts"
}

vi_cleanup_workspace() {
    if [[ -n "${VI_SESSION:-}" ]]; then
        tmux kill-session -t "${VI_SESSION}" >/dev/null 2>&1 || true
    fi
    [[ -n "${VI_WORKDIR:-}" ]] && rm -rf "${VI_WORKDIR}" 2>/dev/null || true
}

vi_start_session() {
    local cols="$1"
    local rows="$2"
    VI_SESSION="pocket-visual-$$-${RANDOM}"
    VI_EXIT_FILE="${VI_WORKDIR}/exit-code"
    VI_MENU_ERR="${REPORT_DIR:-${VI_WORKDIR}}/menu-${VI_SESSION}.err"

    tmux new-session -d -x "${cols}" -y "${rows}" -s "${VI_SESSION}" \
        "env HOME='${VI_HOME}' PATH='${VI_HOME}/.pocketcli:${PATH}' TERM=screen-256color LANG=C.UTF-8 LC_ALL=C.UTF-8 POCKETCLI_TUI_TEST_MODE=1 POCKETCLI_DIRECT_MENU=1 POCKET_TUI_DEBUG=${POCKET_TUI_DEBUG:-0} EXIT_FILE='${VI_EXIT_FILE}' MENU_ERR='${VI_MENU_ERR}' bash -lc '\"${VI_HOME}/.pocketcli/pocket\" menu 2>\"\$MENU_ERR\"; rc=\$?; printf \"%s\\n\" \"\$rc\" > \"\$EXIT_FILE\"; printf \"\\nVISUAL_TEST_PROMPT$ \"; sleep 30'"
}

vi_stop_session() {
    tmux kill-session -t "${VI_SESSION}" >/dev/null 2>&1 || true
    VI_SESSION=""
}

vi_send_key() {
    tmux send-keys -t "${VI_SESSION}" "$@"
}

vi_resize() {
    local cols="$1"
    local rows="$2"
    tmux resize-window -t "${VI_SESSION}" -x "${cols}" -y "${rows}"
}

vi_snapshot() {
    local rows="$1"
    local output="$2"
    local alt_output="${output}.alt"
    local normal_output="${output}.normal"

    tmux capture-pane -p -a -t "${VI_SESSION}" -S 0 -E "$((rows - 1))" 2>/dev/null \
        | sed 's/[[:space:]]*$//' \
        | awk '{ lines[NR] = $0; if ($0 != "") last = NR } END { for (i = 1; i <= last; i++) print lines[i] }' \
        > "${alt_output}" || true

    if [[ -s "${alt_output}" ]]; then
        mv "${alt_output}" "${output}"
        rm -f "${normal_output}"
        return 0
    fi

    tmux capture-pane -p -t "${VI_SESSION}" -S 0 -E "$((rows - 1))" \
        | sed 's/[[:space:]]*$//' \
        | awk '{ lines[NR] = $0; if ($0 != "") last = NR } END { for (i = 1; i <= last; i++) print lines[i] }' \
        > "${normal_output}"
    mv "${normal_output}" "${output}"
    rm -f "${alt_output}"
}

vi_exit_code() {
    if [[ -f "${VI_EXIT_FILE}" ]]; then
        cat "${VI_EXIT_FILE}"
    else
        printf 'null'
    fi
}

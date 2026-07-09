#!/usr/bin/env sh

layout_try_restore_tmux() {
    SESSION="${POCKETCLI_TMUX_SESSION:-pocketcli}"
    if command -v tmux >/dev/null 2>&1; then
        if tmux has-session -t "${SESSION}" 2>/dev/null; then
            exec tmux attach-session -t "${SESSION}"
        fi
    fi
    return 1
}

layout_shell_quote() {
    printf "'"
    printf '%s' "$1" | sed "s/'/'\\\\''/g"
    printf "'"
}

layout_create_restore_tmux() {
    SESSION="${POCKETCLI_TMUX_SESSION:-pocketcli}"
    pocket_cmd=$(layout_shell_quote "${POCKETCLI_DIR}/pocket")

    command -v tmux >/dev/null 2>&1 || return 1

    if tmux has-session -t "${SESSION}" 2>/dev/null; then
        exec tmux attach-session -t "${SESSION}"
    fi

    if [ ! -s "$(pocket_last_command_file)" ]; then
        pocket_save_command menu
    fi

    tmux new-session -d -s "${SESSION}" || {
        if tmux has-session -t "${SESSION}" 2>/dev/null; then
            exec tmux attach-session -t "${SESSION}"
        fi
        return 1
    }
    tmux send-keys -t "${SESSION}" "POCKETCLI_RESTORE=1 ${pocket_cmd} __restore" C-m
    tmux source-file "${HOME}/.config/tmux/tmux.conf" 2>/dev/null || true
    exec tmux attach-session -t "${SESSION}"
}

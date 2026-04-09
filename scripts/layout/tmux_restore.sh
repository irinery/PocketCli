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

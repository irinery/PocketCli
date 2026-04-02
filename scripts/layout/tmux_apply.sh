#!/usr/bin/env sh

layout_apply_tmux_from_spec() {
    SPEC_FILE="$1"
    SESSION_NAME="pocketcli"
    LEFT_CMD=""
    RIGHT_CMD=""

    if command -v jq >/dev/null 2>&1; then
        SESSION_NAME=$(jq -r '.tmux.session_name // "pocketcli"' "${SPEC_FILE}" 2>/dev/null)
        LEFT_CMD=$(jq -r '.panes[0].command // ""' "${SPEC_FILE}" 2>/dev/null)
        RIGHT_CMD=$(jq -r '.panes[1].command // ""' "${SPEC_FILE}" 2>/dev/null)
    fi

    tmux new-session -d -s "${SESSION_NAME}"
    [ -n "${LEFT_CMD}" ] && tmux send-keys -t "${SESSION_NAME}" "${LEFT_CMD}" Enter
    if [ -n "${RIGHT_CMD}" ]; then
        tmux split-window -h -t "${SESSION_NAME}"
        tmux send-keys -t "${SESSION_NAME}" "${RIGHT_CMD}" Enter
    fi
    tmux select-pane -t "${SESSION_NAME}:0.0"
    exec tmux attach-session -t "${SESSION_NAME}"
}

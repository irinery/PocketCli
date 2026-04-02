#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/start_agent.sh
# Agent mode launcher.
# - default: delegate to Session Manager
# - --legacy-direct: apply legacy tmux layout directly (used by manager fallback)
# =============================================================================

set -eu

POCKETCLI_DIR="${HOME}/.pocketcli"

if [ "${1:-}" != "--legacy-direct" ] && [ -x "${POCKETCLI_DIR}/scripts/session/manager.sh" ]; then
    SESSION_INTENT="agent" exec sh "${POCKETCLI_DIR}/scripts/session/manager.sh" agent
fi

LOG_FILE="${POCKETCLI_DEBUG_LOG:-/tmp/pocketcli-debug.log}"
log_debug() {
    TS=$(date '+%Y-%m-%d %H:%M:%S' 2>/dev/null || printf 'unknown-time')
    printf '%s [start_agent] %s\n' "${TS}" "$*" >> "${LOG_FILE}" 2>/dev/null || true
}

SESSION="pocketcli"
log_debug "starting agent session boot session=${SESSION}"
printf '\n[PocketCli] Starting Agent environment...\n'

command -v tmux >/dev/null 2>&1 || { printf '[PocketCli] tmux not found.\n' >&2; exit 1; }

detect_terminal_size() {
    SIZE=$(stty size < /dev/tty 2>/dev/null || printf '30 120')
    ROWS=$(printf '%s' "${SIZE}" | awk '{print $1}')
    COLS=$(printf '%s' "${SIZE}" | awk '{print $2}')
    [ -n "${ROWS}" ] || ROWS=30
    [ -n "${COLS}" ] || COLS=120
    [ "${ROWS}" -lt 18 ] && ROWS=18
    [ "${COLS}" -lt 72 ] && COLS=72
    printf '%s %s\n' "${ROWS}" "${COLS}"
}

agent_left_command() {
    if command -v htop >/dev/null 2>&1; then printf 'htop\n';
    elif command -v top >/dev/null 2>&1; then printf 'top\n';
    else printf 'while true; do date; sleep 5; done\n'; fi
}

agent_right_command() {
    if command -v lazygit >/dev/null 2>&1; then
        if [ -d "${HOME}/.pocketcli/.git" ]; then printf 'cd %s/.pocketcli && lazygit\n' "${HOME}"; else printf 'lazygit\n'; fi
    else
        if [ -d "${HOME}/.pocketcli/.git" ]; then printf 'cd %s/.pocketcli && git status\n' "${HOME}"; else printf 'git status\n'; fi
    fi
}

if tmux has-session -t "${SESSION}" 2>/dev/null; then
    exec tmux attach-session -t "${SESSION}"
fi

TERM_SIZE=$(detect_terminal_size)
TERM_ROWS=${TERM_SIZE%% *}
TERM_COLS=${TERM_SIZE#* }

tmux new-session -d -s "${SESSION}" -x "${TERM_COLS}" -y "${TERM_ROWS}"
tmux send-keys -t "${SESSION}" "$(agent_left_command)" Enter
tmux split-window -h -t "${SESSION}"
tmux send-keys -t "${SESSION}" "$(agent_right_command)" Enter
tmux select-pane -t "${SESSION}:0.0"
tmux source-file "${HOME}/.config/tmux/tmux.conf" 2>/dev/null || true

exec tmux attach-session -t "${SESSION}"

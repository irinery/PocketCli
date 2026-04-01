#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/start_agent.sh
# Starts the Agent mode tmux environment.
# =============================================================================

set -eu

LOG_FILE="${POCKETCLI_DEBUG_LOG:-/tmp/pocketcli-debug.log}"
log_debug() {
    TS=$(date '+%Y-%m-%d %H:%M:%S' 2>/dev/null || printf 'unknown-time')
    printf '%s [start_agent] %s\n' "${TS}" "$*" >> "${LOG_FILE}" 2>/dev/null || true
}

SESSION="pocketcli"
log_debug "starting agent session boot session=${SESSION}"

printf '\n[PocketCli] Starting Agent environment...\n'

# ---------------------------------------------------------------------------
# Guards
# ---------------------------------------------------------------------------
command -v tmux >/dev/null 2>&1 || { printf '[PocketCli] tmux not found.\n' >&2; exit 1; }
log_debug "tmux binary detected"

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
    if command -v htop >/dev/null 2>&1; then
        printf 'htop\n'
    elif command -v top >/dev/null 2>&1; then
        printf 'top\n'
    else
        printf 'while true; do date; sleep 5; done\n'
    fi
}

agent_right_command() {
    if command -v lazygit >/dev/null 2>&1; then
        if [ -d "${HOME}/.pocketcli/.git" ]; then
            printf 'cd %s/.pocketcli && lazygit\n' "${HOME}"
        else
            printf 'lazygit\n'
        fi
    else
        if [ -d "${HOME}/.pocketcli/.git" ]; then
            printf 'cd %s/.pocketcli && git status\n' "${HOME}"
        else
            printf 'git status\n'
        fi
    fi
}

# ---------------------------------------------------------------------------
# If a session already exists, attach to it
# ---------------------------------------------------------------------------
if tmux has-session -t "${SESSION}" 2>/dev/null; then
    log_debug "existing tmux session detected; attaching"
    printf '[PocketCli] Attaching to existing session "%s"...\n' "${SESSION}"
    exec tmux attach-session -t "${SESSION}"
fi

# ---------------------------------------------------------------------------
# Create layout:
#   +---------------------+--------------------+
#   |        htop         |     lazygit        |
#   +---------------------+--------------------+
# ---------------------------------------------------------------------------
set -- $(detect_terminal_size)
TERM_ROWS="$1"
TERM_COLS="$2"
log_debug "detected terminal size rows=${TERM_ROWS} cols=${TERM_COLS}"

tmux new-session  -d -s "${SESSION}" -x "${TERM_COLS}" -y "${TERM_ROWS}"
log_debug "created tmux session"

# Left pane → htop
tmux send-keys    -t "${SESSION}" "$(agent_left_command)" Enter

# Right pane → lazygit (if a git repo exists in HOME, use it)
tmux split-window -h -t "${SESSION}"
tmux send-keys -t "${SESSION}" "$(agent_right_command)" Enter

# Focus left pane
tmux select-pane  -t "${SESSION}:0.0"

# Apply PocketCli tmux config
tmux source-file  "${HOME}/.config/tmux/tmux.conf" 2>/dev/null || true
log_debug "tmux config sourced and attaching"

printf '[PocketCli] Agent environment ready. Attaching...\n\n'
exec tmux attach-session -t "${SESSION}"

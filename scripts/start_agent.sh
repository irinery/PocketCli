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
tmux new-session  -d -s "${SESSION}" -x 220 -y 50
log_debug "created tmux session"

# Left pane → htop
tmux send-keys    -t "${SESSION}" "htop" Enter

# Right pane → lazygit (if a git repo exists in HOME, use it)
tmux split-window -h -t "${SESSION}"
if [ -d "${HOME}/.pocketcli/.git" ]; then
    log_debug "launching lazygit inside repo"
    tmux send-keys -t "${SESSION}" "cd ${HOME}/.pocketcli && lazygit" Enter
else
    log_debug "launching lazygit from current shell"
    tmux send-keys -t "${SESSION}" "lazygit" Enter
fi

# Focus left pane
tmux select-pane  -t "${SESSION}:0.0"

# Apply PocketCli tmux config
tmux source-file  "${HOME}/.config/tmux/tmux.conf" 2>/dev/null || true
log_debug "tmux config sourced and attaching"

printf '[PocketCli] Agent environment ready. Attaching...\n\n'
exec tmux attach-session -t "${SESSION}"

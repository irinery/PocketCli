#!/usr/bin/env bash
# =============================================================================
# PocketCli — scripts/start_agent.sh
# Starts the Agent mode tmux environment.
# =============================================================================

set -euo pipefail

SESSION="pocketcli"

printf '\n[PocketCli] Starting Agent environment...\n'

# ---------------------------------------------------------------------------
# Guards
# ---------------------------------------------------------------------------
command -v tmux >/dev/null 2>&1 || { printf '[PocketCli] tmux not found.\n' >&2; exit 1; }

# ---------------------------------------------------------------------------
# If a session already exists, attach to it
# ---------------------------------------------------------------------------
if tmux has-session -t "${SESSION}" 2>/dev/null; then
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

# Left pane → htop
tmux send-keys    -t "${SESSION}" "htop" Enter

# Right pane → lazygit (if a git repo exists in HOME, use it)
tmux split-window -h -t "${SESSION}"
if [ -d "${HOME}/.pocketcli/.git" ]; then
    tmux send-keys -t "${SESSION}" "cd ${HOME}/.pocketcli && lazygit" Enter
else
    tmux send-keys -t "${SESSION}" "lazygit" Enter
fi

# Focus left pane
tmux select-pane  -t "${SESSION}:0.0"

# Apply PocketCli tmux config
tmux source-file  "${HOME}/.config/tmux/tmux.conf" 2>/dev/null || true

printf '[PocketCli] Agent environment ready. Attaching...\n\n'
exec tmux attach-session -t "${SESSION}"

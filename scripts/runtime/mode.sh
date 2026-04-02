#!/usr/bin/env sh

pocket_determine_mode() {
    REQUESTED="${POCKETCLI_MODE:-${1:-auto}}"

    if [ "${REQUESTED}" = "viewer" ]; then
        MODE_EFFECTIVE="viewer"
        ENTRY_REASON="forced_viewer"
        return 0
    fi

    if [ "${REQUESTED}" = "agent" ]; then
        if [ "${HAS_TTY}" = "true" ] && [ "${HAS_TMUX}" = "true" ]; then
            MODE_EFFECTIVE="agent"
            ENTRY_REASON="forced_agent"
        else
            MODE_EFFECTIVE="viewer"
            ENTRY_REASON="agent_degraded_missing_tmux_or_tty"
        fi
        return 0
    fi

    if [ "${IS_ISH}" = "true" ]; then
        MODE_EFFECTIVE="viewer"
        ENTRY_REASON="auto_ish"
    elif [ "${HAS_TTY}" = "true" ] && [ "${HAS_TMUX}" = "true" ]; then
        MODE_EFFECTIVE="agent"
        ENTRY_REASON="auto_agent_capable"
    else
        MODE_EFFECTIVE="viewer"
        ENTRY_REASON="auto_viewer_restricted"
    fi
}

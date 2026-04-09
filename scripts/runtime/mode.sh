#!/usr/bin/env sh

pocket_determine_mode() {
    REQUESTED="${POCKETCLI_MODE:-${1:-auto}}"

    if [ "${REQUESTED}" = "viewer" ]; then
        # shellcheck disable=SC2034
        MODE_EFFECTIVE="viewer"
        # shellcheck disable=SC2034
        ENTRY_REASON="forced_viewer"
        return 0
    fi

    if [ "${REQUESTED}" = "agent" ]; then
        if [ "${HAS_TTY}" = "true" ] && [ "${HAS_TMUX}" = "true" ]; then
            # shellcheck disable=SC2034
            MODE_EFFECTIVE="agent"
            # shellcheck disable=SC2034
            ENTRY_REASON="forced_agent"
        else
            # shellcheck disable=SC2034
            MODE_EFFECTIVE="viewer"
            # shellcheck disable=SC2034
            ENTRY_REASON="agent_degraded_missing_tmux_or_tty"
        fi
        return 0
    fi

    if [ "${IS_ISH}" = "true" ]; then
        # shellcheck disable=SC2034
        MODE_EFFECTIVE="viewer"
        # shellcheck disable=SC2034
        ENTRY_REASON="auto_ish"
    elif [ "${HAS_TTY}" = "true" ] && [ "${HAS_TMUX}" = "true" ]; then
        # shellcheck disable=SC2034
        MODE_EFFECTIVE="agent"
        # shellcheck disable=SC2034
        ENTRY_REASON="auto_agent_capable"
    else
        # shellcheck disable=SC2034
        MODE_EFFECTIVE="viewer"
        # shellcheck disable=SC2034
        ENTRY_REASON="auto_viewer_restricted"
    fi
}

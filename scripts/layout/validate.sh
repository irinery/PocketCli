#!/usr/bin/env sh

layout_validate_requirements() {
    SPEC_FILE="$1"
    [ -s "${SPEC_FILE}" ] || return 1
    if ! command -v jq >/dev/null 2>&1; then
        return 0
    fi

    REQ_TTY=$(jq -r '.requires.tty // false' "${SPEC_FILE}" 2>/dev/null)
    REQ_TMUX=$(jq -r '.requires.tmux // false' "${SPEC_FILE}" 2>/dev/null)

    if [ "${REQ_TTY}" = "true" ] && [ "${HAS_TTY}" != "true" ]; then
        return 1
    fi
    if [ "${REQ_TMUX}" = "true" ] && [ "${HAS_TMUX}" != "true" ]; then
        return 1
    fi
    return 0
}

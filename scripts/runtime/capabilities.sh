#!/usr/bin/env sh

pocket_detect_capabilities() {
    if [ -t 0 ]; then
        if [ -t 1 ]; then
            # shellcheck disable=SC2034
            HAS_TTY=true
        else
            # shellcheck disable=SC2034
            HAS_TTY=false
        fi
    else
        # shellcheck disable=SC2034
        HAS_TTY=false
    fi

    if command -v tmux >/dev/null 2>&1; then
        # shellcheck disable=SC2034
        HAS_TMUX=true
    else
        # shellcheck disable=SC2034
        HAS_TMUX=false
    fi

    if command -v tailscale >/dev/null 2>&1; then
        # shellcheck disable=SC2034
        HAS_TAILSCALE=true
    else
        # shellcheck disable=SC2034
        HAS_TAILSCALE=false
    fi

    if command -v jq >/dev/null 2>&1; then
        # shellcheck disable=SC2034
        HAS_JQ=true
    else
        # shellcheck disable=SC2034
        HAS_JQ=false
    fi

    if is_ish; then
        # shellcheck disable=SC2034
        IS_ISH=true
    else
        # shellcheck disable=SC2034
        IS_ISH=false
    fi

    return 0
}

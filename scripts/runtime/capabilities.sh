#!/usr/bin/env sh

pocket_detect_capabilities() {
    # shellcheck disable=SC2034
    HAS_TTY=false
    # shellcheck disable=SC2034
    [ -t 0 ] && [ -t 1 ] && HAS_TTY=true

    # shellcheck disable=SC2034
    HAS_TMUX=false
    # shellcheck disable=SC2034
    command -v tmux >/dev/null 2>&1 && HAS_TMUX=true

    # shellcheck disable=SC2034
    HAS_TAILSCALE=false
    # shellcheck disable=SC2034
    command -v tailscale >/dev/null 2>&1 && HAS_TAILSCALE=true

    # shellcheck disable=SC2034
    HAS_JQ=false
    # shellcheck disable=SC2034
    command -v jq >/dev/null 2>&1 && HAS_JQ=true

    # shellcheck disable=SC2034
    IS_ISH=false
    # shellcheck disable=SC2034
    is_ish && IS_ISH=true
}

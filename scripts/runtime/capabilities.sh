#!/usr/bin/env sh

pocket_detect_capabilities() {
    HAS_TTY=false
    [ -t 0 ] && [ -t 1 ] && HAS_TTY=true

    HAS_TMUX=false
    command -v tmux >/dev/null 2>&1 && HAS_TMUX=true

    HAS_TAILSCALE=false
    command -v tailscale >/dev/null 2>&1 && HAS_TAILSCALE=true

    HAS_JQ=false
    command -v jq >/dev/null 2>&1 && HAS_JQ=true

    IS_ISH=false
    is_ish && IS_ISH=true
}

#!/usr/bin/env sh

pocket_capability_has() {
    command -v "$1" >/dev/null 2>&1
}

pocket_detect_capabilities() {
    HAS_TTY=false
    [ -t 0 ] && HAS_TTY=true

    HAS_TMUX=false
    pocket_capability_has tmux && HAS_TMUX=true

    HAS_TAILSCALE=false
    if command -v has_tailscale_cli >/dev/null 2>&1 && has_tailscale_cli; then
        HAS_TAILSCALE=true
    elif command -v is_tailscale_mesh_operational >/dev/null 2>&1 \
        && is_tailscale_mesh_operational; then
        HAS_TAILSCALE=true
    elif pocket_capability_has tailscale || pocket_capability_has tailscale.exe; then
        HAS_TAILSCALE=true
    fi

    HAS_SSH=false
    pocket_capability_has ssh && HAS_SSH=true

    HAS_SCP=false
    pocket_capability_has scp && HAS_SCP=true

    HAS_JQ=false
    pocket_capability_has jq && HAS_JQ=true

    HAS_FZF=false
    pocket_capability_has fzf && HAS_FZF=true

    HAS_RG=false
    pocket_capability_has rg && HAS_RG=true

    HAS_GIT=false
    pocket_capability_has git && HAS_GIT=true

    HAS_GO=false
    pocket_capability_has go && HAS_GO=true

    # Consumida por scripts que carregam este runtime via source.
    # shellcheck disable=SC2034
    IS_ISH=false
    if command -v is_ish >/dev/null 2>&1 && is_ish; then
        # shellcheck disable=SC2034
        IS_ISH=true
    fi
}

pocket_capabilities_json() {
    COLS="${COLUMNS:-80}"
    ROWS="${LINES:-24}"
    pocket_detect_capabilities

    LAYOUT='plain'
    if [ "${HAS_TTY}" = true ]; then
        if [ "${COLS}" -ge 92 ] 2>/dev/null; then
            LAYOUT='split'
        elif [ "${COLS}" -ge 60 ] 2>/dev/null; then
            LAYOUT='stack'
        else
            LAYOUT='compact'
        fi
    fi

    printf '{"schema_version":1,"terminal":{"cols":%s,"rows":%s,"tui_layout":"%s"},"capabilities":{"has_tty":%s,"has_tmux":%s,"has_tailscale":%s,"has_ssh":%s,"has_scp":%s,"has_jq":%s,"has_fzf":%s,"has_rg":%s,"has_git":%s,"has_go":%s}}\n' \
        "${COLS}" "${ROWS}" "${LAYOUT}" "${HAS_TTY}" "${HAS_TMUX}" "${HAS_TAILSCALE}" "${HAS_SSH}" "${HAS_SCP}" "${HAS_JQ}" "${HAS_FZF}" "${HAS_RG}" "${HAS_GIT}" "${HAS_GO}"
}

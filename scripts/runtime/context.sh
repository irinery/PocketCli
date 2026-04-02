#!/usr/bin/env sh

. "${POCKETCLI_DIR}/scripts/runtime/paths.sh"
. "${POCKETCLI_DIR}/scripts/runtime/capabilities.sh"
. "${POCKETCLI_DIR}/scripts/runtime/mode.sh"

pocket_log_event() {
    pocket_paths_init
    TS=$(date -u '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date '+%Y-%m-%dT%H:%M:%SZ')
    printf '%s %s\n' "${TS}" "$*" >> "$(pocket_runtime_log_file)" 2>/dev/null || true
}

pocket_runtime_context_boot() {
    pocket_paths_init
    pocket_detect_capabilities
    pocket_determine_mode "${1:-auto}"
    pocket_log_event "event=capabilities tty=${HAS_TTY} tmux=${HAS_TMUX} tailscale=${HAS_TAILSCALE} jq=${HAS_JQ} ish=${IS_ISH}"
    pocket_log_event "event=mode_decision requested=${POCKETCLI_MODE:-${1:-auto}} effective=${MODE_EFFECTIVE} reason=${ENTRY_REASON}"
}

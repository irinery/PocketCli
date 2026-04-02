#!/usr/bin/env sh

layout_default_id_for_mode() {
    MODE="${1:-viewer}"
    case "${MODE}" in
        agent) printf 'agent-default\n' ;;
        *) printf 'viewer-default\n' ;;
    esac
}

layout_spec_file() {
    LAYOUT_ID="$1"
    printf '%s/specs/layouts/%s.json\n' "${POCKETCLI_DIR}" "${LAYOUT_ID}"
}

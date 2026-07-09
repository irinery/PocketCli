#!/usr/bin/env sh

. "${POCKETCLI_DIR}/scripts/layout/defaults.sh"
. "${POCKETCLI_DIR}/scripts/layout/validate.sh"
. "${POCKETCLI_DIR}/scripts/layout/tmux_restore.sh"
. "${POCKETCLI_DIR}/scripts/layout/tmux_apply.sh"

layout_engine_apply_or_restore() {
    MODE="${1:-viewer}"
    REQUESTED_LAYOUT="${2:-}"
    ACTION="${3:-open_default_layout}"

    LAYOUT_ID="${REQUESTED_LAYOUT:-$(layout_default_id_for_mode "${MODE}")}"
    SPEC_FILE=$(layout_spec_file "${LAYOUT_ID}")

    if ! layout_validate_requirements "${SPEC_FILE}"; then
        FALLBACK="viewer-default"
        SPEC_FILE=$(layout_spec_file "${FALLBACK}")
        LAYOUT_ID="${FALLBACK}"
    fi

    if [ "${ACTION}" = "restore_workspace" ]; then
        layout_try_restore_tmux || layout_create_restore_tmux || true
    fi

    ENTRY_ACTION="menu"
    if command -v jq >/dev/null 2>&1; then
        ENTRY_ACTION=$(jq -r '.entry_action // "menu"' "${SPEC_FILE}" 2>/dev/null)
    fi

    case "${ENTRY_ACTION}" in
        tmux_workspace)
            layout_apply_tmux_from_spec "${SPEC_FILE}"
            ;;
        *)
            exec sh "${POCKETCLI_DIR}/scripts/pocketcli_menu.sh"
            ;;
    esac
}

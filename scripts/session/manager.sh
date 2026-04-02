#!/usr/bin/env sh
set -eu

POCKETCLI_DIR="${POCKETCLI_DIR:-${HOME}/.pocketcli}"
. "${POCKETCLI_DIR}/lib/common.sh"
. "${POCKETCLI_DIR}/scripts/runtime/context.sh"
. "${POCKETCLI_DIR}/scripts/session/state.sh"
. "${POCKETCLI_DIR}/scripts/session/transitions.sh"
. "${POCKETCLI_DIR}/scripts/inventory/refresh.sh"
. "${POCKETCLI_DIR}/scripts/session/restore.sh"
. "${POCKETCLI_DIR}/scripts/session/explain.sh"
. "${POCKETCLI_DIR}/scripts/layout/engine.sh"

session_manager_main() {
    MODE_REQUESTED="${POCKETCLI_MODE:-${1:-auto}}"
    SESSION_INTENT="${SESSION_INTENT:-boot}"

    pocket_runtime_context_boot "${MODE_REQUESTED}"
    pocket_ensure_session_state
    SESSION_ID=$(pocket_session_get_field "session_id")

    pocket_transition "context_ready"
    pocket_transition "inventory_syncing"
    inventory_refresh || INVENTORY_REFRESH_STATUS="degraded"

    if [ "${HAS_TTY}" = "true" ]; then
        pocket_transition "menu_ready"
    else
        pocket_transition "degraded_mode"
    fi

    ENTRY_ACTION=$(session_resolve_entry_action)
    LAYOUT_ID="${MODE_EFFECTIVE}-default"

    if [ "${ENTRY_ACTION}" = "show_menu" ] || [ "${ENTRY_ACTION}" = "show_degraded_menu" ]; then
        exec sh "${POCKETCLI_DIR}/scripts/pocketcli_menu.sh"
    fi

    pocket_transition "layout_preparing"
    pocket_log_event "event=layout_prepare layout_id=${LAYOUT_ID} action=${ENTRY_ACTION}"
    session_explain
    layout_engine_apply_or_restore "${MODE_EFFECTIVE}" "${LAYOUT_ID}" "${ENTRY_ACTION}"
}

session_manager_main "$@"

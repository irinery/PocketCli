#!/usr/bin/env sh

. "${POCKETCLI_DIR}/scripts/session/state.sh"

pocket_transition() {
    NEXT_STATE="$1"
    pocket_session_set_runtime "${NEXT_STATE}"
    pocket_log_event "event=transition state=${NEXT_STATE}"
}

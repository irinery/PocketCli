#!/usr/bin/env sh

session_explain() {
    explain_step "session-manager"
    explain_kv mode_requested "${MODE_REQUESTED}"
    explain_kv mode_effective "${MODE_EFFECTIVE}"
    explain_kv entry_reason "${ENTRY_REASON}"
    explain_kv entry_action "${ENTRY_ACTION}"
    explain_kv inventory_status "${INVENTORY_REFRESH_STATUS:-unknown}"
    explain_kv inventory_known "${INVENTORY_KNOWN_COUNT:-0}"
    explain_kv inventory_online "${INVENTORY_ONLINE_COUNT:-0}"
}

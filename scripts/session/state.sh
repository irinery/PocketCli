#!/usr/bin/env sh

. "${POCKETCLI_DIR}/scripts/runtime/paths.sh"

pocket_now_utc() {
    date -u '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date '+%Y-%m-%dT%H:%M:%SZ'
}

pocket_write_default_session_state() {
    FILE="$(pocket_session_file)"
    NOW="$(pocket_now_utc)"
    cat > "${FILE}" <<JSON
{
  "schema_version": 1,
  "session_id": "${NOW}",
  "mode_requested": "auto",
  "mode_effective": "viewer",
  "entry_reason": "fresh_start",
  "state": "booting",
  "last_transition_at": "${NOW}",
  "workspace": {
    "layout_id": "viewer-default",
    "layout_applied": false,
    "tmux_session": "pocketcli",
    "focused_pane": "main",
    "restorable": false
  },
  "target": {
    "host_id": "",
    "hostname": "",
    "tailscale_ip": "",
    "source": "",
    "approved": false,
    "last_approval_at": ""
  },
  "inventory": {
    "last_refresh_at": "",
    "refresh_status": "unknown",
    "known_count": 0,
    "online_count": 0
  },
  "capabilities": {
    "has_tty": false,
    "has_tmux": false,
    "has_tailscale": false,
    "has_jq": false,
    "is_ish": false
  },
  "restore": {
    "restore_policy": "workspace_first",
    "resume_allowed": true,
    "last_successful_layout_id": "viewer-default"
  }
}
JSON
}

pocket_ensure_session_state() {
    pocket_paths_init
    [ -s "$(pocket_session_file)" ] || pocket_write_default_session_state
}

pocket_session_get_field() {
    KEY="$1"
    FILE="$(pocket_session_file)"
    if command -v jq >/dev/null 2>&1; then
        jq -r ".${KEY} // empty" "${FILE}" 2>/dev/null || true
        return 0
    fi
    sed -n "s/.*\"${KEY}\": \"\([^\"]*\)\".*/\1/p" "${FILE}" | head -1
}

pocket_session_set_runtime() {
    STATE="$1"
    NOW="$(pocket_now_utc)"
    REQUESTED="${MODE_REQUESTED:-auto}"
    EFFECTIVE="${MODE_EFFECTIVE:-viewer}"
    REASON="${ENTRY_REASON:-unknown}"
    LAYOUT_ID="${LAYOUT_ID:-${EFFECTIVE}-default}"

    cat > "$(pocket_session_file)" <<JSON
{
  "schema_version": 1,
  "session_id": "${SESSION_ID:-${NOW}}",
  "mode_requested": "${REQUESTED}",
  "mode_effective": "${EFFECTIVE}",
  "entry_reason": "${REASON}",
  "state": "${STATE}",
  "last_transition_at": "${NOW}",
  "workspace": {
    "layout_id": "${LAYOUT_ID}",
    "layout_applied": ${LAYOUT_APPLIED:-false},
    "tmux_session": "${POCKETCLI_TMUX_SESSION:-pocketcli}",
    "focused_pane": "main",
    "restorable": ${RESTORABLE:-false}
  },
  "target": {
    "host_id": "",
    "hostname": "",
    "tailscale_ip": "",
    "source": "",
    "approved": false,
    "last_approval_at": ""
  },
  "inventory": {
    "last_refresh_at": "${INVENTORY_LAST_REFRESH_AT:-}",
    "refresh_status": "${INVENTORY_REFRESH_STATUS:-unknown}",
    "known_count": ${INVENTORY_KNOWN_COUNT:-0},
    "online_count": ${INVENTORY_ONLINE_COUNT:-0}
  },
  "capabilities": {
    "has_tty": ${HAS_TTY:-false},
    "has_tmux": ${HAS_TMUX:-false},
    "has_tailscale": ${HAS_TAILSCALE:-false},
    "has_jq": ${HAS_JQ:-false},
    "is_ish": ${IS_ISH:-false}
  },
  "restore": {
    "restore_policy": "workspace_first",
    "resume_allowed": true,
    "last_successful_layout_id": "${LAYOUT_ID}"
  }
}
JSON
}

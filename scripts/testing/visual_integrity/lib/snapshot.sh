#!/usr/bin/env bash

vi_write_snapshot_yaml() {
    local text_file="$1"
    local yaml_file="$2"
    local trigger="$3"
    local cols="$4"
    local rows="$5"
    local exit_code="${6:-null}"

    {
        printf 'snapshot:\n'
        printf '  timestamp: "%s"\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
        printf '  trigger_event: "%s"\n' "${trigger}"
        printf '  terminal_size:\n'
        printf '    cols: %s\n' "${cols}"
        printf '    rows: %s\n' "${rows}"
        printf '  exit_code: %s\n' "${exit_code}"
        printf '  lines:\n'
        sed 's/\\/\\\\/g; s/"/\\"/g; s/^/    - "/; s/$/"/' "${text_file}"
    } > "${yaml_file}"
}

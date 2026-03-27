#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
. "$REPO_ROOT/tests/lib/test_helpers.sh"

WORKDIR=$(mktemp -d)

MENU_DEFS="$WORKDIR/menu_defs.sh"
awk '
    /^_remove_host_line\(\) \{$/ { capture=1 }
    capture { print }
    capture && /^}$/ { exit }
' "$REPO_ROOT/scripts/pocketcli_menu.sh" > "$MENU_DEFS"

run_remove_case() {
    CASE_NAME="$1"
    INITIAL_HOSTS="$2"
    LINE_NO="$3"
    EXPECTED_RC="$4"
    EXPECTED_HOSTS="$5"

    HOSTS_FILE="$WORKDIR/hosts.txt"
    if [ -n "${INITIAL_HOSTS}" ]; then
        printf '%s' "${INITIAL_HOSTS}" > "$HOSTS_FILE"
    else
        rm -f "$HOSTS_FILE"
    fi

    set +e
    sh -c ". \"$MENU_DEFS\"; _remove_host_line \"$HOSTS_FILE\" \"$LINE_NO\""
    RC="$?"
    set -e

    [ "${RC}" = "${EXPECTED_RC}" ]

    if [ -n "${EXPECTED_HOSTS}" ]; then
        ACTUAL_HOSTS=$(cat "$HOSTS_FILE" 2>/dev/null || true)
        [ "${ACTUAL_HOSTS}" = "${EXPECTED_HOSTS}" ]
    else
        [ ! -f "$HOSTS_FILE" ]
    fi

    printf 'PASS %s\n' "${CASE_NAME}"
}

run_remove_case \
    "menu remove host removes the selected line portably" \
    "ipad-a
ipad-b
ipad-c
" \
    "2" \
    "0" \
    "ipad-a
ipad-c"

run_remove_case \
    "menu remove host reports a missing line without changing hosts" \
    "ipad-a
ipad-b
ipad-c
" \
    "9" \
    "3" \
    "ipad-a
ipad-b
ipad-c"

run_remove_case \
    "menu remove host handles missing hosts file" \
    "" \
    "1" \
    "2" \
    ""

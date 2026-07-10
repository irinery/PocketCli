#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
MENU_DEFS="$WORKDIR/menu_defs.sh"
LOG_FILE="$WORKDIR/connect.log"
HOME_DIR="$WORKDIR/home"

mkdir -p "$HOME_DIR/.pocketcli/scripts/ssh"

awk '
    /^_run_action\(\) \{$/ { capture=1 }
    capture { print }
    capture && /^}$/ { exit }
' "$REPO_ROOT/scripts/pocketcli_menu.sh" > "$MENU_DEFS"

sh -c '
    set -eu
    . "$1"
    POCKETCLI_DIR=$2
    LOG_FILE=$3
    LAST_MESSAGE=""
    _leave_tui_for_action() { :; }
    _render_header_screen() { :; }
    _pick_host() { printf "%s" "devbox"; }
    _enter_tui() { :; }
    _run_with_pause() {
        shift 3
        printf "%s\n" "$*" > "$LOG_FILE"
    }
    _run_action connect
' sh "$MENU_DEFS" "$HOME_DIR/.pocketcli" "$LOG_FILE"

grep -F 'scripts/ssh/open.sh' "$LOG_FILE" >/dev/null 2>&1
grep -F -- '--run interactive' "$LOG_FILE" >/dev/null 2>&1
grep -F 'devbox' "$LOG_FILE" >/dev/null 2>&1

printf 'PASS menu connect delegates to SSH policy wrapper\n'

#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
MENU_DEFS="$WORKDIR/menu_defs.sh"

extract_function() {
    NAME="$1"
    awk -v name="$NAME" '
        $0 ~ "^" name "\\(\\) \\{$" { capture=1 }
        capture { print }
        capture && /^}$/ { exit }
    ' "$REPO_ROOT/scripts/pocketcli_menu.sh" >> "$MENU_DEFS"
    printf '\n' >> "$MENU_DEFS"
}

: > "$MENU_DEFS"
extract_function "_tui_input_history_file"
extract_function "_tui_input_sanitize"
extract_function "_tui_string_chop_last"
extract_function "_tui_suggestion_for"
extract_function "_tui_record_input"

CANDIDATES="$WORKDIR/candidates.txt"
POCKET_HOME="$WORKDIR/pocketcli"
mkdir -p "$POCKET_HOME/state"
printf '%s\n' 'pocket-dev' 'prod-api' 'mac-mini' > "$CANDIDATES"
printf '%s\n' 'prod-api' 'staging-box' > "$POCKET_HOME/state/tui-input-history"

run_helper() {
    FUNCTION_NAME="$1"
    shift
    sh -c '
        . "$1"
        POCKETCLI_DIR="$2"
        TUI_INPUT_HISTORY_LIMIT=50
        shift 2
        "$@"
    ' sh "$MENU_DEFS" "$POCKET_HOME" "$FUNCTION_NAME" "$@"
}

SUGGESTION=$(run_helper _tui_suggestion_for pr "$CANDIDATES")
[ "$SUGGESTION" = "prod-api" ]

SUGGESTION=$(run_helper _tui_suggestion_for poc "$CANDIDATES")
[ "$SUGGESTION" = "pocket-dev" ]

SUGGESTION=$(run_helper _tui_suggestion_for "" "$CANDIDATES")
[ "$SUGGESTION" = "prod-api" ]

SANITIZED=$(run_helper _tui_input_sanitize 'app; rm -rf /')
[ "$SANITIZED" = "apprm-rf" ]

CHOPPED=$(run_helper _tui_string_chop_last pocket)
[ "$CHOPPED" = "pocke" ]

run_helper _tui_record_input mac-mini >/dev/null
FIRST_HISTORY=$(sed -n '1p' "$POCKET_HOME/state/tui-input-history")
[ "$FIRST_HISTORY" = "mac-mini" ]

MODE=$(stat -c '%a' "$POCKET_HOME/state/tui-input-history" 2>/dev/null || stat -f '%Lp' "$POCKET_HOME/state/tui-input-history")
[ "$MODE" = "600" ]

printf 'PASS tui autocomplete suggests from current input and previous input history\n'

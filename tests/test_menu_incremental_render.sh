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
extract_function "_cursor_move"
extract_function "_clear_line"
extract_function "_line_count_file"
extract_function "_emit_block_diff"

OLD_FILE="$WORKDIR/old.txt"
NEW_FILE="$WORKDIR/new.txt"
DELETE_OLD_FILE="$WORKDIR/delete-old.txt"
DELETE_NEW_FILE="$WORKDIR/delete-new.txt"
ESC=$(printf '\033')

printf 'line-a\nline-b\nline-c\n' > "$OLD_FILE"
printf 'line-a\nline-x\nline-c\nline-d\n' > "$NEW_FILE"

OUTPUT=$(sh -c '. "$1"; _emit_block_diff "$2" "$3" 10' sh "$MENU_DEFS" "$OLD_FILE" "$NEW_FILE")

printf '%s' "$OUTPUT" | grep -F "${ESC}[11;1H${ESC}[2Kline-x" >/dev/null 2>&1
printf '%s' "$OUTPUT" | grep -F "${ESC}[13;1H${ESC}[2Kline-d" >/dev/null 2>&1

if printf '%s' "$OUTPUT" | grep -F 'line-a' >/dev/null 2>&1; then
    printf 'FAIL diff reemitiu linha sem mudança (line-a)\n' >&2
    exit 1
fi

if printf '%s' "$OUTPUT" | grep -F 'line-c' >/dev/null 2>&1; then
    printf 'FAIL diff reemitiu linha sem mudança (line-c)\n' >&2
    exit 1
fi

printf 'line-a\nline-b\nline-z\n' > "$DELETE_OLD_FILE"
printf 'line-a\nline-b\n' > "$DELETE_NEW_FILE"

DELETE_OUTPUT=$(sh -c '. "$1"; _emit_block_diff "$2" "$3" 20' sh "$MENU_DEFS" "$DELETE_OLD_FILE" "$DELETE_NEW_FILE")
printf '%s' "$DELETE_OUTPUT" | grep -F "${ESC}[22;1H${ESC}[2K" >/dev/null 2>&1

printf 'PASS pocketcli_menu emits ANSI diffs only for changed menu lines\n'

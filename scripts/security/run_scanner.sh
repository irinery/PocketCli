#!/usr/bin/env bash
set -u

if [ "$#" -lt 2 ]; then
    printf 'Usage: %s <scanner-id> <command> [args...]\n' "$0" >&2
    exit 2
fi

SCANNER_ID=$1
shift
RESULTS_DIR=${SECURITY_RESULTS_DIR:-.security-results}

mkdir -p "$RESULTS_DIR"
OUT_FILE=$RESULTS_DIR/${SCANNER_ID}.out
ERR_FILE=$RESULTS_DIR/${SCANNER_ID}.err
RC_FILE=$RESULTS_DIR/${SCANNER_ID}.rc

"$@" >"$OUT_FILE" 2>"$ERR_FILE"
RC=$?

cat "$OUT_FILE"
cat "$ERR_FILE" >&2
printf '%s\n' "$RC" > "$RC_FILE"

exit "$RC"

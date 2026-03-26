#!/usr/bin/env sh

set -eu

test_repo_root() {
    CDPATH='' cd -- "$(dirname "$0")/../.." && pwd
}

run_with_timeout() {
    SECS="$1"
    shift

    if command -v timeout >/dev/null 2>&1; then
        timeout "${SECS}" "$@"
        return $?
    fi
    if command -v python3 >/dev/null 2>&1; then
        python3 - "$SECS" "$@" <<'PY'
import subprocess
import sys

timeout = int(sys.argv[1])
command = sys.argv[2:]
proc = subprocess.Popen(command)
try:
    raise SystemExit(proc.wait(timeout=timeout))
except subprocess.TimeoutExpired:
    proc.kill()
    proc.wait()
    raise SystemExit(124)
PY
        return $?
    fi

    "$@" &
    PID=$!
    (
        sleep "${SECS}"
        kill "${PID}" 2>/dev/null || true
    ) &
    GUARD=$!

    wait "${PID}" 2>/dev/null
    RC=$?
    kill "${GUARD}" 2>/dev/null || true
    return "${RC}"
}

run_pty_command() {
    SECS="$1"
    OUTPUT_FILE="$2"
    COMMAND_STRING="$3"

    rm -f "${OUTPUT_FILE}"
    case "$(uname -s 2>/dev/null || printf unknown)" in
        Darwin)
            run_with_timeout "${SECS}" script -q -F "${OUTPUT_FILE}" sh -lc "${COMMAND_STRING}"
            ;;
        *)
            run_with_timeout "${SECS}" script -q -c "${COMMAND_STRING}" "${OUTPUT_FILE}"
            ;;
    esac
}

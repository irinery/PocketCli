#!/usr/bin/env sh

set -eu

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
REPO_ROOT=$(CDPATH='' cd -- "${SCRIPT_DIR}/../.." && pwd)
RESULTS_DIR=${SECURITY_RESULTS_DIR:-${REPO_ROOT}/.security-results}

cd "${REPO_ROOT}"
mkdir -p "${RESULTS_DIR}"
for SCANNER_ID in 01 02 03 04; do
    rm -f "${RESULTS_DIR}/${SCANNER_ID}.out" \
        "${RESULTS_DIR}/${SCANNER_ID}.err" \
        "${RESULTS_DIR}/${SCANNER_ID}.rc"
done

SECURITY_RESULTS_DIR="${RESULTS_DIR}" \
    bash scripts/security/run_scanner.sh 01 \
    bash scripts/security/01_secret_leak_scanner.sh &
PID_01=$!
SECURITY_RESULTS_DIR="${RESULTS_DIR}" \
    bash scripts/security/run_scanner.sh 02 \
    bash scripts/security/02_shell_injection_audit.sh &
PID_02=$!
SECURITY_RESULTS_DIR="${RESULTS_DIR}" \
    bash scripts/security/run_scanner.sh 03 \
    bash scripts/security/03_filesystem_permission_audit.sh repo &
PID_03=$!
SECURITY_RESULTS_DIR="${RESULTS_DIR}" \
    bash scripts/security/run_scanner.sh 04 \
    bash scripts/security/04_ssh_tailscale_hardening.sh &
PID_04=$!

STATUS=0
wait_for_scanner() {
    SCANNER_PID=$1
    SCANNER_ID=$2
    SCANNER_RC=0

    wait "${SCANNER_PID}" || SCANNER_RC=$?
    if [ ! -f "${RESULTS_DIR}/${SCANNER_ID}.rc" ]; then
        printf '2\n' > "${RESULTS_DIR}/${SCANNER_ID}.rc"
        SCANNER_RC=2
    fi
    if [ "${SCANNER_RC}" -ne 0 ]; then
        STATUS=1
    fi
}

wait_for_scanner "${PID_01}" 01
wait_for_scanner "${PID_02}" 02
wait_for_scanner "${PID_03}" 03
wait_for_scanner "${PID_04}" 04

exit "${STATUS}"

#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
. "${REPO_ROOT}/tests/lib/ci.sh"

TOTAL_START=$(ci_now_seconds)
PASS_COUNT=0
FAIL_COUNT=0
PROCESSED_COUNT=0

printf 'Running pocket smoke suite for profile=%s\n' "$(ci_profile)"
ci_summary "### Smoke tests ($(ci_profile))"

TEST_LIST=$(mktemp)
find "${REPO_ROOT}/tests/smoke" -type f -name 'test_*.sh' | sort > "${TEST_LIST}"
DISCOVERED_COUNT=$(wc -l < "${TEST_LIST}")

while IFS= read -r TEST_SCRIPT; do
    PROCESSED_COUNT=$((PROCESSED_COUNT + 1))
    TEST_NAME=$(basename "${TEST_SCRIPT}")
    BUDGET=$(ci_smoke_budget "${TEST_NAME}")
    LOG_FILE=$(mktemp)

    if ci_run_script "${TEST_SCRIPT}" "${BUDGET}" "${LOG_FILE}"; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
    rm -f "${LOG_FILE}"
done < "${TEST_LIST}"
rm -f "${TEST_LIST}"

if [ "${PROCESSED_COUNT}" -ne "${DISCOVERED_COUNT}" ]; then
    printf 'FAIL smoke suite processed %s of %s discovered tests\n' "${PROCESSED_COUNT}" "${DISCOVERED_COUNT}" >&2
    exit 1
fi

TOTAL_END=$(ci_now_seconds)
TOTAL_DURATION=$((TOTAL_END - TOTAL_START))

printf '\nSmoke summary: %s passed, %s failed, %ss total\n' "${PASS_COUNT}" "${FAIL_COUNT}" "${TOTAL_DURATION}"
ci_summary "- total: ${TOTAL_DURATION}s"

[ "${FAIL_COUNT}" -eq 0 ]

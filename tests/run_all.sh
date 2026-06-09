#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
. "${REPO_ROOT}/tests/lib/ci.sh"

TOTAL_BUDGET=$(ci_shell_suite_budget)
TOTAL_START=$(ci_now_seconds)
PASS_COUNT=0
FAIL_COUNT=0

printf 'Running shell regression suite for profile=%s\n' "$(ci_profile)"
ci_summary "### Shell regression ($(ci_profile))"

TEST_LIST=$(mktemp)
find "${REPO_ROOT}/tests" -maxdepth 1 -type f -name 'test_*.sh' | sort > "${TEST_LIST}"

while IFS= read -r TEST_SCRIPT; do
    TEST_NAME=$(basename "${TEST_SCRIPT}")
    if ci_is_excluded "${TEST_NAME}"; then
        printf 'SKIP %s\n' "${TEST_NAME}"
        ci_summary "- ${TEST_NAME}: skipped"
        continue
    fi

    LOG_FILE=$(mktemp)
    if ci_run_script "${TEST_SCRIPT}" -1 "${LOG_FILE}"; then
        PASS_COUNT=$((PASS_COUNT + 1))
    else
        FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
    rm -f "${LOG_FILE}"
done < "${TEST_LIST}"
rm -f "${TEST_LIST}"

TOTAL_END=$(ci_now_seconds)
TOTAL_DURATION=$((TOTAL_END - TOTAL_START))

printf '\nShell suite summary: %s passed, %s failed, %ss total\n' "${PASS_COUNT}" "${FAIL_COUNT}" "${TOTAL_DURATION}"
ci_summary "- total: ${TOTAL_DURATION}s (budget ${TOTAL_BUDGET}s)"

if [ "${TOTAL_DURATION}" -gt "${TOTAL_BUDGET}" ]; then
    printf 'FAIL shell suite exceeded budget (%ss > %ss)\n' "${TOTAL_DURATION}" "${TOTAL_BUDGET}" >&2
    exit 1
fi

[ "${FAIL_COUNT}" -eq 0 ]

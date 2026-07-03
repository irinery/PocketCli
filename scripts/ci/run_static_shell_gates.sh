#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
PROFILE=${POCKETCLI_CI_PROFILE:-linux}
SUMMARY_FILE=${POCKETCLI_CI_SUMMARY_FILE:-}
TEST_EXCLUDES=${POCKETCLI_TEST_EXCLUDES:-test_ish.sh test_menu_fallback.sh test_menu_incremental_render.sh test_menu_no_hosts.sh test_menu_remove_host.sh}

VET_LOG="/tmp/pocket-vet-${PROFILE}.log"
MENU_LOG="/tmp/pocket-menu-${PROFILE}.log"
TEST_LOG="/tmp/pocket-test-${PROFILE}.log"
VET_DURATION_FILE="/tmp/pocket-vet-${PROFILE}.duration"
MENU_DURATION_FILE="/tmp/pocket-menu-${PROFILE}.duration"
SHELL_SUMMARY_FILE="/tmp/pocket-shell-${PROFILE}-summary.md"

now_seconds() {
    date +%s
}

run_vet() {
    START_TS=$(now_seconds)
    STATUS=0
    go vet ./... || STATUS=$?
    END_TS=$(now_seconds)
    printf '%s\n' "$((END_TS - START_TS))" > "${VET_DURATION_FILE}"
    return "${STATUS}"
}

run_menu_regressions() {
    START_TS=$(now_seconds)
    STATUS=0

    for TEST_SCRIPT in \
        tests/test_menu_fallback.sh \
        tests/test_menu_incremental_render.sh \
        tests/test_menu_no_hosts.sh \
        tests/test_menu_remove_host.sh
    do
        TEST_NAME=$(basename "${TEST_SCRIPT}")
        printf '==> %s\n' "${TEST_NAME}"
        if sh "${TEST_SCRIPT}"; then
            :
        else
            STATUS=$?
            break
        fi
    done

    END_TS=$(now_seconds)
    printf '%s\n' "$((END_TS - START_TS))" > "${MENU_DURATION_FILE}"
    return "${STATUS}"
}

run_shell_regressions() {
    POCKETCLI_CI_PROFILE="${PROFILE}" \
        POCKETCLI_CI_SUMMARY_FILE="${SHELL_SUMMARY_FILE}" \
        POCKETCLI_TEST_EXCLUDES="${TEST_EXCLUDES}" \
        sh tests/run_all.sh
}

duration_or_unknown() {
    if [ -f "$1" ]; then
        cat "$1"
    else
        printf 'unknown'
    fi
}

append_summary() {
    [ -n "${SUMMARY_FILE}" ] || return 0

    {
        printf '### Go vet (%s)\n' "${PROFILE}"
        printf -- '- total: %ss\n' "$(duration_or_unknown "${VET_DURATION_FILE}")"
        printf '### Menu regressions (%s)\n' "${PROFILE}"
        printf -- '- total: %ss\n' "$(duration_or_unknown "${MENU_DURATION_FILE}")"
        if [ -s "${SHELL_SUMMARY_FILE}" ]; then
            cat "${SHELL_SUMMARY_FILE}"
        fi
    } >> "${SUMMARY_FILE}"
}

cd "${REPO_ROOT}"
rm -f "${VET_LOG}" "${MENU_LOG}" "${TEST_LOG}" \
    "${VET_DURATION_FILE}" "${MENU_DURATION_FILE}" "${SHELL_SUMMARY_FILE}"
: > "${SHELL_SUMMARY_FILE}"

run_vet > "${VET_LOG}" 2>&1 &
VET_PID=$!
run_menu_regressions > "${MENU_LOG}" 2>&1 &
MENU_PID=$!
run_shell_regressions > "${TEST_LOG}" 2>&1 &
SHELL_PID=$!

VET_EXIT=0
MENU_EXIT=0
SHELL_EXIT=0
wait "${VET_PID}" || VET_EXIT=$?
wait "${MENU_PID}" || MENU_EXIT=$?
wait "${SHELL_PID}" || SHELL_EXIT=$?

printf '%s\n' '=== go vet ==='
cat "${VET_LOG}"
printf '%s\n' '=== menu regressions ==='
cat "${MENU_LOG}"
printf '%s\n' '=== shell regressions ==='
cat "${TEST_LOG}"
append_summary

if [ "${VET_EXIT}" -ne 0 ] || [ "${MENU_EXIT}" -ne 0 ] || [ "${SHELL_EXIT}" -ne 0 ]; then
    printf 'Quality gates failed: vet=%s menu=%s shell=%s\n' \
        "${VET_EXIT}" "${MENU_EXIT}" "${SHELL_EXIT}" >&2
    exit 1
fi

printf 'Quality gates passed: vet, menu and shell (%s)\n' "${PROFILE}"

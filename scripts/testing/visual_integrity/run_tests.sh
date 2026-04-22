#!/usr/bin/env bash
set -u

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
REPO_ROOT=$(CDPATH='' cd -- "${SCRIPT_DIR}/../../.." && pwd)
export REPO_ROOT
FIXTURE_DIR="${SCRIPT_DIR}/fixtures"
REPORT_DIR="${SCRIPT_DIR}/reports"
ACTUAL_DIR="${REPORT_DIR}/actual"

# shellcheck source=lib/terminal.sh
. "${SCRIPT_DIR}/lib/terminal.sh"
# shellcheck source=lib/input.sh
. "${SCRIPT_DIR}/lib/input.sh"
# shellcheck source=lib/snapshot.sh
. "${SCRIPT_DIR}/lib/snapshot.sh"
# shellcheck source=lib/assert.sh
. "${SCRIPT_DIR}/lib/assert.sh"

FAILURES=0
SETUP_FAILED=0

mkdir -p "${FIXTURE_DIR}" "${REPORT_DIR}" "${ACTUAL_DIR}"
rm -f "${REPORT_DIR}"/failure_*.yml 2>/dev/null || true
vi_summary_init

finish() {
    vi_cleanup_workspace
    vi_summary_finish "${FAILURES}"
}
trap finish EXIT

run_case() {
    local id="$1"
    local desc="$2"
    local func="$3"
    local start end duration rc status

    start=$(vi_now_ms)
    "${func}"
    rc=$?
    end=$(vi_now_ms)
    duration=$((end - start))

    if [[ "${rc}" -eq 0 ]]; then
        status=PASS
        printf '[PASS] %s %s (%sms)\n' "${id}" "${desc}" "${duration}"
    else
        status=FAIL
        FAILURES=$((FAILURES + 1))
        printf '[FAIL] %s %s (%sms)\n' "${id}" "${desc}" "${duration}" >&2
    fi

    vi_summary_result "${id}" "${desc}" "${status}" "${duration}"
}

record_or_assert_snapshot() {
    local id="$1"
    local desc="$2"
    local event="$3"
    local actual="$4"
    local fixture="$5"
    vi_record_or_assert "${id}" "${desc}" "${event}" "${actual}" "${fixture}"
}

start_menu() {
    local cols="$1"
    local rows="$2"
    vi_start_session "${cols}" "${rows}"
    sleep 0.5
}

stop_menu() {
    vi_stop_session
}

menu_start_line() {
    awk '/Ações rápidas|Radar da malha/ { print NR; exit }' "$1"
}

assert_prefix_unchanged() {
    local before="$1"
    local after="$2"
    local line
    line=$(menu_start_line "${before}")
    [[ -n "${line}" ]] || return 1
    head -n "$((line - 1))" "${before}" > "${ACTUAL_DIR}/prefix-before.txt"
    head -n "$((line - 1))" "${after}" > "${ACTUAL_DIR}/prefix-after.txt"
    diff -u "${ACTUAL_DIR}/prefix-before.txt" "${ACTUAL_DIR}/prefix-after.txt" >/dev/null 2>&1
}

assert_single_highlight() {
    local file="$1"
    local count
    count=$(grep -o '›' "${file}" 2>/dev/null | wc -l | tr -d ' ')
    [[ "${count}" == "1" ]]
}

test_t12() {
    local actual="${ACTUAL_DIR}/T-12_initial.txt"
    start_menu 80 24
    vi_snapshot 24 "${actual}"
    vi_write_snapshot_yaml "${actual}" "${ACTUAL_DIR}/T-12_initial.yml" "start" 80 24 null
    stop_menu

    vi_assert_no_escape_chars "${actual}" || return 1
    record_or_assert_snapshot T-12 "Snapshot inicial limpo" "start" "${actual}" "${FIXTURE_DIR}/T-12_initial.txt"
}

test_t13() {
    local initial="${ACTUAL_DIR}/T-13_initial.txt"
    local step1="${ACTUAL_DIR}/T-13_nav_step1.txt"
    local step2="${ACTUAL_DIR}/T-13_nav_step2.txt"
    local step3="${ACTUAL_DIR}/T-13_nav_step3.txt"

    start_menu 80 24
    vi_snapshot 24 "${initial}"

    vi_input_j
    sleep 0.25
    vi_snapshot 24 "${step1}"
    vi_input_j
    sleep 0.25
    vi_snapshot 24 "${step2}"
    vi_input_j
    sleep 0.25
    vi_snapshot 24 "${step3}"
    stop_menu

    assert_single_highlight "${step1}" || return 1
    assert_single_highlight "${step2}" || return 1
    assert_single_highlight "${step3}" || return 1
    assert_prefix_unchanged "${initial}" "${step1}" || return 1
    assert_prefix_unchanged "${initial}" "${step2}" || return 1
    assert_prefix_unchanged "${initial}" "${step3}" || return 1

    record_or_assert_snapshot T-13 "Navegação não deixa resíduo" "j#1" "${step1}" "${FIXTURE_DIR}/T-13_nav_step1.txt" || return $?
    record_or_assert_snapshot T-13 "Navegação não deixa resíduo" "j#2" "${step2}" "${FIXTURE_DIR}/T-13_nav_step2.txt" || return $?
    record_or_assert_snapshot T-13 "Navegação não deixa resíduo" "j#3" "${step3}" "${FIXTURE_DIR}/T-13_nav_step3.txt"
}

test_t14() {
    local actual="${ACTUAL_DIR}/T-14_submenu.txt"
    start_menu 80 24
    vi_input_j
    sleep 0.25
    vi_input_enter
    sleep 0.35
    vi_snapshot 24 "${actual}"
    stop_menu

    vi_assert_present_text "${actual}" "Radar da malha" || return 1
    vi_assert_absent_text "${actual}" "Conectar agora" || return 1
    record_or_assert_snapshot T-14 "Transição para sub-menu limpa" "Enter" "${actual}" "${FIXTURE_DIR}/T-14_submenu.txt"
}

test_t15() {
    local actual="${ACTUAL_DIR}/T-15_return.txt"
    start_menu 80 24
    vi_input_j
    sleep 0.25
    vi_input_enter
    sleep 0.35
    vi_input_q
    sleep 0.35
    vi_snapshot 24 "${actual}"
    stop_menu

    vi_assert_present_text "${actual}" "Ações rápidas" || return 1
    vi_assert_absent_text "${actual}" "Executar radar" || return 1
    record_or_assert_snapshot T-15 "Retorno ao menu principal limpo" "q" "${actual}" "${FIXTURE_DIR}/T-15_return.txt"
}

test_t16() {
    local actual="${ACTUAL_DIR}/T-13_nav_step3.txt"
    local fixture="${FIXTURE_DIR}/T-16_regression.txt"

    [[ -f "${actual}" ]] || return 1

    if [[ "${POCKETCLI_VISUAL_RECORD:-0}" == "1" ]]; then
        cp "${actual}" "${fixture}"
        printf '\nRESIDUO-REGRESSION\n' >> "${fixture}"
        return 0
    fi

    vi_assert_not_equal_fixture "${actual}" "${fixture}"
}

test_t17() {
    local actual="${ACTUAL_DIR}/T-17_resize.txt"
    start_menu 80 24
    vi_resize 100 30
    sleep 0.5
    vi_snapshot 30 "${actual}"
    stop_menu

    vi_assert_present_text "${actual}" "PocketCli Control Deck" || return 1
    record_or_assert_snapshot T-17 "Terminal redimensionado" "SIGWINCH 100x30" "${actual}" "${FIXTURE_DIR}/T-17_resize.txt"
}

test_t18() {
    local actual="${ACTUAL_DIR}/T-18_exit.txt"
    local exit_code
    start_menu 80 24
    vi_input_q
    sleep 0.4
    vi_snapshot 24 "${actual}"
    exit_code=$(vi_exit_code)
    stop_menu

    [[ "${exit_code}" == "0" ]] || return 1
    vi_assert_absent_text "${actual}" "PocketCli Control Deck" || return 1
    vi_assert_present_text "${actual}" 'VISUAL_TEST_PROMPT$' || return 1
    record_or_assert_snapshot T-18 "Saída limpa verificada" "q" "${actual}" "${FIXTURE_DIR}/T-18_exit.txt"
}

if ! vi_require_terminal; then
    SETUP_FAILED=1
    FAILURES=1
    vi_summary_result SETUP "Dependências da suíte visual" FAIL 0
    exit 1
fi

if ! vi_setup_workspace; then
    SETUP_FAILED=1
    FAILURES=1
    vi_summary_result SETUP "Workspace da suíte visual" FAIL 0
    exit 1
fi

run_case T-12 "Snapshot inicial limpo" test_t12
run_case T-13 "Navegação não deixa resíduo" test_t13
run_case T-14 "Transição para sub-menu limpa" test_t14
run_case T-15 "Retorno ao menu principal limpo" test_t15
run_case T-16 "Regressão de resíduo é detectada" test_t16
run_case T-17 "Terminal redimensionado" test_t17
run_case T-18 "Saída limpa verificada" test_t18

if [[ "${SETUP_FAILED}" -eq 1 ]]; then
    exit 1
fi

if [[ "${FAILURES}" -eq 0 ]]; then
    exit 0
fi
exit 1

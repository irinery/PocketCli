#!/usr/bin/env bash

vi_now_ms() {
    if command -v python3 >/dev/null 2>&1; then
        python3 - <<'PY'
import time
print(int(time.time() * 1000))
PY
    else
        printf '%s000\n' "$(date +%s)"
    fi
}

vi_summary_init() {
    mkdir -p "${REPORT_DIR}"
    SUMMARY_FILE="${REPORT_DIR}/summary.md"
    {
        printf '## Layout Gate — Visual Integrity Results\n\n'
        printf '| ID | Descrição | Status | Duração |\n'
        printf '|---|---|---|---|\n'
    } > "${SUMMARY_FILE}"
}

vi_summary_result() {
    local id="$1"
    local desc="$2"
    local status="$3"
    local duration="$4"
    local label

    case "${status}" in
        PASS) label='✅ PASS' ;;
        FAIL) label='❌ FAIL' ;;
        SKIP) label='⏭ SKIP' ;;
        *) label="${status}" ;;
    esac

    printf '| %s | %s | %s | %sms |\n' "${id}" "${desc}" "${label}" "${duration}" >> "${SUMMARY_FILE}"
}

vi_summary_finish() {
    local failures="$1"
    {
        printf '\n'
        if [[ "${failures}" -eq 0 ]]; then
            printf '**Resultado:** todos os testes visuais passaram.\n'
        else
            printf '**Resultado:** %s falha(s) detectada(s) — merge bloqueado.\n' "${failures}"
        fi
    } >> "${SUMMARY_FILE}"
}

vi_report_failure() {
    local id="$1"
    local desc="$2"
    local event="$3"
    local expected="$4"
    local actual="$5"
    local report
    report="${REPORT_DIR}/failure_${id}_$(date +%Y%m%d%H%M%S).yml"

    {
        printf 'test_id: "%s"\n' "${id}"
        printf 'description: "%s"\n' "${desc}"
        printf 'trigger_event: "%s"\n' "${event}"
        printf 'exit_code_expected: null\n'
        printf 'exit_code_actual: null\n'
        printf 'diff: |\n'
        diff -u "${expected}" "${actual}" 2>/dev/null | sed 's/^/  /' || true
    } > "${report}"
}

vi_record_or_assert() {
    local id="$1"
    local desc="$2"
    local event="$3"
    local actual="$4"
    local fixture="$5"

    if [[ "${POCKETCLI_VISUAL_RECORD:-0}" == "1" ]]; then
        cp "${actual}" "${fixture}"
        return 0
    fi

    if [[ ! -f "${fixture}" ]]; then
        printf 'Fixture ausente para %s: %s\n' "${id}" "${fixture}" >&2
        return 2
    fi

    if diff -u "${fixture}" "${actual}" >/dev/null 2>&1; then
        return 0
    fi

    vi_report_failure "${id}" "${desc}" "${event}" "${fixture}" "${actual}"
    return 1
}

vi_assert_not_equal_fixture() {
    local actual="$1"
    local fixture="$2"

    if [[ ! -f "${fixture}" ]]; then
        printf 'Fixture ausente para regressão: %s\n' "${fixture}" >&2
        return 2
    fi

    if diff -u "${fixture}" "${actual}" >/dev/null 2>&1; then
        return 1
    fi

    return 0
}

vi_assert_no_escape_chars() {
    local file="$1"
    local esc
    esc=$(printf '\033')
    if LC_ALL=C grep "${esc}" "${file}" >/dev/null 2>&1; then
        return 1
    fi
    return 0
}

vi_assert_absent_text() {
    local file="$1"
    local text="$2"
    if grep -F "${text}" "${file}" >/dev/null 2>&1; then
        return 1
    fi
    return 0
}

vi_assert_present_text() {
    local file="$1"
    local text="$2"
    if grep -F "${text}" "${file}" >/dev/null 2>&1; then
        return 0
    fi
    return 1
}

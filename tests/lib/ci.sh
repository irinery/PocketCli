#!/usr/bin/env sh

set -eu

ci_now_seconds() {
    date +%s
}

ci_summary() {
    if [ -n "${POCKETCLI_CI_SUMMARY_FILE:-}" ]; then
        printf '%s\n' "$*" >> "${POCKETCLI_CI_SUMMARY_FILE}"
    fi
}

ci_profile() {
    printf '%s' "${POCKETCLI_CI_PROFILE:-linux}"
}

ci_shell_suite_budget() {
    if [ -n "${POCKETCLI_SHELL_SUITE_BUDGET:-}" ]; then
        printf '%s' "${POCKETCLI_SHELL_SUITE_BUDGET}"
        return 0
    fi

    case "$(ci_profile)" in
        alpine) printf '45' ;;
        macos) printf '50' ;;
        *) printf '45' ;;
    esac
}

ci_smoke_budget() {
    TEST_NAME="$1"

    case "${TEST_NAME}" in
        test_pocket_help.sh)
            case "$(ci_profile)" in
                alpine) printf '%s' "${POCKETCLI_SMOKE_HELP_BUDGET:-2}" ;;
                *) printf '%s' "${POCKETCLI_SMOKE_HELP_BUDGET:-1}" ;;
            esac
            ;;
        test_pocket_resume.sh)
            case "$(ci_profile)" in
                alpine) printf '%s' "${POCKETCLI_SMOKE_RESUME_BUDGET:-3}" ;;
                *) printf '%s' "${POCKETCLI_SMOKE_RESUME_BUDGET:-2}" ;;
            esac
            ;;
        test_pocket_default_entrypoint.sh)
            case "$(ci_profile)" in
                alpine) printf '%s' "${POCKETCLI_SMOKE_DEFAULT_BUDGET:-3}" ;;
                *) printf '%s' "${POCKETCLI_SMOKE_DEFAULT_BUDGET:-2}" ;;
            esac
            ;;
        test_pocket_update_profile.sh)
            case "$(ci_profile)" in
                alpine) printf '%s' "${POCKETCLI_SMOKE_UPDATE_BUDGET:-6}" ;;
                *) printf '%s' "${POCKETCLI_SMOKE_UPDATE_BUDGET:-4}" ;;
            esac
            ;;
        test_pocket_tailscale_fallback.sh)
            case "$(ci_profile)" in
                alpine) printf '%s' "${POCKETCLI_SMOKE_FALLBACK_BUDGET:-4}" ;;
                *) printf '%s' "${POCKETCLI_SMOKE_FALLBACK_BUDGET:-3}" ;;
            esac
            ;;
        *)
            printf '%s' "${POCKETCLI_SMOKE_GENERIC_BUDGET:-5}"
            ;;
    esac
}

ci_is_excluded() {
    TEST_NAME="$1"
    for EXCLUDED in ${POCKETCLI_TEST_EXCLUDES:-}; do
        if [ "${EXCLUDED}" = "${TEST_NAME}" ]; then
            return 0
        fi
    done
    return 1
}

ci_run_script() {
    SCRIPT_PATH="$1"
    BUDGET="$2"
    LOG_FILE="$3"
    TEST_NAME=$(basename "${SCRIPT_PATH}")
    START_TS=$(ci_now_seconds)

    # Testes não podem herdar a stdin usada pelo loop que contém a lista.
    if sh "${SCRIPT_PATH}" </dev/null >"${LOG_FILE}" 2>&1; then
        STATUS=0
    else
        STATUS=$?
    fi

    END_TS=$(ci_now_seconds)
    DURATION=$((END_TS - START_TS))

    printf '%s\n' "==> ${TEST_NAME} (${DURATION}s)"
    cat "${LOG_FILE}"

    if [ "${STATUS}" -ne 0 ]; then
        printf 'FAIL %s exited with %s\n' "${TEST_NAME}" "${STATUS}" >&2
        ci_summary "- ${TEST_NAME}: fail (${DURATION}s, exit ${STATUS})"
        return "${STATUS}"
    fi

    if [ "${BUDGET}" -ge 0 ] && [ "${DURATION}" -gt "${BUDGET}" ]; then
        printf 'FAIL %s exceeded budget (%ss > %ss)\n' "${TEST_NAME}" "${DURATION}" "${BUDGET}" >&2
        ci_summary "- ${TEST_NAME}: fail (${DURATION}s > ${BUDGET}s budget)"
        return 1
    fi

    ci_summary "- ${TEST_NAME}: pass (${DURATION}s)"
    return 0
}

#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/pocketcli-capabilities.XXXXXX")
TOOLS_BIN="${TMP_DIR}/tools-bin"
EMPTY_BIN="${TMP_DIR}/empty-bin"
HOME_DIR="${TMP_DIR}/home"

cleanup() {
    rm -rf "${TMP_DIR}"
}

fail() {
    printf 'FAIL %s\n' "$1" >&2
    exit 1
}

assert_eq() {
    LABEL="$1"
    EXPECTED="$2"
    ACTUAL="$3"

    if [ "${ACTUAL}" != "${EXPECTED}" ]; then
        fail "${LABEL}: esperado '${EXPECTED}', obtido '${ACTUAL}'"
    fi
}

make_stub() {
    NAME="$1"
    cat > "${TOOLS_BIN}/${NAME}" <<'SH'
#!/usr/bin/env sh
exit 0
SH
    chmod +x "${TOOLS_BIN}/${NAME}"
}

run_detect() {
    BIN_DIR="$1"
    ISH_RC="$2"

    (
        set -eu
        PATH="${BIN_DIR}"
        POCKETCLI_DIR="${REPO_ROOT}"
        . "${REPO_ROOT}/scripts/runtime/capabilities.sh"

        is_ish() {
            return "${ISH_RC}"
        }

        pocket_detect_capabilities
        printf '%s|%s|%s|%s|%s\n' \
            "${HAS_TTY}" \
            "${HAS_TMUX}" \
            "${HAS_TAILSCALE}" \
            "${HAS_JQ}" \
            "${IS_ISH}"
    )
}

trap cleanup EXIT INT TERM HUP

mkdir -p "${TOOLS_BIN}" "${EMPTY_BIN}" "${HOME_DIR}"
make_stub tmux
make_stub tailscale
make_stub jq

RESULT=$(run_detect "${TOOLS_BIN}" 1)
assert_eq "tools-present" "false|true|true|true|false" "${RESULT}"

RESULT=$(run_detect "${EMPTY_BIN}" 1)
assert_eq "all-false" "false|false|false|false|false" "${RESULT}"

RESULT=$(run_detect "${TOOLS_BIN}" 0)
assert_eq "ish-true" "false|true|true|true|true" "${RESULT}"

REPEATED=$(
    (
        set -eu
        # shellcheck disable=SC2034
        POCKETCLI_DIR="${REPO_ROOT}"
        . "${REPO_ROOT}/scripts/runtime/capabilities.sh"

        is_ish() {
            return 1
        }

        PATH="${EMPTY_BIN}"
        pocket_detect_capabilities
        printf '%s\n' "${HAS_TTY}|${HAS_TMUX}|${HAS_TAILSCALE}|${HAS_JQ}|${IS_ISH}"

        is_ish() {
            return 0
        }

        PATH="${TOOLS_BIN}"
        pocket_detect_capabilities
        printf '%s\n' "${HAS_TTY}|${HAS_TMUX}|${HAS_TAILSCALE}|${HAS_JQ}|${IS_ISH}"
    )
)

FIRST_RESULT=$(printf '%s\n' "${REPEATED}" | sed -n '1p')
SECOND_RESULT=$(printf '%s\n' "${REPEATED}" | sed -n '2p')
assert_eq "repeat-first" "false|false|false|false|false" "${FIRST_RESULT}"
assert_eq "repeat-second" "false|true|true|true|true" "${SECOND_RESULT}"

ln -s "${REPO_ROOT}" "${HOME_DIR}/.pocketcli"
TRACE_FILE="${TMP_DIR}/manager.trace"

if ! HOME="${HOME_DIR}" \
    POCKETCLI_DIR="${HOME_DIR}/.pocketcli" \
    POCKETCLI_MENU_RENDER_ONCE=1 \
    sh -x "${REPO_ROOT}/scripts/session/manager.sh" auto >"${TRACE_FILE}" 2>&1; then
    fail "session-manager-trace"
fi

grep -q 'session_manager_main' "${TRACE_FILE}" || fail "trace-sem-session_manager_main"
grep -q 'pocket_detect_capabilities' "${TRACE_FILE}" || fail "trace-sem-capabilities"
grep -q 'pocket_transition context_ready' "${TRACE_FILE}" || fail "trace-sem-context_ready"
grep -q 'Ações rápidas' "${TRACE_FILE}" || fail "trace-cortado-antes-do-menu"

printf 'PASS capabilities hardening\n'

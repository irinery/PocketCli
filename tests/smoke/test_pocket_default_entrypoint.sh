#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
. "${REPO_ROOT}/tests/lib/test_helpers.sh"

WORKDIR=$(mktemp -d)
HOME_DIR="${WORKDIR}/home"
MOCKBIN="${WORKDIR}/mockbin"
LOG_FILE="${WORKDIR}/menu.log"
OUTPUT_FILE="${WORKDIR}/tty.out"
mkdir -p "${HOME_DIR}/.pocketcli/scripts/lib" "${HOME_DIR}/.pocketcli/state" "${MOCKBIN}"

cp "${REPO_ROOT}/pocket" "${HOME_DIR}/.pocketcli/pocket"
cp "${REPO_ROOT}/scripts/lib/common.sh" "${HOME_DIR}/.pocketcli/scripts/lib/common.sh"
chmod +x "${HOME_DIR}/.pocketcli/pocket"

cat > "${HOME_DIR}/.pocketcli/scripts/pocketcli_menu.sh" <<'EOS'
#!/usr/bin/env sh
set -eu
printf 'menu-invoked\n' >> "${POCKETCLI_TEST_LOG}"
exit 0
EOS
chmod +x "${HOME_DIR}/.pocketcli/scripts/pocketcli_menu.sh"

run_pty_command 3 "${OUTPUT_FILE}" "env HOME='${HOME_DIR}' PATH='${MOCKBIN}:/usr/bin:/bin' TMUX='1' POCKETCLI_TEST_LOG='${LOG_FILE}' sh '${HOME_DIR}/.pocketcli/pocket'"

grep -F 'menu-invoked' "${LOG_FILE}" >/dev/null 2>&1
grep -F 'menu' "${HOME_DIR}/.pocketcli/state/last-command" >/dev/null 2>&1
if grep -F 'help' "${HOME_DIR}/.pocketcli/state/last-command" >/dev/null 2>&1; then
    printf 'FAIL pocket defaulted to help instead of menu when /dev/tty is available\n' >&2
    exit 1
fi

printf 'PASS pocket defaults to menu when stdin is non-interactive and /dev/tty is available\n'


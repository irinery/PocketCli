#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
WORKDIR=$(mktemp -d)
HOME_DIR="${WORKDIR}/home"
MOCKBIN="${WORKDIR}/mockbin"
LOG_FILE="${WORKDIR}/tmux.log"
mkdir -p "${HOME_DIR}/.pocketcli/scripts/lib" "${MOCKBIN}"

cp "${REPO_ROOT}/pocket" "${HOME_DIR}/.pocketcli/pocket"
cp "${REPO_ROOT}/scripts/lib/common.sh" "${HOME_DIR}/.pocketcli/scripts/lib/common.sh"
chmod +x "${HOME_DIR}/.pocketcli/pocket"

cat > "${MOCKBIN}/tmux" <<'EOS'
#!/usr/bin/env sh
set -eu
printf 'tmux:%s\n' "$*" >> "${POCKETCLI_TEST_LOG}"
case "$1" in
    has-session) exit 1 ;;
    new-session)
        [ "$#" -eq 4 ] || exit 1
        [ "$2" = "-d" ] || exit 1
        [ "$3" = "-s" ] || exit 1
        [ "$4" = "pocketcli" ] || exit 1
        exit 0
        ;;
    send-keys)
        [ "$2" = "-t" ] || exit 1
        [ "$3" = "pocketcli" ] || exit 1
        [ "$4" = "POCKETCLI_RESTORE=1 '$HOME/.pocketcli/pocket' __restore" ] || exit 1
        [ "$5" = "C-m" ] || exit 1
        exit 0
        ;;
    source-file|attach-session) exit 0 ;;
    *) exit 0 ;;
esac
EOS
chmod +x "${MOCKBIN}/tmux"

env HOME="${HOME_DIR}" PATH="${MOCKBIN}:/usr/bin:/bin" POCKETCLI_TEST_LOG="${LOG_FILE}" sh "${HOME_DIR}/.pocketcli/pocket" resume >"${WORKDIR}/resume.out" 2>"${WORKDIR}/resume.err"

grep -F 'tmux:new-session -d -s pocketcli' "${LOG_FILE}" >/dev/null 2>&1
grep -F "tmux:send-keys -t pocketcli POCKETCLI_RESTORE=1 '${HOME_DIR}/.pocketcli/pocket' __restore C-m" "${LOG_FILE}" >/dev/null 2>&1
grep -F 'tmux:attach-session -t pocketcli' "${LOG_FILE}" >/dev/null 2>&1
grep -F 'menu' "${HOME_DIR}/.pocketcli/state/last-command" >/dev/null 2>&1

printf 'PASS pocket resume recreates tmux state and keeps the saved command contract\n'


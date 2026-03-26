#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
WORKDIR=$(mktemp -d)
HOME_DIR="${WORKDIR}/home"
MOCKBIN="${WORKDIR}/mockbin"
LOG_FILE="${WORKDIR}/git.log"
mkdir -p "${HOME_DIR}/.pocketcli/scripts/lib" "${HOME_DIR}/.pocketcli/profile" "${MOCKBIN}"

cp "${REPO_ROOT}/pocket" "${HOME_DIR}/.pocketcli/pocket"
cp "${REPO_ROOT}/scripts/lib/common.sh" "${HOME_DIR}/.pocketcli/scripts/lib/common.sh"
chmod +x "${HOME_DIR}/.pocketcli/pocket"

printf 'preserve-me\n' > "${HOME_DIR}/.pocketcli/profile/custom.txt"

cat > "${MOCKBIN}/git" <<'EOS'
#!/usr/bin/env sh
set -eu
printf '%s\n' "$*" >> "${POCKETCLI_TEST_GIT_LOG}"
if [ "$1" = "-C" ]; then
    REPO_DIR="$2"
    shift 2
else
    REPO_DIR=""
fi

case "$1" in
    rev-parse)
        if [ "${2:-}" = "--is-inside-work-tree" ]; then
            exit 1
        fi
        exit 0
        ;;
    clone)
        DEST=""
        for ARG in "$@"; do
            DEST="${ARG}"
        done
        mkdir -p "${DEST}/.git" "${DEST}/profile"
        printf '# cloned\n' > "${DEST}/pocket"
        printf 'shared-default\n' > "${DEST}/profile/shellrc"
        exit 0
        ;;
    fetch|pull|describe|stash|status)
        exit 0
        ;;
    *)
        exit 0
        ;;
esac
EOS
chmod +x "${MOCKBIN}/git"

env HOME="${HOME_DIR}" PATH="${MOCKBIN}:/usr/bin:/bin" POCKETCLI_TEST_GIT_LOG="${LOG_FILE}" sh "${HOME_DIR}/.pocketcli/pocket" update >"${WORKDIR}/update.out" 2>"${WORKDIR}/update.err"

grep -F 'clone --quiet --branch main https://github.com/irinery/PocketCli.git' "${LOG_FILE}" >/dev/null 2>&1
grep -F 'preserve-me' "${HOME_DIR}/.pocketcli/profile/custom.txt" >/dev/null 2>&1
if [ -e "${HOME_DIR}/.pocketcli/custom.txt" ]; then
    printf 'FAIL pocket update leaked custom profile data outside profile/\n' >&2
    exit 1
fi

printf 'PASS pocket update preserves user customizations inside profile/\n'

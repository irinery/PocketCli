#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
WORKDIR=$(mktemp -d)
HOME_DIR="${WORKDIR}/home"
mkdir -p "${HOME_DIR}/.pocketcli/scripts/lib"

cp "${REPO_ROOT}/pocket" "${HOME_DIR}/.pocketcli/pocket"
cp "${REPO_ROOT}/scripts/lib/common.sh" "${HOME_DIR}/.pocketcli/scripts/lib/common.sh"
chmod +x "${HOME_DIR}/.pocketcli/pocket"

env HOME="${HOME_DIR}" PATH="/usr/bin:/bin" sh "${HOME_DIR}/.pocketcli/pocket" help >"${WORKDIR}/shell.out" 2>"${WORKDIR}/shell.err"

grep -F 'PocketCli' "${WORKDIR}/shell.out" >/dev/null 2>&1
grep -F 'update' "${WORKDIR}/shell.out" >/dev/null 2>&1
grep -F 'resume' "${WORKDIR}/shell.out" >/dev/null 2>&1

if [ -n "${POCKETCLI_GO_BINARY:-}" ] && [ -x "${POCKETCLI_GO_BINARY}" ]; then
    "${POCKETCLI_GO_BINARY}" help >"${WORKDIR}/go.out" 2>"${WORKDIR}/go.err"
    grep -F 'PocketCli core CLI' "${WORKDIR}/go.out" >/dev/null 2>&1
    grep -F 'hosts' "${WORKDIR}/go.out" >/dev/null 2>&1
    grep -F 'ssh' "${WORKDIR}/go.out" >/dev/null 2>&1
fi

printf 'PASS pocket help returns contract help output for shell and go entrypoints\n'


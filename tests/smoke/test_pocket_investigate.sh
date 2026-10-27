#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
REAL_GO_BINARY=${POCKETCLI_GO_BINARY:-}
WORKDIR=$(mktemp -d)
trap 'rm -rf "${WORKDIR}"' 0 HUP INT TERM
HOME_DIR="${WORKDIR}/home"
mkdir -p "${HOME_DIR}/.pocketcli/scripts/lib"
cp "${REPO_ROOT}/pocket" "${HOME_DIR}/.pocketcli/pocket"
cp "${REPO_ROOT}/scripts/lib/common.sh" "${HOME_DIR}/.pocketcli/scripts/lib/common.sh"
cat >"${WORKDIR}/pocket-go" <<'SH'
#!/usr/bin/env sh
printf '%s\n' "$@" >"${POCKETCLI_ARG_FILE}"
SH
chmod +x "${WORKDIR}/pocket-go"

env HOME="${HOME_DIR}" POCKETCLI_GO_BINARY="${WORKDIR}/pocket-go" POCKETCLI_ARG_FILE="${WORKDIR}/args" sh "${HOME_DIR}/.pocketcli/pocket" investigate doctor >"${WORKDIR}/out" 2>"${WORKDIR}/err"

EXPECTED=$(printf 'investigate\ndoctor')
ACTUAL=$(cat "${WORKDIR}/args")
if [ "${ACTUAL}" != "${EXPECTED}" ]; then
    printf 'FAIL shell investigate dispatch: %s\n' "${ACTUAL}" >&2
    exit 1
fi

if [ -n "${REAL_GO_BINARY}" ] && [ -x "${REAL_GO_BINARY}" ]; then
    "${REAL_GO_BINARY}" investigate doctor >"${WORKDIR}/doctor.out" 2>"${WORKDIR}/doctor.err"
    grep -F 'investigate:' "${WORKDIR}/doctor.out" >/dev/null
	if "${REAL_GO_BINARY}" investigate --bad=1 --json >"${WORKDIR}/json.out" 2>"${WORKDIR}/json.err"; then
		printf 'FAIL investigate --json aceitou flag inválida\n' >&2
		exit 1
	fi
	if [ -s "${WORKDIR}/json.out" ] || ! grep -F '"ok": false' "${WORKDIR}/json.err" >/dev/null || ! grep -F '"code": "ERR_VALIDATION"' "${WORKDIR}/json.err" >/dev/null; then
		printf 'FAIL investigate --json não retornou erro estruturado no stderr\n' >&2
		exit 1
	fi
fi

printf 'PASS pocket investigate dispatch, doctor and structured JSON errors\n'

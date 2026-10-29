#!/usr/bin/env sh

# `pocket update` recompiles the Investigation Agent when it was built here
# and is behind the checkout. A fake `git` and `go` stand in for both, so
# nothing is fetched or compiled.

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
WORKDIR=$(mktemp -d)
trap 'rm -rf "${WORKDIR}"' 0 HUP INT TERM
HOME_DIR="${WORKDIR}/home"
FAKE_BIN="${WORKDIR}/fakebin"
CODE_DIR="${HOME_DIR}/.pocketcli"
mkdir -p "${CODE_DIR}/scripts/lib" "${CODE_DIR}/cmd/pocket" "${CODE_DIR}/bin" "${FAKE_BIN}"
cp "${REPO_ROOT}/pocket" "${CODE_DIR}/pocket"
cp "${REPO_ROOT}/scripts/lib/common.sh" "${CODE_DIR}/scripts/lib/common.sh"

cat >"${FAKE_BIN}/git" <<'SH'
#!/usr/bin/env sh
[ "$1" = "-C" ] && shift 2
case "$1" in
    rev-parse)
        if [ "${2:-}" = "HEAD" ]; then cat "${POCKETCLI_TEST_HEAD}"; fi
        exit 0 ;;
    *) exit 0 ;;
esac
SH
cat >"${FAKE_BIN}/go" <<'SH'
#!/usr/bin/env sh
OUT=''
while [ "$#" -gt 0 ]; do
    [ "$1" = "-o" ] && OUT="$2"
    shift
done
printf '%s\n' "${OUT}" >>"${POCKETCLI_BUILD_LOG}"
[ -n "${POCKETCLI_GO_FAIL:-}" ] && exit 1
printf '#!/usr/bin/env sh\nprintf "built\\n"\n' >"${OUT}"
chmod +x "${OUT}"
SH
cat >"${FAKE_BIN}/cc" <<'SH'
#!/usr/bin/env sh
exit 0
SH
chmod +x "${FAKE_BIN}/git" "${FAKE_BIN}/go" "${FAKE_BIN}/cc"

HEAD_FILE="${WORKDIR}/head"
BUILD_LOG="${WORKDIR}/build.log"
BIN="${CODE_DIR}/bin/pocket-go"

run_update() {
    env -u POCKETCLI_GO_BINARY -u POCKETCLI_INVESTIGATE_BUILD HOME="${HOME_DIR}" PATH="${FAKE_BIN}:/usr/bin:/bin" \
        POCKETCLI_TEST_HEAD="${HEAD_FILE}" POCKETCLI_BUILD_LOG="${BUILD_LOG}" "$@" \
        sh "${CODE_DIR}/pocket" update >"${WORKDIR}/out" 2>"${WORKDIR}/err" </dev/null
}

builds() { if [ -e "${BUILD_LOG}" ]; then wc -l <"${BUILD_LOG}" | tr -d ' '; else printf '0'; fi; }

fail() {
    printf 'FAIL update rebuilds the agent: %s\n' "$1" >&2
    exit 1
}

printf 'aaaa1111\n' >"${HEAD_FILE}"

# 1. Never built here: update does not build for the first time.
run_update || fail 'update falhou sem binário'
[ "$(builds)" = 0 ] || fail 'compilou sem o agente ter sido instalado antes'

# 2. Built before (no revision recorded): update recompiles and records HEAD.
printf '#!/usr/bin/env sh\nprintf "old\\n"\n' >"${BIN}"
chmod +x "${BIN}"
run_update || fail "update falhou: $(cat "${WORKDIR}/err")"
[ "$(builds)" = 1 ] || fail 'deveria recompilar o agente desatualizado'
[ "$("${BIN}")" = built ] || fail 'binário não foi substituído'
[ "$(cat "${BIN}.rev")" = aaaa1111 ] || fail 'commit compilado não foi registrado'
for leftover in "${CODE_DIR}/bin/"*.build.*; do
    [ ! -e "${leftover}" ] || fail 'arquivo temporário de build ficou para trás'
done

# 3. Same commit: nothing to rebuild.
run_update || fail 'segundo update falhou'
[ "$(builds)" = 1 ] || fail 'recompilou com o agente já no commit atual'

# 4. New commit: rebuild again.
printf 'bbbb2222\n' >"${HEAD_FILE}"
run_update || fail 'update com commit novo falhou'
[ "$(builds)" = 2 ] || fail 'não recompilou depois de um commit novo'
[ "$(cat "${BIN}.rev")" = bbbb2222 ] || fail 'commit novo não foi registrado'

# 5. A failed build keeps the previous binary and never fails the update.
printf 'cccc3333\n' >"${HEAD_FILE}"
run_update POCKETCLI_GO_FAIL=1 || fail 'build com falha derrubou o update'
[ "$("${BIN}")" = built ] || fail 'binário anterior não foi mantido'
[ "$(cat "${BIN}.rev")" = bbbb2222 ] || fail 'commit registrado mudou apesar da falha'
grep -F 'binário anterior foi mantido' "${WORKDIR}/err" "${WORKDIR}/out" >/dev/null || fail 'falha sem aviso nem comando manual'
for leftover in "${CODE_DIR}/bin/"*.build.*; do
    [ ! -e "${leftover}" ] || fail 'build com falha deixou temporário'
done

# 6. Opt-outs: POCKETCLI_INVESTIGATE_BUILD=no and an explicit binary.
before=$(builds)
run_update POCKETCLI_INVESTIGATE_BUILD=no || fail 'update com BUILD=no falhou'
run_update POCKETCLI_GO_BINARY="${WORKDIR}/custom-agent" || fail 'update com binário explícito falhou'
[ "$(builds)" = "${before}" ] || fail 'compilou apesar do opt-out'

printf 'PASS pocket update rebuilds the investigation agent when it is behind\n'

#!/usr/bin/env sh

# `pocket update` compiles the Investigation Agent in the background and
# releases it only when the build is complete. A fake `git` and `go` stand in
# for both (the fake `go` waits on a gate file), so nothing is fetched or
# compiled and the test controls when the build finishes.

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
waited=0
while [ -e "${POCKETCLI_GATE:-/nonexistent}" ] && [ "${waited}" -lt 200 ]; do
    sleep 0.1
    waited=$((waited + 1))
done
[ -n "${POCKETCLI_GO_FAIL:-}" ] && exit 1
printf '#!/usr/bin/env sh\nprintf "built %s\\n" "$*"\n' >"${OUT}"
chmod +x "${OUT}"
SH
cat >"${FAKE_BIN}/cc" <<'SH'
#!/usr/bin/env sh
exit 0
SH
chmod +x "${FAKE_BIN}/git" "${FAKE_BIN}/go" "${FAKE_BIN}/cc"

HEAD_FILE="${WORKDIR}/head"
BUILD_LOG="${WORKDIR}/build.log"
GATE="${WORKDIR}/gate"
BIN="${CODE_DIR}/bin/pocket-go"

pocket_env() {
    env -u POCKETCLI_GO_BINARY -u POCKETCLI_INVESTIGATE_BUILD -u POCKETCLI_INVESTIGATE_BUILD_WAIT \
        HOME="${HOME_DIR}" PATH="${FAKE_BIN}:/usr/bin:/bin" POCKETCLI_TEST_HEAD="${HEAD_FILE}" \
        POCKETCLI_BUILD_LOG="${BUILD_LOG}" POCKETCLI_GATE="${GATE}" "$@"
}
run_update() { pocket_env "$@" sh "${CODE_DIR}/pocket" update >"${WORKDIR}/out" 2>"${WORKDIR}/err" </dev/null; }
run_investigate() { pocket_env sh "${CODE_DIR}/pocket" investigate doctor >"${WORKDIR}/iout" 2>"${WORKDIR}/ierr" </dev/null; }

builds() { if [ -e "${BUILD_LOG}" ]; then wc -l <"${BUILD_LOG}" | tr -d ' '; else printf '0'; fi; }

# Waits until the background builder has released its lock.
wait_idle() {
    waited=0
    while [ -d "${BIN}.lock" ] && [ "${waited}" -lt 200 ]; do
        sleep 0.1
        waited=$((waited + 1))
    done
    [ ! -d "${BIN}.lock" ] || fail 'a compilação em segundo plano não terminou'
}

fail() {
    printf 'FAIL update builds the agent in the background: %s\n' "$1" >&2
    exit 1
}

printf 'aaaa1111\n' >"${HEAD_FILE}"

# 1. Not built yet: update returns while the build is still running, and
#    nothing is released until it finishes.
: >"${GATE}"
run_update || fail "update falhou: $(cat "${WORKDIR}/err")"
[ -d "${BIN}.lock" ] || fail 'update deveria deixar a compilação rodando em segundo plano'
[ ! -e "${BIN}" ] || fail 'binário liberado antes de a compilação terminar'
if run_investigate; then fail 'pocket investigate rodou sem binário'; fi
grep -F 'sendo compilado em segundo plano' "${WORKDIR}/ierr" >/dev/null || fail 'uso durante a compilação sem aviso'
[ "$(builds)" -le 1 ] || fail 'mais de um go build'
rm -f "${GATE}"
wait_idle
[ -x "${BIN}" ] || fail 'binário não foi liberado ao terminar'
[ "$(cat "${BIN}.rev")" = aaaa1111 ] || fail 'commit compilado não foi registrado'
[ "$(builds)" = 1 ] || fail 'deveria ter compilado uma vez'
for leftover in "${CODE_DIR}/bin/"*.build.[0-9]*; do
    [ ! -e "${leftover}" ] || fail 'arquivo temporário de build ficou para trás'
done

# 2. Same commit: nothing to rebuild.
run_update || fail 'segundo update falhou'
[ "$(builds)" = 1 ] || fail 'recompilou com o agente já no commit atual'

# 3. New commit: the old binary keeps serving while the new one builds.
printf 'bbbb2222\n' >"${HEAD_FILE}"
: >"${GATE}"
run_update || fail 'update com commit novo falhou'
[ "$(cat "${BIN}.rev")" = aaaa1111 ] || fail 'binário trocado antes da hora'
run_investigate || fail "o binário atual deveria continuar servindo: $(cat "${WORKDIR}/ierr")"
grep -F 'versão atual até terminar' "${WORKDIR}/ierr" >/dev/null || fail 'sem aviso de compilação em andamento'
# A second update while it builds does not start another build.
run_update || fail 'update durante a compilação falhou'
rm -f "${GATE}"
wait_idle
[ "$(cat "${BIN}.rev")" = bbbb2222 ] || fail 'commit novo não foi registrado'
[ "$(builds)" = 2 ] || fail "esperava 2 builds no total, houve $(builds)"

# 4. A failed build keeps the previous binary, marks the failure and warns.
printf 'cccc3333\n' >"${HEAD_FILE}"
run_update POCKETCLI_GO_FAIL=1 || fail 'build com falha derrubou o update'
wait_idle
[ "$(cat "${BIN}.rev")" = bbbb2222 ] || fail 'commit registrado mudou apesar da falha'
[ -e "${BIN}.failed" ] || fail 'falha sem marcador'
run_investigate || fail 'binário anterior deveria continuar servindo'
grep -F 'última compilação do Investigation Agent falhou' "${WORKDIR}/ierr" >/dev/null || fail 'falha anterior sem aviso'
for leftover in "${CODE_DIR}/bin/"*.build.[0-9]*; do
    [ ! -e "${leftover}" ] || fail 'build com falha deixou temporário'
done

# 5. POCKETCLI_INVESTIGATE_BUILD_WAIT=yes waits and releases before returning.
run_update POCKETCLI_INVESTIGATE_BUILD_WAIT=yes || fail 'update com espera falhou'
[ ! -e "${BIN}.failed" ] || fail 'marcador de falha não foi limpo após sucesso'
[ "$(cat "${BIN}.rev")" = cccc3333 ] || fail 'update com espera não liberou o binário'

# 6. Opt-outs: POCKETCLI_INVESTIGATE_BUILD=no and an explicit binary.
printf 'dddd4444\n' >"${HEAD_FILE}"
before=$(builds)
run_update POCKETCLI_INVESTIGATE_BUILD=no || fail 'update com BUILD=no falhou'
run_update POCKETCLI_GO_BINARY="${WORKDIR}/custom-agent" || fail 'update com binário explícito falhou'
[ "$(builds)" = "${before}" ] || fail 'compilou apesar do opt-out'

# 7. Without Go the update still succeeds and says what to run.
rm -f "${FAKE_BIN}/go"
run_update || fail 'update sem Go falhou'
grep -F 'sem Go/compilador C' "${WORKDIR}/err" "${WORKDIR}/out" >/dev/null || fail 'sem Go e sem aviso'

printf 'PASS pocket update compiles the investigation agent in the background and releases it when done\n'

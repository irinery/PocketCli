#!/usr/bin/env sh

# The first `pocket investigate` offers to build the Go agent. A fake `go`
# stands in for the compiler so the test never compiles anything.

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
WORKDIR=$(mktemp -d)
trap 'rm -rf "${WORKDIR}"' 0 HUP INT TERM
HOME_DIR="${WORKDIR}/home"
FAKE_BIN="${WORKDIR}/fakebin"
mkdir -p "${HOME_DIR}/.pocketcli/scripts/lib" "${HOME_DIR}/.pocketcli/cmd/pocket" "${FAKE_BIN}"
cp "${REPO_ROOT}/pocket" "${HOME_DIR}/.pocketcli/pocket"
cp "${REPO_ROOT}/scripts/lib/common.sh" "${HOME_DIR}/.pocketcli/scripts/lib/common.sh"

cat >"${FAKE_BIN}/go" <<'SH'
#!/usr/bin/env sh
# Fake `go build ... -o OUT`: record the call and write an agent that echoes its args.
OUT=''
while [ "$#" -gt 0 ]; do
    [ "$1" = "-o" ] && OUT="$2"
    shift
done
printf '%s %s\n' "${CGO_ENABLED:-}" "${OUT}" >>"${POCKETCLI_BUILD_LOG}"
[ -n "${OUT}" ] || exit 1
printf '#!/usr/bin/env sh\nprintf "agent:%%s\\n" "$*"\n' >"${OUT}"
chmod +x "${OUT}"
SH
cat >"${FAKE_BIN}/cc" <<'SH'
#!/usr/bin/env sh
exit 0
SH
chmod +x "${FAKE_BIN}/go" "${FAKE_BIN}/cc"

run_pocket() {
    env -u POCKETCLI_GO_BINARY HOME="${HOME_DIR}" PATH="${FAKE_BIN}:/usr/bin:/bin" POCKETCLI_BUILD_LOG="${WORKDIR}/build.log" "$@" \
        sh "${HOME_DIR}/.pocketcli/pocket" investigate doctor </dev/null
}

fail() {
    printf 'FAIL investigate build offer: %s\n' "$1" >&2
    exit 1
}

# 1. Without a terminal the build is refused, with the command to run.
if run_pocket >"${WORKDIR}/out" 2>"${WORKDIR}/err"; then
    fail 'compilou sem terminal nem POCKETCLI_INVESTIGATE_BUILD=yes'
fi
grep -F 'POCKETCLI_INVESTIGATE_BUILD=yes' "${WORKDIR}/err" >/dev/null || fail 'erro sem a dica de compilar sob demanda'
[ ! -e "${WORKDIR}/build.log" ] || fail 'go build rodou sem confirmação'

# 2. POCKETCLI_INVESTIGATE_BUILD=no never builds.
if run_pocket POCKETCLI_INVESTIGATE_BUILD=no >"${WORKDIR}/out" 2>"${WORKDIR}/err"; then
    fail 'compilou com POCKETCLI_INVESTIGATE_BUILD=no'
fi
[ ! -e "${WORKDIR}/build.log" ] || fail 'go build rodou com POCKETCLI_INVESTIGATE_BUILD=no'

# 3. =yes builds once into bin/pocket-go with CGO and runs the agent with the args.
run_pocket POCKETCLI_INVESTIGATE_BUILD=yes >"${WORKDIR}/out" 2>"${WORKDIR}/err" || fail "build sob demanda falhou: $(cat "${WORKDIR}/err")"
[ "$(cat "${WORKDIR}/out")" = "agent:investigate doctor" ] || fail "agente não recebeu os argumentos: $(cat "${WORKDIR}/out")"
[ "$(wc -l <"${WORKDIR}/build.log")" -eq 1 ] || fail 'go build deveria rodar uma vez'
grep -q '^1 ' "${WORKDIR}/build.log" || fail 'go build sem CGO_ENABLED=1'
[ -x "${HOME_DIR}/.pocketcli/bin/pocket-go" ] || fail 'binário não foi instalado'
for build_temp in "${HOME_DIR}/.pocketcli/bin/"*.build.*; do
    [ ! -e "${build_temp}" ] || fail 'arquivo temporário de build ficou para trás'
done

# 4. With the binary in place nothing builds again.
run_pocket >"${WORKDIR}/out" 2>"${WORKDIR}/err" || fail "segunda chamada falhou: $(cat "${WORKDIR}/err")"
[ "$(wc -l <"${WORKDIR}/build.log")" -eq 1 ] || fail 'recompilou com o binário já instalado'

# 5. A failed build leaves no binary behind.
rm -f "${HOME_DIR}/.pocketcli/bin/pocket-go" "${WORKDIR}/build.log"
cat >"${FAKE_BIN}/go" <<'SH'
#!/usr/bin/env sh
exit 1
SH
if run_pocket POCKETCLI_INVESTIGATE_BUILD=yes >"${WORKDIR}/out" 2>"${WORKDIR}/err"; then
    fail 'aceitou um build que falhou'
fi
[ ! -e "${HOME_DIR}/.pocketcli/bin/pocket-go" ] || fail 'binário parcial instalado após falha'
grep -F 'Build failed' "${WORKDIR}/err" >/dev/null || fail 'falha de build sem mensagem'

printf 'PASS pocket investigate offers to build the agent on first use\n'

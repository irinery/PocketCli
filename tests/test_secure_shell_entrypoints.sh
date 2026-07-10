#!/usr/bin/env sh
# Verifica que entradas shell não retornam a SSH/SCP/fleet diretos quando o
# wrapper seguro ou binário Go não está disponível.

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
HOME_DIR="${WORKDIR}/home"
MOCKBIN="${WORKDIR}/mockbin"
mkdir -p "${HOME_DIR}/.pocketcli/scripts/lib" "${MOCKBIN}"
trap 'rm -rf "${WORKDIR}"' EXIT INT TERM

cp "${REPO_ROOT}/pocket" "${HOME_DIR}/.pocketcli/pocket"
cp "${REPO_ROOT}/scripts/lib/common.sh" "${HOME_DIR}/.pocketcli/scripts/lib/common.sh"
chmod +x "${HOME_DIR}/.pocketcli/pocket"

cat > "${MOCKBIN}/ssh" <<'EOF'
#!/usr/bin/env sh
printf 'unexpected ssh invocation\n' >&2
exit 99
EOF
cat > "${MOCKBIN}/scp" <<'EOF'
#!/usr/bin/env sh
printf 'unexpected scp invocation\n' >&2
exit 99
EOF
chmod +x "${MOCKBIN}/ssh" "${MOCKBIN}/scp"

expect_secure_wrapper() {
    label="$1"
    shift
    if env HOME="${HOME_DIR}" PATH="${MOCKBIN}:/usr/bin:/bin" sh "${HOME_DIR}/.pocketcli/pocket" "$@" >"${WORKDIR}/out" 2>"${WORKDIR}/err"; then
        printf 'FAIL expected secure wrapper requirement for: %s\n' "${label}" >&2
        exit 1
    fi
    grep -F 'Secure SSH wrapper unavailable' "${WORKDIR}/err" >/dev/null
}

expect_secure_wrapper connect connect prod-api
expect_secure_wrapper run run prod-api uptime
expect_secure_wrapper copy copy prod-api:/tmp/a ./a

if env HOME="${HOME_DIR}" PATH="${MOCKBIN}:/usr/bin:/bin" POCKETCLI_DIR="${REPO_ROOT}" sh "${REPO_ROOT}/scripts/fleet.sh" run uptime >"${WORKDIR}/fleet.out" 2>"${WORKDIR}/fleet.err"; then
    printf 'FAIL expected legacy fleet entrypoint to require Go binary\n' >&2
    exit 1
fi
grep -F 'Fleet seguro requer o binário Go' "${WORKDIR}/fleet.err" >/dev/null

printf 'PASS shell entrypoints fail closed without the secure backend\n'

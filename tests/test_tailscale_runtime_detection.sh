#!/usr/bin/env sh

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
HOME_DIR="${WORKDIR}/home"
MOCKBIN="${WORKDIR}/mockbin"
NATIVE_DIR="${WORKDIR}/native"
LOG_FILE="${WORKDIR}/calls.log"
trap 'rm -rf "${WORKDIR}"' EXIT INT TERM HUP

mkdir -p "${HOME_DIR}/.pocketcli/lib" "${HOME_DIR}/.pocketcli/scripts" "${MOCKBIN}" "${NATIVE_DIR}"
cp "${REPO_ROOT}/lib/common.sh" "${HOME_DIR}/.pocketcli/lib/common.sh"
cp "${REPO_ROOT}/scripts/tailscale_daemon.sh" "${HOME_DIR}/.pocketcli/scripts/tailscale_daemon.sh"
cp "${REPO_ROOT}/scripts/tailscale_ip.sh" "${HOME_DIR}/.pocketcli/scripts/tailscale_ip.sh"
chmod +x "${HOME_DIR}/.pocketcli/scripts/tailscale_daemon.sh" \
    "${HOME_DIR}/.pocketcli/scripts/tailscale_ip.sh"

fail() {
    printf 'FAIL %s\n' "$1" >&2
    exit 1
}

assert_contains() {
    printf '%s\n' "$1" | grep -F "$2" >/dev/null 2>&1 || fail "$3"
}

assert_not_contains() {
    if printf '%s\n' "$1" | grep -F "$2" >/dev/null 2>&1; then
        fail "$3"
    fi
}

cat > "${NATIVE_DIR}/tailscale.exe" <<'EOS'
#!/usr/bin/env sh
printf '%s|%s\n' "${TAILSCALE_BE_CLI:-missing}" "$*" >> "${POCKETCLI_TEST_LOG}"
case "${1:-} ${2:-}" in
    'status --json')
        printf '{"BackendState":"%s","Self":{"TailscaleIPs":["100.72.10.20"]},"Peer":{"node":{"HostName":"server-a","TailscaleIPs":["100.82.20.30"],"Online":true}}}\n' \
            "${POCKETCLI_TEST_BACKEND_STATE:-Running}"
        ;;
    'ip -4')
        printf '%s\n' '100.72.10.20'
        ;;
    'status ')
        printf '%s\n' '100.82.20.30 server-a user@ linux active; direct'
        ;;
    *)
        exit 0
        ;;
esac
EOS

cat > "${MOCKBIN}/pgrep" <<'EOS'
#!/usr/bin/env sh
exit 1
EOS

cat > "${MOCKBIN}/apk" <<'EOS'
#!/usr/bin/env sh
printf 'apk:%s\n' "$*" >> "${POCKETCLI_TEST_LOG}"
exit 1
EOS

cat > "${MOCKBIN}/ip" <<'EOS'
#!/usr/bin/env sh
printf '9: tailscale0: <POINTOPOINT,UP> mtu 1280\n'
printf '    inet %s/32 scope global tailscale0\n' "${POCKETCLI_TEST_INTERFACE_IP:-192.0.2.10}"
EOS

chmod +x "${NATIVE_DIR}/tailscale.exe" "${MOCKBIN}/pgrep" "${MOCKBIN}/apk" "${MOCKBIN}/ip"

OUTPUT=$(env \
    HOME="${HOME_DIR}" \
    PATH="${MOCKBIN}:/usr/bin:/bin" \
    POCKETCLI_TEST_LOG="${LOG_FILE}" \
    POCKETCLI_TAILSCALE_CLI="${NATIVE_DIR}/tailscale.exe" \
    sh "${HOME_DIR}/.pocketcli/scripts/tailscale_daemon.sh" setup)

assert_contains "${OUTPUT}" 'Tailscale mesh already operational (IP: 100.72.10.20).' \
    'native CLI operational was not accepted'
assert_contains "${OUTPUT}" 'Keeping the existing system/app-managed installation.' \
    'native installation was not preserved'
assert_not_contains "${OUTPUT}" 'tailscaled is not installed' \
    'native backend still required tailscaled process'
grep -F '1|status --json' "${LOG_FILE}" >/dev/null 2>&1 \
    || fail 'macOS CLI mode environment was not forced'
if grep -F 'apk:' "${LOG_FILE}" >/dev/null 2>&1; then
    fail 'installer ran despite operational native mesh'
fi

START_OUTPUT=$(env \
    HOME="${HOME_DIR}" \
    PATH="${MOCKBIN}:/usr/bin:/bin" \
    POCKETCLI_TEST_LOG="${LOG_FILE}" \
    POCKETCLI_TAILSCALE_CLI="${NATIVE_DIR}/tailscale.exe" \
    sh "${HOME_DIR}/.pocketcli/scripts/tailscale_daemon.sh" start)
assert_contains "${START_OUTPUT}" 'no extra daemon was started' \
    'tailscale-start did not reuse native backend'

TMUX_IP=$(env \
    HOME="${HOME_DIR}" \
    PATH="${MOCKBIN}:/usr/bin:/bin" \
    POCKETCLI_TEST_LOG="${LOG_FILE}" \
    POCKETCLI_TAILSCALE_CLI="${NATIVE_DIR}/tailscale.exe" \
    sh "${HOME_DIR}/.pocketcli/scripts/tailscale_ip.sh")
[ "${TMUX_IP}" = '100.72.10.20' ] || fail 'tmux Tailscale IP helper ignored native CLI'

: > "${LOG_FILE}"
INTERFACE_OUTPUT=$(env \
    HOME="${HOME_DIR}" \
    PATH="${MOCKBIN}:/usr/bin:/bin" \
    POCKETCLI_TEST_LOG="${LOG_FILE}" \
    POCKETCLI_TAILSCALE_CLI="${WORKDIR}/missing-tailscale" \
    POCKETCLI_TEST_INTERFACE_IP='100.100.20.30' \
    sh "${HOME_DIR}/.pocketcli/scripts/tailscale_daemon.sh" setup)
assert_contains "${INTERFACE_OUTPUT}" 'Tailscale mesh already operational (IP: 100.100.20.30).' \
    'OS-managed interface fallback was not accepted'
assert_contains "${INTERFACE_OUTPUT}" 'system-managed VPN' \
    'OS-managed interface status was not reported'
if grep -F 'apk:' "${LOG_FILE}" >/dev/null 2>&1; then
    fail 'installer ran despite operational OS-managed interface'
fi

# shellcheck disable=SC2016
if env \
    HOME="${HOME_DIR}" \
    PATH="${MOCKBIN}:/usr/bin:/bin" \
    POCKETCLI_TEST_LOG="${LOG_FILE}" \
    POCKETCLI_TAILSCALE_CLI="${WORKDIR}/missing-tailscale" \
    POCKETCLI_TEST_INTERFACE_IP='100.10.20.30' \
    sh -c '. "$1"; is_tailscale_mesh_operational' sh \
    "${HOME_DIR}/.pocketcli/lib/common.sh"; then
    fail 'address outside 100.64.0.0/10 was accepted as Tailscale'
fi

# shellcheck disable=SC2016
if env \
    HOME="${HOME_DIR}" \
    PATH="${MOCKBIN}:/usr/bin:/bin" \
    POCKETCLI_TEST_LOG="${LOG_FILE}" \
    POCKETCLI_TAILSCALE_CLI="${NATIVE_DIR}/tailscale.exe" \
    POCKETCLI_TEST_BACKEND_STATE='Stopped' \
    POCKETCLI_TEST_INTERFACE_IP='192.0.2.10' \
    sh -c '. "$1"; is_tailscale_mesh_operational' sh \
    "${HOME_DIR}/.pocketcli/lib/common.sh"; then
    fail 'stopped backend with stale Tailscale IP was accepted as operational'
fi

printf 'PASS tailscale runtime detects native and OS-managed backends without redundant install\n'

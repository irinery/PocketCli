#!/usr/bin/env sh
# =============================================================================
# PocketCli — scripts/install_requirements.sh
# Preferred path: install host requirements through the local Ansible playbook.
# Fallback path: use the legacy shell bootstrap to obtain Ansible/core packages.
# =============================================================================

set -eu

OS="${1:-}"
MODE="${2:-}"
SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
INSTALL_DIR=$(CDPATH='' cd -- "${SCRIPT_DIR}/.." && pwd)
PLAYBOOK="${INSTALL_DIR}/scripts/setup/requirements.yml"
FALLBACK_INSTALLER="${SCRIPT_DIR}/install_deps.sh"

[ -z "${OS}" ] && { printf '[PocketCli] OS not provided\n' >&2; exit 1; }
[ -z "${MODE}" ] && { printf '[PocketCli] MODE not provided\n' >&2; exit 1; }

case "${MODE}" in
    viewer|agent) ;;
    *) printf '[PocketCli] Unknown mode: %s\n' "${MODE}" >&2; exit 1 ;;
esac

run_ansible_requirements() {
    command -v ansible-playbook >/dev/null 2>&1 || return 127
    [ -f "${PLAYBOOK}" ] || {
        printf '[PocketCli] Requirements playbook not found: %s\n' "${PLAYBOOK}" >&2
        return 1
    }

    printf '[PocketCli] Installing system requirements with Ansible...\n'
    ansible-playbook \
        -i localhost, \
        -c local \
        "${PLAYBOOK}" \
        -e "pocketcli_os=${OS}" \
        -e "pocketcli_mode=${MODE}"
}

set +e
run_ansible_requirements
RC=$?
set -e

if [ "${RC}" -eq 0 ]; then
    printf '[PocketCli] System requirements ready.\n'
    exit 0
fi

[ "${RC}" -eq 127 ] || exit "${RC}"

printf '[PocketCli] Ansible not available yet; bootstrapping requirements with shell fallback.\n'
sh "${FALLBACK_INSTALLER}" "${OS}" "${MODE}"

set +e
run_ansible_requirements
RC=$?
set -e

if [ "${RC}" -eq 0 ]; then
    printf '[PocketCli] System requirements ready.\n'
    exit 0
fi

if [ "${RC}" -eq 127 ] && [ "${MODE}" = "viewer" ]; then
    printf '[PocketCli] Viewer requirements ready; Ansible is not required at runtime.\n'
    exit 0
fi

exit "${RC}"

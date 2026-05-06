#!/usr/bin/env sh
set -eu

POCKETCLI_DIR="${HOME}/.pocketcli"

if [ "${1:-}" != "--legacy-direct" ] && [ -x "${POCKETCLI_DIR}/scripts/session/manager.sh" ]; then
    SESSION_INTENT="viewer" exec sh "${POCKETCLI_DIR}/scripts/session/manager.sh" viewer
fi

. "${POCKETCLI_DIR}/lib/common.sh"
export PATH="${POCKETCLI_DIR}:${PATH}"

mkdir -p "${HOME}/.ssh"
chmod 700 "${HOME}/.ssh"
SSH_CONFIG="${HOME}/.ssh/config"
if ! grep -qF "ForwardAgent" "${SSH_CONFIG}" 2>/dev/null; then
    cat >> "${SSH_CONFIG}" << 'SSHEOF'

# PocketCli
Host *
    ForwardAgent no
    ServerAliveInterval 60
    ServerAliveCountMax 3
SSHEOF
    chmod 600 "${SSH_CONFIG}"
fi

exec sh "${POCKETCLI_DIR}/scripts/pocketcli_menu.sh"

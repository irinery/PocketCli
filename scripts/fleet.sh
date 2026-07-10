#!/usr/bin/env sh
# Legacy shell entrypoint for fleet. Remote execution is intentionally delegated
# to the Go executor so plans, approvals, transport policy and audit stay unified.

set -eu

POCKETCLI_DIR="${POCKETCLI_DIR:-${HOME}/.pocketcli}"
GO_BINARY="${POCKETCLI_GO_BINARY:-${POCKETCLI_DIR}/bin/pocket-go}"

if [ -x "${GO_BINARY}" ]; then
    exec "${GO_BINARY}" fleet "$@"
fi

printf '%s\n' "[PocketCli] Fleet seguro requer o binário Go em ${GO_BINARY}." >&2
printf '%s\n' "[PocketCli] Defina POCKETCLI_GO_BINARY ou instale ${POCKETCLI_DIR}/bin/pocket-go." >&2
exit 64

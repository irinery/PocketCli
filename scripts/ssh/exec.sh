#!/usr/bin/env sh
set -eu
POCKETCLI_DIR="${POCKETCLI_DIR:-${HOME}/.pocketcli}"
exec sh "${POCKETCLI_DIR}/scripts/ssh/open.sh" --run exec "$@"

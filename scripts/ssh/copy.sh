#!/usr/bin/env sh
set -eu
POCKETCLI_DIR="${POCKETCLI_DIR:-${HOME}/.pocketcli}"
[ $# -lt 2 ] && { printf 'Usage: pocket copy <src> <dst>\n' >&2; exit 1; }

SRC="$1"
DST="$2"

extract_host() {
    VALUE="$1"
    case "${VALUE}" in
        *:*)
            printf '%s' "${VALUE%%:*}" | tr -cd 'a-zA-Z0-9._-'
            ;;
        *)
            printf ''
            ;;
    esac
}

HOST=$(extract_host "${SRC}")
[ -n "${HOST}" ] || HOST=$(extract_host "${DST}")

if [ -z "${HOST}" ]; then
    # no remote endpoint: preserve legacy scp wrapper behavior
    exec scp -r "${SRC}" "${DST}"
fi

exec sh "${POCKETCLI_DIR}/scripts/ssh/open.sh" --run copy "${HOST}" "${SRC}" "${DST}"

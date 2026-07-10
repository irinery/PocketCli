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
            RAW_HOST=${VALUE%%:*}
            SAFE_HOST=$(printf '%s' "${RAW_HOST}" | tr -cd 'a-zA-Z0-9._-')
            [ -n "${SAFE_HOST}" ] && [ "${SAFE_HOST}" = "${RAW_HOST}" ] || return 1
            printf '%s' "${SAFE_HOST}"
            ;;
        *)
            printf ''
            ;;
    esac
}

case "${SRC}" in
    *:*) HOST=$(extract_host "${SRC}") || { printf 'Invalid remote source.\n' >&2; exit 1; } ;;
    *) HOST='' ;;
esac
if [ -z "${HOST}" ]; then
    case "${DST}" in
        *:*) HOST=$(extract_host "${DST}") || { printf 'Invalid remote destination.\n' >&2; exit 1; } ;;
    esac
fi

case "${SRC}" in -*) printf 'Invalid source path. Prefix local paths with ./\n' >&2; exit 1 ;; esac
case "${DST}" in -*) printf 'Invalid destination path. Prefix local paths with ./\n' >&2; exit 1 ;; esac

if [ -z "${HOST}" ]; then
    # no remote endpoint: preserve legacy scp wrapper behavior
    exec scp -r "${SRC}" "${DST}"
fi

exec sh "${POCKETCLI_DIR}/scripts/ssh/open.sh" --run copy "${HOST}" "${SRC}" "${DST}"

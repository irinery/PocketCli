#!/usr/bin/env sh
# shellcheck disable=SC2016

set -eu

TAG="${1:-}"
CUSTOM_FILE="${2:-}"
DISPLAY_TAG="${RELEASE_DISPLAY_TAG:-${TAG}}"
COMMIT_SHA="${RELEASE_COMMIT_SHA:-}"

if [ -z "${TAG}" ]; then
    printf 'uso: %s <tag> [custom_file]\n' "$0" >&2
    exit 1
fi

printf '## PocketCli %s\n\n' "${DISPLAY_TAG}"

if [ -n "${CUSTOM_FILE}" ] && [ -f "${CUSTOM_FILE}" ]; then
    cat "${CUSTOM_FILE}"
    printf '\n\n'
fi

if [ -n "${COMMIT_SHA}" ]; then
    printf 'Commit: `%s`\n\n' "${COMMIT_SHA}"
fi

cat <<EOF
### Install

\`\`\`bash
curl -fsSL https://raw.githubusercontent.com/irinery/PocketCli/${TAG}/bootstrap.sh -o bootstrap.sh
sh bootstrap.sh
\`\`\`

### Verify checksum (recommended)

\`\`\`bash
curl -fsSL https://raw.githubusercontent.com/irinery/PocketCli/${TAG}/bootstrap.sh -o bootstrap.sh
curl -fsSL https://github.com/irinery/PocketCli/releases/download/${TAG}/checksums.sha256 -o checksums.sha256
sha256sum -c checksums.sha256
bash bootstrap.sh
\`\`\`
EOF

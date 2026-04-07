#!/usr/bin/env sh

set -eu

TAG="${1:-}"
CUSTOM_FILE="${2:-}"

if [ -z "${TAG}" ]; then
    printf 'uso: %s <tag> [custom_file]\n' "$0" >&2
    exit 1
fi

printf '## PocketCli %s\n\n' "${TAG}"

if [ -n "${CUSTOM_FILE}" ] && [ -f "${CUSTOM_FILE}" ]; then
    cat "${CUSTOM_FILE}"
    printf '\n\n'
fi

cat <<EOF
### Install

\`\`\`bash
curl -fsSL https://raw.githubusercontent.com/irinery/PocketCli/${TAG}/bootstrap.sh | bash
\`\`\`

### Verify checksum (recommended)

\`\`\`bash
curl -fsSL https://raw.githubusercontent.com/irinery/PocketCli/${TAG}/bootstrap.sh -o bootstrap.sh
curl -fsSL https://github.com/irinery/PocketCli/releases/download/${TAG}/checksums.sha256 -o checksums.sha256
sha256sum -c checksums.sha256
bash bootstrap.sh
\`\`\`
EOF

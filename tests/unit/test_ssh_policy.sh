#!/usr/bin/env sh
set -eu

WORKDIR=$(mktemp -d)
HOME_DIR="$WORKDIR/home"
mkdir -p "$HOME_DIR"

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
# Consumed by sourced SSH policy helpers below.
# shellcheck disable=SC2034
POCKETCLI_DIR="$REPO_ROOT"
HOME="$HOME_DIR"

. "$REPO_ROOT/scripts/runtime/paths.sh"
. "$REPO_ROOT/scripts/ssh/policy.sh"

ssh_policy_ensure_file
[ -s "$(pocket_ssh_policy_file)" ]
[ "$(ssh_policy_get strict_mode)" = "false" ]
[ "$(ssh_policy_get connect_timeout_sec)" = "10" ]

cat > "$(pocket_ssh_policy_file)" <<'EOF'
{"host_key_policy":"accept-new -o ProxyCommand=malicious","connect_timeout_sec":"10 -o ProxyCommand=malicious"}
EOF
chmod 600 "$(pocket_ssh_policy_file)"
[ "$(ssh_policy_get host_key_policy)" = "accept-new" ]
[ "$(ssh_policy_get connect_timeout_sec)" = "10" ]

echo "PASS ssh policy defaults and rejects option injection"

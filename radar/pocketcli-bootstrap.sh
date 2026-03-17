#!/usr/bin/env sh
# =============================================================================
# PocketCli — radar/pocketcli-bootstrap.sh
# Pushes the radar agent script to a remote host and starts it.
# Called by: pocket radar-bootstrap <host>
# =============================================================================

set -eu

POCKETCLI_DIR="${HOME}/.pocketcli"
. "${POCKETCLI_DIR}/lib/common.sh"

HOST="${1:-}"
[ -z "${HOST}" ] && die "Usage: $0 <hostname>"
HOST=$(safe_host "${HOST}")
[ -z "${HOST}" ] && die "Invalid hostname."

AGENT_SCRIPT="${POCKETCLI_DIR}/scripts/pocket-status.sh"
REMOTE_AGENT=".pocketcli-agent.sh"

step "Bootstrapping radar agent on ${HOST}"

# 1. Test reachability — -n prevents stdin swallow inside conditionals
if ! ssh -n -o ConnectTimeout=3 -o BatchMode=yes \
        "${HOST}" "echo ok" >/dev/null 2>&1; then
    die "Cannot reach ${HOST}. Check SSH access and Tailscale."
fi
ok "Host reachable: ${HOST}"

# 2. Copy agent script
info "Copying agent to ${HOST}:~/${REMOTE_AGENT}..."
scp -q "${AGENT_SCRIPT}" "${HOST}:${REMOTE_AGENT}"

# 3. Make executable — -n required: called outside interactive context
ssh -n -o BatchMode=yes \
    "${HOST}" "chmod +x ~/${REMOTE_AGENT}"
ok "Agent installed."

# 4. Run agent once to verify
info "Running agent on ${HOST}..."
ssh -n -o BatchMode=yes \
    "${HOST}" \
    "sh ~/${REMOTE_AGENT}" 2>/dev/null \
    || warn "Agent ran with errors on ${HOST} — check manually."

ok "Bootstrap complete: ${HOST}"

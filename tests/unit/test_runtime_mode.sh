#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
# Consumed by sourced runtime scripts below.
# shellcheck disable=SC2034
POCKETCLI_DIR="$REPO_ROOT"
. "$REPO_ROOT/lib/common.sh"
. "$REPO_ROOT/scripts/runtime/capabilities.sh"
. "$REPO_ROOT/scripts/runtime/mode.sh"

# Consumed by pocket_determine_mode via sourced script.
# shellcheck disable=SC2034
HAS_TTY=true
# Consumed by pocket_determine_mode via sourced script.
# shellcheck disable=SC2034
HAS_TMUX=true
# Consumed by pocket_determine_mode via sourced script.
# shellcheck disable=SC2034
IS_ISH=false
pocket_determine_mode auto
[ "$MODE_EFFECTIVE" = "agent" ]

# Consumed by pocket_determine_mode via sourced script.
# shellcheck disable=SC2034
HAS_TTY=false
# Consumed by pocket_determine_mode via sourced script.
# shellcheck disable=SC2034
HAS_TMUX=false
# Consumed by pocket_determine_mode via sourced script.
# shellcheck disable=SC2034
IS_ISH=false
pocket_determine_mode auto
[ "$MODE_EFFECTIVE" = "viewer" ]

# Consumed by pocket_determine_mode via sourced script.
# shellcheck disable=SC2034
HAS_TTY=true
# Consumed by pocket_determine_mode via sourced script.
# shellcheck disable=SC2034
HAS_TMUX=false
# Consumed by pocket_determine_mode via sourced script.
# shellcheck disable=SC2034
IS_ISH=false
pocket_determine_mode agent
[ "$MODE_EFFECTIVE" = "viewer" ]

echo "PASS runtime mode decisions"

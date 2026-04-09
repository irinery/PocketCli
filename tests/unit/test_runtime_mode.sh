#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
# shellcheck disable=SC2034 -- consumed by sourced runtime scripts below
POCKETCLI_DIR="$REPO_ROOT"
. "$REPO_ROOT/lib/common.sh"
. "$REPO_ROOT/scripts/runtime/capabilities.sh"
. "$REPO_ROOT/scripts/runtime/mode.sh"

# shellcheck disable=SC2034 -- consumed by pocket_determine_mode via sourced script
HAS_TTY=true
# shellcheck disable=SC2034 -- consumed by pocket_determine_mode via sourced script
HAS_TMUX=true
# shellcheck disable=SC2034 -- consumed by pocket_determine_mode via sourced script
IS_ISH=false
pocket_determine_mode auto
[ "$MODE_EFFECTIVE" = "agent" ]

# shellcheck disable=SC2034 -- consumed by pocket_determine_mode via sourced script
HAS_TTY=false
# shellcheck disable=SC2034 -- consumed by pocket_determine_mode via sourced script
HAS_TMUX=false
# shellcheck disable=SC2034 -- consumed by pocket_determine_mode via sourced script
IS_ISH=false
pocket_determine_mode auto
[ "$MODE_EFFECTIVE" = "viewer" ]

# shellcheck disable=SC2034 -- consumed by pocket_determine_mode via sourced script
HAS_TTY=true
# shellcheck disable=SC2034 -- consumed by pocket_determine_mode via sourced script
HAS_TMUX=false
# shellcheck disable=SC2034 -- consumed by pocket_determine_mode via sourced script
IS_ISH=false
pocket_determine_mode agent
[ "$MODE_EFFECTIVE" = "viewer" ]

echo "PASS runtime mode decisions"

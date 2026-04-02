#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
POCKETCLI_DIR="$REPO_ROOT"
. "$REPO_ROOT/lib/common.sh"
. "$REPO_ROOT/scripts/runtime/capabilities.sh"
. "$REPO_ROOT/scripts/runtime/mode.sh"

HAS_TTY=true
HAS_TMUX=true
IS_ISH=false
pocket_determine_mode auto
[ "$MODE_EFFECTIVE" = "agent" ]

HAS_TTY=false
HAS_TMUX=false
IS_ISH=false
pocket_determine_mode auto
[ "$MODE_EFFECTIVE" = "viewer" ]

HAS_TTY=true
HAS_TMUX=false
IS_ISH=false
pocket_determine_mode agent
[ "$MODE_EFFECTIVE" = "viewer" ]

echo "PASS runtime mode decisions"

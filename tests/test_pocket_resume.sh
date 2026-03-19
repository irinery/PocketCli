#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
HOME_DIR="$WORKDIR/home"
MOCKBIN="$WORKDIR/mockbin"
LOG_FILE="$WORKDIR/log"
mkdir -p "$HOME_DIR/.pocketcli/scripts/lib" "$MOCKBIN"
cp "$REPO_ROOT/pocket" "$HOME_DIR/.pocketcli/pocket"
cp "$REPO_ROOT/scripts/lib/common.sh" "$HOME_DIR/.pocketcli/scripts/lib/common.sh"
chmod +x "$HOME_DIR/.pocketcli/pocket"

cat > "$MOCKBIN/tmux" <<'EOS'
#!/usr/bin/env sh
set -eu
printf 'tmux:%s
' "$*" >> "$POCKETCLI_TEST_LOG"
case "$1" in
  has-session) exit 1 ;;
  new-session) exit 0 ;;
  source-file) exit 0 ;;
  attach-session) exit 0 ;;
  *) exit 0 ;;
esac
EOS
chmod +x "$MOCKBIN/tmux"

script -q -c "env HOME='$HOME_DIR' PATH='$MOCKBIN:/usr/bin:/bin' POCKETCLI_TEST_LOG='$LOG_FILE' sh '$HOME_DIR/.pocketcli/pocket'" /dev/null >/dev/null 2>&1 || true

grep -F 'tmux:new-session -d -s pocketcli' "$LOG_FILE" >/dev/null 2>&1
grep -F 'tmux:attach-session -t pocketcli' "$LOG_FILE" >/dev/null 2>&1
grep -F 'resume' "$HOME_DIR/.pocketcli/state/last-command" >/dev/null 2>&1 && exit 1 || true
grep -F 'menu' "$HOME_DIR/.pocketcli/state/last-command" >/dev/null 2>&1
printf 'PASS pocket resume creates tmux session and keeps last command state
'

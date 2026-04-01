#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
HOME_DIR="$WORKDIR/home"
MOCKBIN="$WORKDIR/mockbin"
LOG_FILE="$WORKDIR/tmux.log"
mkdir -p "$HOME_DIR/.pocketcli/scripts" "$MOCKBIN"

cp "$REPO_ROOT/scripts/start_agent.sh" "$HOME_DIR/.pocketcli/scripts/start_agent.sh"
chmod +x "$HOME_DIR/.pocketcli/scripts/start_agent.sh"

cat > "$MOCKBIN/tmux" <<'TMUXEOF'
#!/usr/bin/env sh
set -eu
printf '%s\n' "$*" >> "$POCKETCLI_TEST_TMUX_LOG"
case "$1" in
    has-session)
        exit 1
        ;;
    *)
        exit 0
        ;;
esac
TMUXEOF

cat > "$MOCKBIN/stty" <<'STTYEOF'
#!/usr/bin/env sh
# Simula ausência de TTY interativo para forçar fallback interno.
exit 1
STTYEOF

chmod +x "$MOCKBIN/tmux" "$MOCKBIN/stty"

env \
    HOME="$HOME_DIR" \
    PATH="$MOCKBIN:/usr/bin:/bin" \
    POCKETCLI_TEST_TMUX_LOG="$LOG_FILE" \
    sh "$HOME_DIR/.pocketcli/scripts/start_agent.sh" >/tmp/pocketcli-start-agent.out 2>/tmp/pocketcli-start-agent.err

grep -F 'new-session -d -s pocketcli -x 120 -y 30' "$LOG_FILE" >/dev/null 2>&1
grep -F 'split-window -h -t pocketcli' "$LOG_FILE" >/dev/null 2>&1
grep -F 'attach-session -t pocketcli' "$LOG_FILE" >/dev/null 2>&1

printf 'PASS start_agent launcher adapts tmux size with safe defaults when tty size is unavailable\n'

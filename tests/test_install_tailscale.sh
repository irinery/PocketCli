#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
HOME_DIR="$WORKDIR/home"
LOG_FILE="$WORKDIR/daemon.log"
mkdir -p "$HOME_DIR/.pocketcli/scripts"

cat > "$HOME_DIR/.pocketcli/scripts/tailscale_daemon.sh" <<'EOS'
#!/usr/bin/env sh
set -eu
printf 'tailscale-daemon:%s\n' "$*" >> "$POCKETCLI_TEST_DAEMON_LOG"
printf '[PocketCli] Tailscale mesh already operational (IP: 100.113.114.52).\n'
EOS
chmod +x "$HOME_DIR/.pocketcli/scripts/tailscale_daemon.sh"

OUTPUT=$(env HOME="$HOME_DIR" POCKETCLI_DIR="$HOME_DIR/.pocketcli" POCKETCLI_TEST_DAEMON_LOG="$LOG_FILE" sh "$REPO_ROOT/scripts/install_tailscale.sh" mac)

printf '%s\n' "$OUTPUT" | grep -F 'Tailscale mesh already operational' >/dev/null 2>&1
grep -Fx 'tailscale-daemon:setup' "$LOG_FILE" >/dev/null 2>&1

printf 'PASS install_tailscale delegates to the unified Tailscale setup flow\n'

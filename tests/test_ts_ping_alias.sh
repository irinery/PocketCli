#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
HOME_DIR="$WORKDIR/home"
mkdir -p "$HOME_DIR/.pocketcli/scripts/lib"

cp "$REPO_ROOT/pocket" "$HOME_DIR/.pocketcli/pocket"
cp "$REPO_ROOT/scripts/lib/common.sh" "$HOME_DIR/.pocketcli/scripts/lib/common.sh"
chmod +x "$HOME_DIR/.pocketcli/pocket"

cat > "$HOME_DIR/.pocketcli/scripts/tailscale_daemon.sh" <<'EOS'
#!/usr/bin/env sh
set -eu
printf '%s\n' "$*" >> "${HOME}/ts-dispatch.log"
EOS
chmod +x "$HOME_DIR/.pocketcli/scripts/tailscale_daemon.sh"

env HOME="$HOME_DIR" sh "$HOME_DIR/.pocketcli/pocket" ts-ping 100.64.0.1 >/tmp/pocket-ts-ping.out 2>/tmp/pocket-ts-ping.err
env HOME="$HOME_DIR" sh "$HOME_DIR/.pocketcli/pocket" tailscale-ping 100.64.0.2 >/tmp/pocket-tailscale-ping.out 2>/tmp/pocket-tailscale-ping.err

grep -Fx 'ping 100.64.0.1' "$HOME_DIR/ts-dispatch.log" >/dev/null 2>&1
grep -Fx 'ping 100.64.0.2' "$HOME_DIR/ts-dispatch.log" >/dev/null 2>&1

printf 'PASS ts-ping/tailscale-ping dispatch to tailscale daemon ping\n'

#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
HOME_DIR="$WORKDIR/home"
MOCKBIN="$WORKDIR/mockbin"
mkdir -p "$HOME_DIR/.pocketcli/lib" "$MOCKBIN"

cp "$REPO_ROOT/radar.sh" "$HOME_DIR/.pocketcli/radar.sh"
cp "$REPO_ROOT/lib/common.sh" "$HOME_DIR/.pocketcli/lib/common.sh"
chmod +x "$HOME_DIR/.pocketcli/radar.sh"
printf 'ipad-a\nipad-b\n' > "$HOME_DIR/.pocketcli/hosts"
printf '100.113.114.52\n' > "$HOME_DIR/.pocketcli/fallback_seeds"

cat > "$MOCKBIN/tailscale" <<'EOS'
#!/usr/bin/env sh
exit 1
EOS

cat > "$MOCKBIN/ping" <<'EOS'
#!/usr/bin/env sh
case "$*" in
  *ipad-a*|*100.113.114.52*) exit 0 ;;
  *) exit 1 ;;
esac
EOS

chmod +x "$MOCKBIN/tailscale" "$MOCKBIN/ping"

OUTPUT=$(env HOME="$HOME_DIR" PATH="$MOCKBIN:/usr/bin:/bin" sh "$HOME_DIR/.pocketcli/radar.sh")

printf '%s\n' "$OUTPUT" | grep -F 'saved+seed' >/dev/null 2>&1
printf '%s\n' "$OUTPUT" | grep -F 'ipad-a' >/dev/null 2>&1
printf '%s\n' "$OUTPUT" | grep -F 'reachable' >/dev/null 2>&1
printf '%s\n' "$OUTPUT" | grep -F 'ipad-b' >/dev/null 2>&1
printf '%s\n' "$OUTPUT" | grep -F '100.113.114.52' >/dev/null 2>&1
printf '%s\n' "$OUTPUT" | grep -F 'seed-ok' >/dev/null 2>&1

printf 'PASS radar falls back to saved hosts when tailscale status is unavailable\n'

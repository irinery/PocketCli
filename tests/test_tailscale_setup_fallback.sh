#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
HOME_DIR="$WORKDIR/home"
MOCKBIN="$WORKDIR/mockbin"
mkdir -p "$HOME_DIR/.pocketcli/lib" "$MOCKBIN"

cp "$REPO_ROOT/scripts/tailscale_daemon.sh" "$HOME_DIR/.pocketcli/tailscale_daemon.sh"
cp "$REPO_ROOT/lib/common.sh" "$HOME_DIR/.pocketcli/lib/common.sh"
chmod +x "$HOME_DIR/.pocketcli/tailscale_daemon.sh"

cat > "$MOCKBIN/apk" <<'EOS'
#!/usr/bin/env sh
exit 1
EOS

cat > "$MOCKBIN/ping" <<'EOS'
#!/usr/bin/env sh
case "$*" in
  *100.113.114.52*) exit 0 ;;
  *) exit 1 ;;
esac
EOS

cat > "$MOCKBIN/ssh" <<'EOS'
#!/usr/bin/env sh
exit 1
EOS

chmod +x "$MOCKBIN/apk" "$MOCKBIN/ping" "$MOCKBIN/ssh"

OUTPUT=$(env \
  HOME="$HOME_DIR" \
  PATH="$MOCKBIN:/usr/bin:/bin" \
  POCKETCLI_TAILSCALE_FALLBACK_TARGETS="100.113.114.52" \
  sh "$HOME_DIR/.pocketcli/tailscale_daemon.sh" setup)

printf '%s\n' "$OUTPUT" | grep -F 'continuing with connectivity fallback' >/dev/null 2>&1
printf '%s\n' "$OUTPUT" | grep -F '100.113.114.52 is reachable' >/dev/null 2>&1
printf '%s\n' "$OUTPUT" | grep -F "pocket scan" >/dev/null 2>&1

grep -Fx '100.113.114.52' "$HOME_DIR/.pocketcli/hosts" >/dev/null 2>&1
grep -Fx '100.113.114.52' "$HOME_DIR/.pocketcli/fallback_seeds" >/dev/null 2>&1

printf 'PASS tailscale setup falls back to a saved seed IP when install fails\n'

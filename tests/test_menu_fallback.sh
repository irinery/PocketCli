#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
HOME_DIR="$WORKDIR/home"
MOCKBIN="$WORKDIR/mockbin"
mkdir -p "$HOME_DIR/.pocketcli/scripts" "$HOME_DIR/.pocketcli/lib" "$MOCKBIN"

cp "$REPO_ROOT/scripts/pocketcli_menu.sh" "$HOME_DIR/.pocketcli/scripts/pocketcli_menu.sh"
cp "$REPO_ROOT/lib/common.sh" "$HOME_DIR/.pocketcli/lib/common.sh"
chmod +x "$HOME_DIR/.pocketcli/scripts/pocketcli_menu.sh"
printf 'ipad-a\nipad-b\n' > "$HOME_DIR/.pocketcli/hosts"

cat > "$MOCKBIN/clear" <<'EOS'
#!/usr/bin/env sh
exit 1
EOS

cat > "$MOCKBIN/tailscale" <<'EOS'
#!/usr/bin/env sh
exit 1
EOS

cat > "$MOCKBIN/jq" <<'EOS'
#!/usr/bin/env sh
cat >/dev/null
exit 0
EOS

chmod +x "$MOCKBIN/clear" "$MOCKBIN/tailscale" "$MOCKBIN/jq"

OUTPUT_FILE="$WORKDIR/menu.out"

timeout 2 script -q -c "env HOME='$HOME_DIR' PATH='$MOCKBIN:/usr/bin:/bin' TERM='dumb' sh '$HOME_DIR/.pocketcli/scripts/pocketcli_menu.sh'" "$OUTPUT_FILE" >/dev/null 2>&1 || true

grep -F 'peer online 2/2 visíveis' "$OUTPUT_FILE" >/dev/null 2>&1
grep -F 'host foco   ipad-a' "$OUTPUT_FILE" >/dev/null 2>&1
grep -F 'PocketCli Control Deck' "$OUTPUT_FILE" >/dev/null 2>&1

printf 'PASS pocketcli_menu falls back to saved hosts when tailscale status is unavailable\n'

#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
. "$REPO_ROOT/tests/lib/test_helpers.sh"
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

cat > "$MOCKBIN/stty" <<'EOS'
#!/usr/bin/env sh
if [ "${1:-}" = "size" ]; then
    printf '24 80\n'
    exit 0
fi
exit 0
EOS

chmod +x "$MOCKBIN/clear" "$MOCKBIN/tailscale" "$MOCKBIN/jq" "$MOCKBIN/stty"

OUTPUT_FILE="$WORKDIR/menu.out"

env HOME="$HOME_DIR" PATH="$MOCKBIN:/usr/bin:/bin" TERM="dumb" POCKETCLI_MENU_RENDER_ONCE="1" sh "$HOME_DIR/.pocketcli/scripts/pocketcli_menu.sh" >"$OUTPUT_FILE" 2>/dev/null || true

grep -F 'peer online 2/2 visíveis' "$OUTPUT_FILE" >/dev/null 2>&1
grep -F 'host foco   ipad-a' "$OUTPUT_FILE" >/dev/null 2>&1
grep -F 'PocketCli Control Deck' "$OUTPUT_FILE" >/dev/null 2>&1

cat > "$MOCKBIN/stty" <<'EOS'
#!/usr/bin/env sh
if [ "${1:-}" = "size" ]; then
    printf '24 58\n'
    exit 0
fi
exit 0
EOS
chmod +x "$MOCKBIN/stty"

NARROW_OUTPUT_FILE="$WORKDIR/menu-narrow.out"
env HOME="$HOME_DIR" PATH="$MOCKBIN:/usr/bin:/bin" TERM="dumb" POCKETCLI_MENU_RENDER_ONCE="1" sh "$HOME_DIR/.pocketcli/scripts/pocketcli_menu.sh" >"$NARROW_OUTPUT_FILE" 2>/dev/null || true

grep -F 'PocketCli SSH/tmux' "$NARROW_OUTPUT_FILE" >/dev/null 2>&1
grep -F 'peer online 2/2 visíveis' "$NARROW_OUTPUT_FILE" >/dev/null 2>&1

printf 'PASS pocketcli_menu falls back to saved hosts when tailscale status is unavailable\n'

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

cat > "$MOCKBIN/ping" <<'EOS'
#!/usr/bin/env sh
exit 1
EOS

chmod +x "$MOCKBIN/clear" "$MOCKBIN/tailscale" "$MOCKBIN/jq" "$MOCKBIN/ping"

OUTPUT_FILE="$WORKDIR/menu.out"

env HOME="$HOME_DIR" PATH="$MOCKBIN:/usr/bin:/bin" TERM="dumb" POCKETCLI_MENU_RENDER_ONCE="1" sh "$HOME_DIR/.pocketcli/scripts/pocketcli_menu.sh" >"$OUTPUT_FILE" 2>/dev/null || true

grep -F 'sem host salvo' "$OUTPUT_FILE" >/dev/null 2>&1
grep -F 'PocketCli Control Deck' "$OUTPUT_FILE" >/dev/null 2>&1

printf 'PASS pocketcli_menu stays alive without tailscale peers or saved hosts\n'

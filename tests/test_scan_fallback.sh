#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
HOME_DIR="$WORKDIR/home"
MOCKBIN="$WORKDIR/mockbin"
mkdir -p "$HOME_DIR/.pocketcli/discovery" "$HOME_DIR/.pocketcli/lib" "$MOCKBIN"

cp "$REPO_ROOT/discovery/pocketcli-scan.sh" "$HOME_DIR/.pocketcli/discovery/pocketcli-scan.sh"
cp "$REPO_ROOT/lib/common.sh" "$HOME_DIR/.pocketcli/lib/common.sh"
chmod +x "$HOME_DIR/.pocketcli/discovery/pocketcli-scan.sh"
printf 'ipad-a\nipad-b\n' > "$HOME_DIR/.pocketcli/hosts"

cat > "$MOCKBIN/tailscale" <<'EOS'
#!/usr/bin/env sh
exit 1
EOS

cat > "$MOCKBIN/jq" <<'EOS'
#!/usr/bin/env sh
cat >/dev/null
exit 0
EOS

cat > "$MOCKBIN/ssh" <<'EOS'
#!/usr/bin/env sh
case "$*" in
  *ipad-a*command\ -v\ docker*) exit 0 ;;
  *ipad-a*docker\ ps*) printf 'nginx\napi\n'; exit 0 ;;
  *ipad-b*command\ -v\ docker*) exit 1 ;;
  *) exit 1 ;;
esac
EOS

chmod +x "$MOCKBIN/tailscale" "$MOCKBIN/jq" "$MOCKBIN/ssh"

OUTPUT=$(env HOME="$HOME_DIR" PATH="$MOCKBIN:/usr/bin:/bin" sh "$HOME_DIR/.pocketcli/discovery/pocketcli-scan.sh")

printf '%s\n' "$OUTPUT" | grep -F 'saved host(s)' >/dev/null 2>&1
printf '%s\n' "$OUTPUT" | grep -F 'ipad-a' >/dev/null 2>&1
printf '%s\n' "$OUTPUT" | grep -F 'nginx' >/dev/null 2>&1
printf '%s\n' "$OUTPUT" | grep -F 'ipad-b' >/dev/null 2>&1

printf 'PASS scan falls back to saved hosts when tailscale discovery is unavailable\n'

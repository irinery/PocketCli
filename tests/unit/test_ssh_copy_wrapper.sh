#!/usr/bin/env sh
set -eu

WORKDIR=$(mktemp -d)
HOME_DIR="$WORKDIR/home"
MOCKBIN="$WORKDIR/mockbin"
mkdir -p "$HOME_DIR/.pocketcli/scripts/ssh" "$MOCKBIN"

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/../.." && pwd)
cp "$REPO_ROOT/scripts/ssh/copy.sh" "$HOME_DIR/.pocketcli/scripts/ssh/copy.sh"
chmod +x "$HOME_DIR/.pocketcli/scripts/ssh/copy.sh"

cat > "$HOME_DIR/.pocketcli/scripts/ssh/open.sh" <<'EOS'
#!/usr/bin/env sh
printf 'OPEN:%s\n' "$*"
EOS
chmod +x "$HOME_DIR/.pocketcli/scripts/ssh/open.sh"

cat > "$MOCKBIN/scp" <<'EOS'
#!/usr/bin/env sh
printf 'SCP:%s\n' "$*"
EOS
chmod +x "$MOCKBIN/scp"

OUT1=$(env HOME="$HOME_DIR" POCKETCLI_DIR="$HOME_DIR/.pocketcli" PATH="$MOCKBIN:/usr/bin:/bin" sh "$HOME_DIR/.pocketcli/scripts/ssh/copy.sh" node1:/tmp/a ./b)
printf '%s' "$OUT1" | grep -F 'OPEN:--run copy node1 node1:/tmp/a ./b' >/dev/null

OUT2=$(env HOME="$HOME_DIR" POCKETCLI_DIR="$HOME_DIR/.pocketcli" PATH="$MOCKBIN:/usr/bin:/bin" sh "$HOME_DIR/.pocketcli/scripts/ssh/copy.sh" ./a ./b)
printf '%s' "$OUT2" | grep -F 'SCP:-r ./a ./b' >/dev/null

if env HOME="$HOME_DIR" POCKETCLI_DIR="$HOME_DIR/.pocketcli" PATH="$MOCKBIN:/usr/bin:/bin" sh "$HOME_DIR/.pocketcli/scripts/ssh/copy.sh" -oProxyCommand=bad ./b >"$WORKDIR/bad.out" 2>"$WORKDIR/bad.err"; then
    printf 'FAIL expected scp option-like source to be rejected\n' >&2
    exit 1
fi
grep -F 'Invalid source path' "$WORKDIR/bad.err" >/dev/null

echo "PASS ssh copy wrapper routes remote copy through policy and rejects option injection"

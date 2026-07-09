#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
MOCKBIN="$WORKDIR/mockbin"
LOG_FILE="$WORKDIR/sudo.log"
mkdir -p "$MOCKBIN"

cat > "$MOCKBIN/tailscale" <<'EOS'
#!/usr/bin/env sh
if [ "${1:-}" = "ip" ] && [ "${2:-}" = "-4" ]; then
    printf '100.113.114.52\n'
    exit 0
fi
exit 1
EOS

cat > "$MOCKBIN/sudo" <<'EOS'
#!/usr/bin/env sh
printf 'sudo-called:%s\n' "$*" >> "$POCKETCLI_TEST_SUDO_LOG"
exit 1
EOS

chmod +x "$MOCKBIN/tailscale" "$MOCKBIN/sudo"

OUTPUT=$(env PATH="$MOCKBIN:/usr/bin:/bin" POCKETCLI_TEST_SUDO_LOG="$LOG_FILE" sh "$REPO_ROOT/scripts/install_tailscale.sh" mac)

printf '%s\n' "$OUTPUT" | grep -F 'Tailscale already installed' >/dev/null 2>&1
printf '%s\n' "$OUTPUT" | grep -F 'already authenticated' >/dev/null 2>&1
[ ! -f "$LOG_FILE" ]

printf 'PASS install_tailscale skips auth when Tailscale already has an IP\n'

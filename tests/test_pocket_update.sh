#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
WORKDIR=$(mktemp -d)
HOME_DIR="$WORKDIR/home"
MOCKBIN="$WORKDIR/mockbin"
LOG_FILE="$WORKDIR/git.log"
STASH_FILE="$WORKDIR/stash-state"
mkdir -p "$HOME_DIR/.pocketcli/scripts/lib" "$MOCKBIN"

cp "$REPO_ROOT/pocket" "$HOME_DIR/.pocketcli/pocket"
cp "$REPO_ROOT/scripts/lib/common.sh" "$HOME_DIR/.pocketcli/scripts/lib/common.sh"
chmod +x "$HOME_DIR/.pocketcli/pocket"

cat > "$MOCKBIN/git" <<'EOS'
#!/usr/bin/env sh
set -eu
printf '%s\n' "$*" >> "$POCKETCLI_TEST_GIT_LOG"
if [ "$1" = "-C" ]; then
    shift 2
fi
case "$1" in
    rev-parse)
        if [ -f "$POCKETCLI_TEST_STASH_FILE" ]; then
            printf 'stash@{0}\n'
        fi
        exit 0
        ;;
    status)
        printf ' M profile/zshrc\n'
        exit 0
        ;;
    stash)
        case "${2:-}" in
            push)
                : > "$POCKETCLI_TEST_STASH_FILE"
                printf 'saved\n'
                ;;
            pop)
                rm -f "$POCKETCLI_TEST_STASH_FILE"
                printf 'restored\n'
                ;;
        esac
        exit 0
        ;;
    fetch|pull)
        exit 0
        ;;
    describe)
        printf 'dev\n'
        exit 0
        ;;
    *)
        exit 0
        ;;
esac
EOS
chmod +x "$MOCKBIN/git"

env HOME="$HOME_DIR" PATH="$MOCKBIN:/usr/bin:/bin" POCKETCLI_TEST_GIT_LOG="$LOG_FILE" POCKETCLI_TEST_STASH_FILE="$STASH_FILE" sh "$HOME_DIR/.pocketcli/pocket" -e update >/tmp/pocketcli-update.out 2>/tmp/pocketcli-update.err

grep -F 'status --porcelain' "$LOG_FILE" >/dev/null 2>&1
grep -F 'stash push --include-untracked --quiet -m pocketcli-update' "$LOG_FILE" >/dev/null 2>&1
grep -F 'fetch --quiet origin' "$LOG_FILE" >/dev/null 2>&1
grep -F 'pull --ff-only' "$LOG_FILE" >/dev/null 2>&1
grep -F 'stash pop --quiet' "$LOG_FILE" >/dev/null 2>&1
printf 'PASS pocket update salva mudanças locais, atualiza e restaura o stash\n'
grep -F 'PocketCli invocation' /tmp/pocketcli-update.out >/dev/null 2>&1
grep -F 'Checking local modifications' /tmp/pocketcli-update.out >/dev/null 2>&1
grep -F 'worktree_status:' /tmp/pocketcli-update.out >/dev/null 2>&1

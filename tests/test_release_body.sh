#!/usr/bin/env sh
# shellcheck disable=SC2016
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
SCRIPT="$REPO_ROOT/scripts/release_body.sh"

assert_contains() {
    HAYSTACK="$1"
    NEEDLE="$2"

    case "$HAYSTACK" in
        *"$NEEDLE"*) ;;
        *)
            printf 'FAIL expected to find %s in output\n' "$NEEDLE" >&2
            exit 1
            ;;
    esac
}

CUSTOM_FILE=$(mktemp)
trap 'rm -f "$CUSTOM_FILE"' EXIT INT TERM
cat >"$CUSTOM_FILE" <<'EOF'
### Destaques

- release customizada
EOF

DEFAULT_OUTPUT=$(sh "$SCRIPT" "v1.2.0")
assert_contains "$DEFAULT_OUTPUT" "## PocketCli v1.2.0"
assert_contains "$DEFAULT_OUTPUT" "curl -fsSL https://raw.githubusercontent.com/irinery/PocketCli/v1.2.0/bootstrap.sh -o bootstrap.sh"
assert_contains "$DEFAULT_OUTPUT" "sh bootstrap.sh"

CUSTOM_OUTPUT=$(sh "$SCRIPT" "v1.2.0" "$CUSTOM_FILE")
assert_contains "$CUSTOM_OUTPUT" "### Destaques"
assert_contains "$CUSTOM_OUTPUT" "release customizada"
assert_contains "$CUSTOM_OUTPUT" "checksums.sha256"

TRACE_OUTPUT=$(RELEASE_DISPLAY_TAG="v1.2.0" RELEASE_COMMIT_SHA="abc123def456" sh "$SCRIPT" "release/v1.2.0")
assert_contains "$TRACE_OUTPUT" "## PocketCli v1.2.0"
assert_contains "$TRACE_OUTPUT" 'Commit: `abc123def456`'
assert_contains "$TRACE_OUTPUT" "https://raw.githubusercontent.com/irinery/PocketCli/release/v1.2.0/bootstrap.sh"

printf 'PASS release_body gera corpo padrao e aceita descricao customizada por arquivo\n'

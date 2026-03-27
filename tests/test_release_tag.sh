#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
SCRIPT="$REPO_ROOT/scripts/next_release_tag.sh"

assert_next_tag() {
    EXPECTED="$1"
    shift

    ACTUAL=$(sh "$SCRIPT" "$@")
    if [ "$ACTUAL" != "$EXPECTED" ]; then
        printf 'FAIL expected %s, got %s\n' "$EXPECTED" "$ACTUAL" >&2
        exit 1
    fi
}

assert_next_tag_in_repo() {
    EXPECTED="$1"
    REPO_DIR="$2"

    ACTUAL=$(CDPATH='' cd -- "$REPO_DIR" && sh "$SCRIPT")
    if [ "$ACTUAL" != "$EXPECTED" ]; then
        printf 'FAIL expected %s in %s, got %s\n' "$EXPECTED" "$REPO_DIR" "$ACTUAL" >&2
        exit 1
    fi
}

assert_next_tag "release/v1.1.6" v1.1.4 v1.1.5 v1.0.9
assert_next_tag "release/v1.1.7" release/v1.1.6 release/v1.1.5
assert_next_tag "release/v1.2.1" v1.1.9 release/v1.2.0 v1.0.8

EMPTY_REPO=$(mktemp -d)
trap 'rm -rf "$EMPTY_REPO"' EXIT INT TERM
git -C "$EMPTY_REPO" init >/dev/null 2>&1
assert_next_tag_in_repo "release/v0.0.1" "$EMPTY_REPO"

printf 'PASS next_release_tag calcula a proxima tag para legadas, novas, mistas e sem tags\n'

#!/usr/bin/env sh

set -eu

print_candidates() {
    if [ "$#" -gt 0 ]; then
        for tag in "$@"; do
            printf '%s\n' "$tag"
        done
        return
    fi

    git tag --list
}

next_release_tag() {
    awk '
        BEGIN {
            best_major = -1
            best_minor = -1
            best_patch = -1
        }

        {
            tag = $0
            sub(/^release\//, "", tag)
            sub(/^v/, "", tag)

            if (tag !~ /^[0-9]+\.[0-9]+\.[0-9]+$/) {
                next
            }

            split(tag, parts, ".")
            major = parts[1] + 0
            minor = parts[2] + 0
            patch = parts[3] + 0

            if (major > best_major ||
                (major == best_major && minor > best_minor) ||
                (major == best_major && minor == best_minor && patch > best_patch)) {
                best_major = major
                best_minor = minor
                best_patch = patch
            }
        }

        END {
            if (best_major < 0) {
                best_major = 0
                best_minor = 0
                best_patch = 0
            }

            best_patch += 1
            printf "release/v%d.%d.%d\n", best_major, best_minor, best_patch
        }
    '
}

print_candidates "$@" | next_release_tag

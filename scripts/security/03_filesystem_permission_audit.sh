#!/usr/bin/env bash
set -u

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/security/lib.sh
. "$SCRIPT_DIR/lib.sh"

security_init_repo "$SCRIPT_DIR/../.."

SCAN_TIMEOUT=60
STARTED=$SECONDS
FOUND=0
BLOCKING=0
MODE=${1:-repo}

emit_finding() {
    local file mode rule severity description display
    file=$1
    mode=$2
    rule=$3
    severity=$4
    description=$5

    if [[ $file == "$REPO_ROOT"/* ]]; then
        display=$(security_relpath "$file")
    else
        display=$file
    fi

    printf '%s:%s:%s:%s:%s\n' "$display" "$mode" "$rule" "$severity" "$description"
    FOUND=1
    case "$severity" in
        HIGH|CRITICAL) BLOCKING=1 ;;
    esac
}

is_shell_script_file() {
    local file
    file=$1
    security_is_shell_scope "$file"
}

is_sensitive_name() {
    local path base lower
    path=$1
    if security_is_shell_scope "$path"; then
        return 1
    fi
    base=$(basename -- "$path")
    lower=$(printf '%s' "$base" | tr '[:upper:]' '[:lower:]')

    case "$lower" in
        *.env|*.key|*secret*|*credential*|*token*) return 0 ;;
        *) return 1 ;;
    esac
}

others_digit() {
    local mode
    mode=$1
    printf '%s\n' "${mode: -1}"
}

has_other_read() {
    local other
    other=$(others_digit "$1")
    [ $((other & 4)) -ne 0 ]
}

has_other_write() {
    local other
    other=$(others_digit "$1")
    [ $((other & 2)) -ne 0 ]
}

has_other_execute() {
    local other
    other=$(others_digit "$1")
    [ $((other & 1)) -ne 0 ]
}

has_any_execute() {
    local digits
    digits=${1#0}
    [ $((digits % 2)) -eq 1 ] && return 0
    [ $((((digits / 10) % 10) & 1)) -ne 0 ] && return 0
    [ $((((digits / 100) % 10) & 1)) -ne 0 ]
}

scan_repo_file() {
    local file rel mode
    file=$1

    if ! mode=$(security_file_mode "$file"); then
        security_err "WARN: cannot stat file mode: $(security_relpath "$file")"
        return 0
    fi
    rel=$(security_relpath "$file")

    if has_other_write "$mode"; then
        emit_finding "$file" "$mode" "WORLD_WRITABLE" "HIGH" "File should not be world-writable"
    fi

    if is_sensitive_name "$file" && has_other_read "$mode"; then
        emit_finding "$file" "$mode" "SENSITIVE_FILE_WORLD_READABLE" "HIGH" "Sensitive file should not be world-readable"
    fi

    case "$rel" in
        *.log)
            if has_other_read "$mode"; then
                emit_finding "$file" "$mode" "LOG_FILE_WORLD_READABLE" "MEDIUM" "Log file should not be world-readable"
            fi
            ;;
    esac

    case "$rel" in
        *.sh|*.bash)
            if has_other_execute "$mode"; then
                emit_finding "$file" "$mode" "SCRIPT_WORLD_EXECUTABLE" "MEDIUM" "Script should not be world-executable (expected 0750)"
            fi
            return 0
            ;;
    esac

    if has_any_execute "$mode" && ! is_shell_script_file "$file"; then
        emit_finding "$file" "$mode" "NON_SHELL_EXECUTABLE" "LOW" "Non-shell file has executable bit"
    fi
}

scan_repo() {
    local file
    while IFS= read -r -d '' file; do
        scan_repo_file "$file"
        security_check_timeout "$STARTED" "$SCAN_TIMEOUT"
    done < <(find "$REPO_ROOT" \( -name .git -o -name node_modules \) -type d -prune -o -type f -maxdepth 10 -print0)
}

mode_is_more_open_than_600() {
    local mode digits group other
    mode=$1
    digits=${mode#0}
    group=$(((digits / 10) % 10))
    other=$((digits % 10))
    [ "$group" -ne 0 ] || [ "$other" -ne 0 ]
}

scan_ssh_file() {
    local file mode base
    file=$1
    [ -e "$file" ] || return 0
    [ -f "$file" ] || return 0

    if ! mode=$(security_file_mode "$file"); then
        security_err "WARN: cannot stat file mode: $file"
        return 0
    fi
    base=$(basename -- "$file")

    case "$base" in
        *.pub)
            return 0
            ;;
        config|id_*)
            if mode_is_more_open_than_600 "$mode"; then
                emit_finding "$file" "$mode" "SSH_CONFIG_TOO_OPEN" "HIGH" "SSH config or key should be 0600"
            fi
            ;;
    esac
}

scan_local() {
    local home pocket_dir ssh_dir file mode
    home=${HOME:-}
    [ -n "$home" ] || return 0

    pocket_dir=$home/.pocketcli
    if [ -d "$pocket_dir" ]; then
        if mode=$(security_file_mode "$pocket_dir"); then
            if has_other_read "$mode" || has_other_execute "$mode"; then
                emit_finding "$pocket_dir" "$mode" "POCKETCLI_DIR_TOO_OPEN" "HIGH" "PocketCLI runtime directory should be 0700"
            fi
        else
            security_err "WARN: cannot stat file mode: $pocket_dir"
        fi
    fi

    if [ -d "$pocket_dir/logs" ]; then
        while IFS= read -r -d '' file; do
            if mode=$(security_file_mode "$file"); then
                if has_other_read "$mode"; then
                    emit_finding "$file" "$mode" "LOG_FILE_WORLD_READABLE" "MEDIUM" "Log file should not be world-readable"
                fi
            else
                security_err "WARN: cannot stat file mode: $file"
            fi
        done < <(find "$pocket_dir/logs" -maxdepth 10 -type f -name '*.log' -print0)
    fi

    ssh_dir=$home/.ssh
    [ -d "$ssh_dir" ] || return 0
    scan_ssh_file "$ssh_dir/config"
    while IFS= read -r -d '' file; do
        scan_ssh_file "$file"
    done < <(find "$ssh_dir" -maxdepth 1 -type f -name 'id_*' -print0)
}

case "$MODE" in
    repo)
        scan_repo
        ;;
    local)
        scan_local
        ;;
    all)
        scan_repo
        scan_local
        ;;
    *)
        security_err "Usage: $0 [repo|local|all]"
        exit 2
        ;;
esac

if [ "$FOUND" -eq 0 ]; then
    printf 'No permission issues found.\n'
fi

if [ "$BLOCKING" -eq 1 ]; then
    exit 1
fi
exit 0

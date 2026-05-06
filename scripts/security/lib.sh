#!/usr/bin/env bash

security_err() {
    printf '%s\n' "$*" >&2
}

security_init_repo() {
    local default_root
    default_root=$1

    if [ -z "${REPO_ROOT:-}" ]; then
        if ! REPO_ROOT=$(CDPATH='' cd -- "$default_root" && pwd); then
            security_err "ERROR: REPO_ROOT not found: $default_root"
            exit 2
        fi
    fi

    if [ ! -d "$REPO_ROOT" ]; then
        security_err "ERROR: REPO_ROOT not found: $REPO_ROOT"
        exit 2
    fi
}

security_relpath() {
    local path
    path=$1
    case "$path" in
        "$REPO_ROOT"/*) printf '%s\n' "${path#"$REPO_ROOT"/}" ;;
        *) printf '%s\n' "$path" ;;
    esac
}

security_file_size() {
    local file
    file=$1

    if stat -c %s "$file" 2>/dev/null; then
        return 0
    fi
    stat -f %z "$file" 2>/dev/null
}

security_file_mode() {
    local file mode
    file=$1

    if mode=$(stat -c %a "$file" 2>/dev/null); then
        printf '0%s\n' "$mode"
        return 0
    fi
    if mode=$(stat -f %Lp "$file" 2>/dev/null); then
        printf '0%s\n' "$mode"
        return 0
    fi
    return 1
}

security_find_repo_files() {
    find "$REPO_ROOT" \( -name .git -o -name node_modules \) -type d -prune -o -type f -print0
}

security_check_timeout() {
    local started limit
    started=$1
    limit=$2

    if [ "$((SECONDS - started))" -gt "$limit" ]; then
        security_err "TIMEOUT: scanner exceeded ${limit}s"
        exit 2
    fi
}

security_warn_large_file() {
    local file size max_size
    file=$1
    size=$2
    max_size=$3

    security_err "WARN: skipping large file $(security_relpath "$file") (${size} bytes > ${max_size})"
}

security_should_skip_large_file() {
    local file max_size size
    file=$1
    max_size=$2

    if ! size=$(security_file_size "$file"); then
        security_err "WARN: cannot stat file size: $(security_relpath "$file")"
        return 0
    fi

    if [ "$size" -gt "$max_size" ]; then
        security_warn_large_file "$file" "$size" "$max_size"
        return 0
    fi

    return 1
}

security_is_standard_scope() {
    local rel
    rel=$1

    case "$rel" in
        *.md|*.png|*.jpg) return 1 ;;
        *.sh|*.bash|*.env.example|*.yml|*.yaml|Makefile|*/Makefile) return 0 ;;
        *) return 1 ;;
    esac
}

security_has_shell_shebang() {
    local file first
    file=$1
    first=''
    IFS= read -r first < "$file" || true

    case "$first" in
        '#!'*'/sh'|'#!'*'/bash'|'#!'*'env sh'|'#!'*'env bash') return 0 ;;
        *) return 1 ;;
    esac
}

security_is_shell_scope() {
    local file rel
    file=$1
    rel=$(security_relpath "$file")

    case "$rel" in
        *.sh|*.bash) return 0 ;;
    esac

    security_has_shell_shebang "$file"
}

security_is_hardening_scope() {
    local file rel lower
    file=$1
    rel=$(security_relpath "$file")
    lower=$(printf '%s' "$rel" | tr '[:upper:]' '[:lower:]')

    case "$rel" in
        *.sh|*.bash|*.env.example|*.yml|*.yaml) return 0 ;;
    esac
    case "$lower" in
        *ssh*config*) return 0 ;;
    esac

    security_has_shell_shebang "$file"
}

security_assignment_value() {
    local line value
    line=$1

    if [[ ! $line =~ ^[[:space:]]*(export[[:space:]]+)?[A-Za-z_][A-Za-z0-9_-]*[[:space:]]*= ]]; then
        return 1
    fi
    value=${line#*=}

    value=${value%%#*}
    value=${value#"${value%%[![:space:]]*}"}
    value=${value%"${value##*[![:space:]]}"}
    case "$value" in
        \"*\") value=${value#\"}; value=${value%\"} ;;
        \'*\') value=${value#\'}; value=${value%\'} ;;
    esac
    printf '%s\n' "$value"
}

security_is_placeholder_value() {
    local value
    value=$1

    case "$value" in
        PLACEHOLDER|YOUR_KEY_HERE|'<your-token>'|example|EXAMPLE|TODO|CHANGEME|xxxxxxxx)
            return 0
            ;;
        *)
            return 1
            ;;
    esac
}

security_line_is_comment_or_empty() {
    local line trimmed
    line=$1
    trimmed=${line#"${line%%[![:space:]]*}"}

    case "$trimmed" in
        ''|'#'*) return 0 ;;
        *) return 1 ;;
    esac
}

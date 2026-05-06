#!/usr/bin/env bash
# shellcheck disable=SC2016,SC2094
set -u
shopt -s nocasematch

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=scripts/security/lib.sh
. "$SCRIPT_DIR/lib.sh"

security_init_repo "$SCRIPT_DIR/../.."

MAX_SIZE=$((5 * 1024 * 1024))
SCAN_TIMEOUT=60
STARTED=$SECONDS
FOUND=0
BLOCKING=0

emit_finding() {
    local file line_no rule severity description rel
    file=$1
    line_no=$2
    rule=$3
    severity=$4
    description=$5
    rel=$(security_relpath "$file")

    printf '%s:%s:%s:%s:%s\n' "$rel" "$line_no" "$rule" "$severity" "$description"
    FOUND=1
    case "$severity" in
        HIGH|CRITICAL) BLOCKING=1 ;;
    esac
}

file_has_set_e() {
    local file line
    file=$1

    while IFS= read -r line || [ -n "$line" ]; do
        security_line_is_comment_or_empty "$line" && continue
        if [[ $line =~ (^|[[:space:];])set[[:space:]]+-[^[:space:]]*e ]]; then
            return 0
        fi
    done < "$file"
    return 1
}

file_has_ifs_restore() {
    local file line
    file=$1

    while IFS= read -r line || [ -n "$line" ]; do
        case "$line" in
            *'OLD_IFS='*|*'ORIGINAL_IFS='*|*'IFS=$OLD_IFS'*|*'IFS="${OLD_IFS}"'*|*'IFS=$ORIGINAL_IFS'*|*'IFS="${ORIGINAL_IFS}"'*)
                return 0
                ;;
        esac
    done < "$file"
    return 1
}

has_previous_positional_validation() {
    local file var max_line line_no line
    file=$1
    var=$2
    max_line=$3
    line_no=0

    while IFS= read -r line || [ -n "$line" ]; do
        line_no=$((line_no + 1))
        [ "$line_no" -ge "$max_line" ] && break
        case "$line" in
            *"[ -"*"$var"*|*"case \"${var}\""*|*"case ${var}"*|*"${var}:?"*)
                return 0
                ;;
        esac
    done < "$file"
    return 1
}

scan_line() {
    local file line_no line has_set_e has_ifs_restore pipe_re eval_var_re eval_lit_re danger_re network_re
    local path_re source_re seteu_sub_re seteu_cmd_re command_sub_re ifs_re pos_re matched_pos
    file=$1
    line_no=$2
    line=${3:0:4096}
    has_set_e=$4
    has_ifs_restore=$5

    security_line_is_comment_or_empty "$line" && return 0

    pipe_re='(^|[[:space:];])(curl|wget)[^|]*\|[[:space:]]*(bash|sh|zsh)([[:space:]]|$)'
    if [[ $line =~ $pipe_re ]]; then
        emit_finding "$file" "$line_no" "PIPE_TO_SHELL" "CRITICAL" "Network download piped directly to shell"
    fi

    eval_var_re='(^|[[:space:];])eval[[:space:]]+["'\'']?\$'
    eval_lit_re='(^|[[:space:];])eval[[:space:]]+["'\''][^$]*["'\'']'
    if [[ $line =~ $eval_var_re ]]; then
        emit_finding "$file" "$line_no" "EVAL_WITH_VARIABLE" "HIGH" "eval receives variable input"
    elif [[ $line =~ $eval_lit_re ]]; then
        emit_finding "$file" "$line_no" "EVAL_LITERAL" "MEDIUM" "eval with literal string"
    fi

    danger_re='(^|[[:space:];])(rm|chmod|chown|mv|cp)[[:space:]][^#"'\'']*\$[A-Za-z_][A-Za-z0-9_]*'
    if [[ $line =~ $danger_re ]]; then
        emit_finding "$file" "$line_no" "UNQUOTED_VAR_DANGEROUS_CMD" "HIGH" "Dangerous filesystem command uses unquoted variable"
    fi

    network_re='(^|[[:space:];])(ssh|scp|rsync|curl|wget)[[:space:]][^#"'\'']*\$[A-Za-z_][A-Za-z0-9_]*'
    if [[ $line =~ $network_re ]]; then
        emit_finding "$file" "$line_no" "UNQUOTED_VAR_NETWORK_CMD" "HIGH" "Network command uses unquoted variable"
    fi

    path_re='(^|[[:space:];])(cp|mv|cat|ln)[[:space:]]+\$[0-9]'
    if [[ $line =~ $path_re ]]; then
        emit_finding "$file" "$line_no" "PATH_TRAVERSAL_RISK" "HIGH" "Positional path used without quoting"
    fi

    source_re='(^|[[:space:];])(source|\.)[[:space:]]+\$[A-Za-z_][A-Za-z0-9_]*'
    if [[ $line =~ $source_re ]]; then
        emit_finding "$file" "$line_no" "UNSAFE_SOURCE" "HIGH" "source receives variable path"
    fi

    seteu_sub_re='\$\([^)]*\)[[:space:]]*&&[[:space:]]*[A-Za-z_][A-Za-z0-9_]*='
    seteu_cmd_re='^[[:space:]]*[A-Za-z0-9_./-]+([^&]|&[^&])*&&[[:space:]]*[A-Za-z_][A-Za-z0-9_]*=(true|false|ok)([[:space:]]|$)'
    if [ "$has_set_e" -eq 1 ] && { [[ $line =~ $seteu_sub_re ]] || [[ $line =~ $seteu_cmd_re ]]; }; then
        emit_finding "$file" "$line_no" "SET_EU_AND_PATTERN" "HIGH" "cmd && assignment under set -e can hide failure"
    fi

    command_sub_re='(^|[[:space:];])for[[:space:]].*[[:space:]]in[[:space:]]+\$\('
    if [[ $line =~ $command_sub_re ]]; then
        emit_finding "$file" "$line_no" "UNQUOTED_COMMAND_SUBSTITUTION" "MEDIUM" "Command substitution is unquoted"
    fi

    ifs_re='(^|[[:space:];])IFS='
    ifs_read_re='IFS=.*[[:space:]]read[[:space:]]'
    if [[ $line =~ $ifs_re ]] && [[ ! $line =~ $ifs_read_re ]] && [ "$has_ifs_restore" -eq 0 ]; then
        emit_finding "$file" "$line_no" "UNSAFE_IFS" "MEDIUM" "IFS changed without visible restore"
    fi

    pos_re='(^|[[:space:];])(rm|chmod|chown|mv|cp|cat|ln)[[:space:]][^#]*\$([0-9])'
    if [[ $line =~ $pos_re ]]; then
        matched_pos="\$${BASH_REMATCH[3]}"
        if ! has_previous_positional_validation "$file" "$matched_pos" "$line_no"; then
            emit_finding "$file" "$line_no" "UNVALIDATED_POSITIONAL" "HIGH" "Positional argument used in filesystem command before validation"
        fi
    fi
}

scan_file() {
    local file line_no line set_e ifs_restore
    file=$1

    if [ ! -r "$file" ]; then
        security_err "WARN: cannot read file: $(security_relpath "$file")"
        return 0
    fi
    security_should_skip_large_file "$file" "$MAX_SIZE" && return 0

    set_e=0
    if file_has_set_e "$file"; then
        set_e=1
    fi
    ifs_restore=0
    if file_has_ifs_restore "$file"; then
        ifs_restore=1
    fi

    line_no=0
    while IFS= read -r line || [ -n "$line" ]; do
        line_no=$((line_no + 1))
        scan_line "$file" "$line_no" "$line" "$set_e" "$ifs_restore"
        security_check_timeout "$STARTED" "$SCAN_TIMEOUT"
    done < "$file"
}

while IFS= read -r -d '' file; do
    security_is_shell_scope "$file" || continue
    scan_file "$file"
    security_check_timeout "$STARTED" "$SCAN_TIMEOUT"
done < <(security_find_repo_files)

if [ "$FOUND" -eq 0 ]; then
    printf 'No injection risks found.\n'
fi

if [ "$BLOCKING" -eq 1 ]; then
    exit 1
fi
exit 0

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

ASSIGNMENT_VALUE=''

extract_assignment_value() {
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
    ASSIGNMENT_VALUE=$value
    return 0
}

scan_line() {
    local file line_no line value private_prefix private_suffix pass_re secret_re api_re aws_secret_re
    file=$1
    line_no=$2
    line=${3:0:4096}

    security_line_is_comment_or_empty "$line" && return 0

    private_prefix='-----BEGIN '
    private_suffix=' PRIVATE KEY-----'
    if [[ $line == *"${private_prefix}RSA${private_suffix}"* ||
          $line == *"${private_prefix}EC${private_suffix}"* ||
          $line == *"${private_prefix}DSA${private_suffix}"* ||
          $line == *"${private_prefix}OPENSSH${private_suffix}"* ]]; then
        emit_finding "$file" "$line_no" "PRIVATE_KEY" "CRITICAL" "Private key block found"
    fi

    if [[ $line =~ (ghp_|gho_|ghs_|ghr_|github_pat_)[A-Za-z0-9]{30,} ]]; then
        emit_finding "$file" "$line_no" "GITHUB_TOKEN" "CRITICAL" "GitHub token found in assignment"
    fi

    if [[ $line =~ AKIA[0-9A-Z]{16} ]]; then
        emit_finding "$file" "$line_no" "AWS_ACCESS_KEY" "CRITICAL" "AWS access key id found"
    fi

    if [[ $line =~ tskey-[a-z]+-[A-Za-z0-9]{10,} ]]; then
        emit_finding "$file" "$line_no" "TAILSCALE_KEY" "CRITICAL" "Tailscale auth key found"
    fi

    if [[ $line =~ sshpass[[:space:]]+-p[[:space:]]+[^[:space:]]+ ]]; then
        emit_finding "$file" "$line_no" "SSH_PASSWORD_FLAG" "HIGH" "SSH password passed as command argument"
    fi

    if ! extract_assignment_value "$line"; then
        return 0
    fi
    value=$ASSIGNMENT_VALUE
    security_is_placeholder_value "$value" && return 0

    aws_secret_re='^[[:space:]]*(export[[:space:]]+)?[A-Za-z_][A-Za-z0-9_]*aws_secret[A-Za-z0-9_]*[[:space:]]*='
    if [[ $line =~ $aws_secret_re ]] && [ "${#value}" -ge 20 ]; then
        emit_finding "$file" "$line_no" "AWS_SECRET_KEY" "CRITICAL" "AWS secret key found in assignment"
    fi

    api_re='^[[:space:]]*(export[[:space:]]+)?(api_key|apikey|api-key)[[:space:]]*='
    if [[ $line =~ $api_re ]] &&
       [[ $value =~ ^[A-Za-z0-9/+]{16,} ]]; then
        emit_finding "$file" "$line_no" "GENERIC_API_KEY" "HIGH" "Generic API key found in assignment"
    fi

    pass_re='^[[:space:]]*(export[[:space:]]+)?(password|passwd|pwd)[[:space:]]*='
    if [[ $line =~ $pass_re ]]; then
        if [[ $value == *'$('* ]]; then
            emit_finding "$file" "$line_no" "HARDCODED_PASSWORD" "HIGH" "Password assignment uses command substitution"
        elif [[ $value != *'$'* && $value != *'{'* && $value != *'('* && ${#value} -ge 6 ]]; then
            emit_finding "$file" "$line_no" "HARDCODED_PASSWORD" "HIGH" "Hardcoded password found in assignment"
        fi
    fi

    secret_re='^[[:space:]]*(export[[:space:]]+)?(secret|token|credential)[[:space:]]*='
    if [[ $line =~ $secret_re ]] &&
       [[ $value != *'$'* && $value != *'{'* && $value != *'('* && ${#value} -ge 8 ]]; then
        emit_finding "$file" "$line_no" "GENERIC_SECRET" "HIGH" "Generic secret found in assignment"
    fi
}

scan_file() {
    local file line_no line
    file=$1

    if [ ! -r "$file" ]; then
        security_err "WARN: cannot read file: $(security_relpath "$file")"
        return 0
    fi
    security_should_skip_large_file "$file" "$MAX_SIZE" && return 0

    line_no=0
    while IFS= read -r line || [ -n "$line" ]; do
        line_no=$((line_no + 1))
        scan_line "$file" "$line_no" "$line"
        security_check_timeout "$STARTED" "$SCAN_TIMEOUT"
    done < "$file"
}

while IFS= read -r -d '' file; do
    rel=$(security_relpath "$file")
    security_is_standard_scope "$rel" || continue
    scan_file "$file"
    security_check_timeout "$STARTED" "$SCAN_TIMEOUT"
done < <(security_find_repo_files)

if [ "$FOUND" -eq 0 ]; then
    printf 'No secrets found.\n'
fi

if [ "$BLOCKING" -eq 1 ]; then
    exit 1
fi
exit 0

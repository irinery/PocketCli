#!/usr/bin/env bash
# shellcheck disable=SC2094
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

is_ssh_config_file() {
    local file rel
    file=$1
    rel=$(security_relpath "$file")

    case "$rel" in
        *ssh*config*) return 0 ;;
        *) return 1 ;;
    esac
}

scan_file() {
    local file line_no line in_if last_has_session_line is_config strict_inline_re strict_config_re password_re key_re
    local forward_re no_hostkey_re ts_inline_re ts_assign_re tmux_name_re tmux_name tmux_kill_re
    file=$1

    if [ ! -r "$file" ]; then
        security_err "WARN: cannot read file: $(security_relpath "$file")"
        return 0
    fi
    security_should_skip_large_file "$file" "$MAX_SIZE" && return 0

    line_no=0
    in_if=0
    is_config=0
    if is_ssh_config_file "$file"; then
        is_config=1
    fi
    last_has_session_line=0
    strict_inline_re='-o[[:space:]]+StrictHostKeyChecking[[:space:]]*=[[:space:]]*no'
    strict_config_re='(^|[[:space:]])StrictHostKeyChecking[[:space:]]+no([[:space:]]|$)'
    password_re='(^|[[:space:];])sshpass[[:space:]]+-p'
    key_re='(^|[[:space:];])ssh[[:space:]][^#]*-i[[:space:]]+/[^[:space:]]+'
    forward_re='(^|[[:space:]])ForwardAgent[[:space:]]+yes([[:space:]]|$)'
    no_hostkey_re='(^|[[:space:]])UserKnownHostsFile[[:space:]]+/dev/null([[:space:]]|$)'
    ts_inline_re='--authkey[[:space:]]*=[[:space:]]*tskey-[a-z]+-[A-Za-z0-9]{10,}'
    ts_assign_re='(^|[[:space:];])TAILSCALE_AUTHKEY[[:space:]]*=[[:space:]]*tskey-'
    tmux_name_re='(^|[[:space:];])tmux[[:space:]]+new(-session)?([^#]*[[:space:]])-s[[:space:]]+([A-Za-z0-9_:-]+)'
    tmux_kill_re='(^|[[:space:];])tmux[[:space:]]+kill-server([[:space:]]|$)'

    while IFS= read -r line || [ -n "$line" ]; do
        line_no=$((line_no + 1))
        line=${line:0:4096}
        security_line_is_comment_or_empty "$line" && continue

        if [[ $line =~ (^|[[:space:];])if[[:space:]] ]]; then
            in_if=$((in_if + 1))
        fi

        if [[ $line =~ tmux[[:space:]]+has-session ]]; then
            last_has_session_line=$line_no
        fi

        if [[ $line =~ $strict_inline_re ]]; then
            emit_finding "$file" "$line_no" "SSH_STRICT_HOST_DISABLED" "HIGH" "SSH strict host key checking disabled inline"
        fi

        if [ "$is_config" -eq 1 ] && [[ $line =~ $strict_config_re ]]; then
            emit_finding "$file" "$line_no" "SSH_CONFIG_STRICT_HOST_DISABLED" "HIGH" "SSH config disables strict host key checking"
        fi

        if [[ $line =~ $password_re ]]; then
            emit_finding "$file" "$line_no" "SSH_PASSWORD_IN_SCRIPT" "CRITICAL" "SSH password appears in script"
        fi

        if [[ $line =~ $key_re ]]; then
            emit_finding "$file" "$line_no" "SSH_HARDCODED_KEY_PATH" "MEDIUM" "SSH key path is hardcoded"
        fi

        if [[ $line =~ $forward_re ]]; then
            emit_finding "$file" "$line_no" "SSH_FORWARD_AGENT" "MEDIUM" "SSH agent forwarding enabled"
        fi

        if [[ $line =~ $no_hostkey_re ]]; then
            emit_finding "$file" "$line_no" "SSH_NO_HOSTKEY_VERIFY" "HIGH" "SSH host key storage disabled"
        fi

        if [[ $line =~ $ts_inline_re ]]; then
            emit_finding "$file" "$line_no" "TAILSCALE_KEY_INLINE" "CRITICAL" "Tailscale auth key is inline"
        fi

        if [[ $line =~ $ts_assign_re ]]; then
            emit_finding "$file" "$line_no" "TAILSCALE_KEY_ENV_UNSAFE" "CRITICAL" "Tailscale auth key assigned directly"
        fi

        if [[ $line =~ $tmux_name_re ]]; then
            tmux_name=${BASH_REMATCH[4]}
            case "$tmux_name" in
                pocket_*) ;;
                *) emit_finding "$file" "$line_no" "TMUX_SESSION_NONSTANDARD_NAME" "LOW" "tmux session name should use pocket_ prefix" ;;
            esac
        fi

        if [[ $line =~ $tmux_kill_re ]]; then
            if [[ $line == *'&&'*'tmux kill-server'* || $line == *'||'*'tmux kill-server'* ]]; then
                :
            elif [ "$in_if" -gt 0 ]; then
                :
            elif [ "$last_has_session_line" -gt 0 ] && [ "$((line_no - last_has_session_line))" -le 1 ]; then
                :
            else
                emit_finding "$file" "$line_no" "TMUX_KILL_SERVER_UNCONDITIONAL" "HIGH" "tmux kill-server should be guarded"
            fi
        fi

        if [[ $line =~ (^|[[:space:];])fi([[:space:];]|$) ]] && [ "$in_if" -gt 0 ]; then
            in_if=$((in_if - 1))
        fi

        security_check_timeout "$STARTED" "$SCAN_TIMEOUT"
    done < "$file"
}

while IFS= read -r -d '' file; do
    security_is_hardening_scope "$file" || continue
    scan_file "$file"
    security_check_timeout "$STARTED" "$SCAN_TIMEOUT"
done < <(security_find_repo_files)

if [ "$FOUND" -eq 0 ]; then
    printf 'No SSH/Tailscale/tmux issues found.\n'
fi

if [ "$BLOCKING" -eq 1 ]; then
    exit 1
fi
exit 0

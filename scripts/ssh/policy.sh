#!/usr/bin/env sh

. "${POCKETCLI_DIR}/scripts/runtime/paths.sh"

ssh_policy_ensure_file() {
    FILE="$(pocket_ssh_policy_file)"
    [ -s "${FILE}" ] && return 0
    mkdir -p "$(dirname "${FILE}")"
    cat > "${FILE}" <<JSON
{
  "schema_version": 1,
  "host_key_policy": "accept-new",
  "connect_timeout_sec": 10,
  "strict_mode": false,
  "require_approval_for_unknown": true,
  "auto_approve_saved_hosts": true,
  "auto_approve_tailscale_known_hosts": true,
  "log_all_connections": true
}
JSON
}

ssh_policy_get() {
    KEY="$1"
    FILE="$(pocket_ssh_policy_file)"
    ssh_policy_ensure_file

    if command -v jq >/dev/null 2>&1; then
        jq -r ".${KEY}" "${FILE}" 2>/dev/null
        return 0
    fi

    case "${KEY}" in
        connect_timeout_sec) printf '10\n' ;;
        host_key_policy) printf 'accept-new\n' ;;
        strict_mode) printf 'false\n' ;;
        require_approval_for_unknown) printf 'true\n' ;;
        auto_approve_saved_hosts|auto_approve_tailscale_known_hosts|log_all_connections) printf 'true\n' ;;
        *) printf '\n' ;;
    esac
}

ssh_policy_flags() {
    HK=$(ssh_policy_get host_key_policy)
    CT=$(ssh_policy_get connect_timeout_sec)
    printf '%s' "-o StrictHostKeyChecking=${HK} -o ConnectTimeout=${CT}"
}

ssh_command_policy_normalize() {
    printf '%s\n' "$*" | awk '{$1=$1; print}'
}

ssh_command_policy_evaluate() {
    COMMAND=$(ssh_command_policy_normalize "$*")
    LOWER=$(printf '%s' "${COMMAND}" | tr '[:upper:]' '[:lower:]')

    case "${LOWER}" in
        *"curl "*" | sh"|*"curl "*" | bash"|*"wget "*" | sh"|*"wget "*" | bash"|*"base64 -d "*" | sh"|*"base64 -d "*" | bash"|*"python -c"*|*"python3 -c"*|*"perl -e"*|*"ruby -e"*)
            printf '%s|%s|%s|%s\n' blocked destructive false remote_code_execution
            return 0
            ;;
        *"rm -rf /"*|*"rm -rf /*"*|*"mkfs"*|*"dd if="*|*":(){ :|:& };:"*|*"chmod -r 777 /"*|*"chown -r"*|*"shutdown"*|*"reboot"*|*"halt"*|*"poweroff"*|*"truncate -s 0 /etc"*|*"/dev/null"*)
            printf '%s|%s|%s|%s\n' blocked destructive false destructive_command
            return 0
            ;;
        *"iptables -f"*|*"iptables --flush"*|*"ufw disable"*|*"ufw reset"*|*"systemctl stop ssh"*|*"systemctl restart ssh"*|*"systemctl disable ssh"*)
            printf '%s|%s|%s|%s\n' blocked destructive false network_lockout
            return 0
            ;;
    esac

    case "${COMMAND}" in
        *';'*|*'&&'*|*'||'*|*'|'*|*'`'*|*'$('*|*'>'*|*'<'*)
            printf '%s|%s|%s|%s\n' blocked destructive false shell_injection_attempt
            return 0
            ;;
    esac

    BASE="${COMMAND}"
    SUDO=false
    case "${BASE}" in
    sudo\ *)
        SUDO=true
        BASE=${BASE#sudo }
        while :; do
            case "${BASE}" in
                -[![:space:]]*\ *) BASE=${BASE#* } ;;
                *) break ;;
            esac
        done
        ;;
    esac

    RISK=
    case "${BASE}" in
        uptime|whoami|hostname|date|"df -h"|"df -hT"|"free -m"|"free -h"|"ps aux"|"ps -ef"|"ss -tulpn"|"netstat -tulpn"|"uname -a"|"cat /etc/os-release"|env|printenv)
            RISK=read_only
            ;;
        "ip addr"|ip\ addr\ *|"ip route"|ip\ route\ *|"ip link"|ip\ link\ *)
            RISK=read_only
            ;;
        journalctl|journalctl\ *|systemctl\ status|systemctl\ status\ *|systemctl\ list-units|systemctl\ list-units\ *|docker\ ps|docker\ ps\ *|docker\ logs|docker\ logs\ *|docker\ inspect|docker\ inspect\ *|kubectl\ get|kubectl\ get\ *|kubectl\ describe|kubectl\ describe\ *|kubectl\ logs|kubectl\ logs\ *|"cat /var/log/syslog"|"cat /var/log/auth.log"|tail\ -n\ *|tail\ -f\ *|grep|grep\ *|find|find\ *|"ls -la"|ls\ -la\ *|"ls -lah"|ls\ -lah\ *|du\ -sh|du\ -sh\ *|du\ -h|du\ -h\ *)
            RISK=diagnostic
            ;;
        systemctl\ restart|systemctl\ restart\ *|systemctl\ reload|systemctl\ reload\ *)
            RISK=service_restart
            ;;
    esac

    if [ -z "${RISK}" ]; then
        printf '%s|%s|%s|%s\n' blocked read_only false not_in_allowlist
        return 0
    fi

    if [ "${SUDO}" = "true" ]; then
        RISK=$(ssh_command_policy_elevate_risk "${RISK}")
    fi

    case "${RISK}" in
        read_only|diagnostic)
            printf '%s|%s|%s|\n' allow "${RISK}" false
            ;;
        service_restart|file_change|network_change)
            printf '%s|%s|%s|\n' pending_approval "${RISK}" true
            ;;
        *)
            printf '%s|%s|%s|%s\n' blocked destructive false destructive_command
            ;;
    esac
}

ssh_command_policy_elevate_risk() {
    case "$1" in
        read_only) printf '%s\n' diagnostic ;;
        diagnostic) printf '%s\n' service_restart ;;
        service_restart) printf '%s\n' file_change ;;
        file_change) printf '%s\n' network_change ;;
        network_change) printf '%s\n' destructive ;;
        *) printf '%s\n' destructive ;;
    esac
}

#!/usr/bin/env sh
set -eu

POCKETCLI_DIR="${POCKETCLI_DIR:-${HOME}/.pocketcli}"
. "${POCKETCLI_DIR}/lib/common.sh"
. "${POCKETCLI_DIR}/scripts/runtime/context.sh"
. "${POCKETCLI_DIR}/scripts/inventory/refresh.sh"
. "${POCKETCLI_DIR}/scripts/inventory/approvals.sh"
. "${POCKETCLI_DIR}/scripts/ssh/policy.sh"
. "${POCKETCLI_DIR}/scripts/ssh/resolve.sh"

pocket_ssh_run() {
    ACTION="$1"
    HOST_INPUT="$2"
    shift 2

    pocket_runtime_context_boot "${POCKETCLI_MODE:-auto}"
    inventory_refresh || true

    ssh_resolve_target "${HOST_INPUT}" || die "Invalid target."

    POLICY_STRICT=$(ssh_policy_get strict_mode)
    REQ_APPROVAL=$(ssh_policy_get require_approval_for_unknown)

    NEED_APPROVAL=false
    if [ "${TARGET_APPROVED}" != "true" ] && [ "${REQ_APPROVAL}" = "true" ]; then
        NEED_APPROVAL=true
    fi
    if [ "${POLICY_STRICT}" = "true" ] && [ "${TARGET_SOURCE}" = "unknown" ]; then
        die "Strict SSH policy blocked unknown host '${HOST_INPUT}'."
    fi

    if [ "${NEED_APPROVAL}" = "true" ]; then
        approval_prompt_for_host "${TARGET_HOST}" "${ACTION}" "${MODE_EFFECTIVE}-default" || die "SSH session not approved."
        pocket_log_event "event=approval host=${TARGET_HOST} action=${ACTION} approved=true"
    else
        pocket_log_event "event=approval host=${TARGET_HOST} action=${ACTION} approved=implicit"
    fi

    FLAGS=$(ssh_policy_flags)
    # shellcheck disable=SC2086
    case "${ACTION}" in
        interactive)
            pocket_log_event "event=ssh_connect host=${TARGET_HOST}"
            exec ssh ${FLAGS} "${TARGET_HOST}" "$@"
            ;;
        exec)
            pocket_log_event "event=ssh_exec host=${TARGET_HOST}"
            exec ssh ${FLAGS} "${TARGET_HOST}" -- "$@"
            ;;
        copy)
            pocket_log_event "event=ssh_copy host=${TARGET_HOST}"
            exec scp -o StrictHostKeyChecking=$(ssh_policy_get host_key_policy) -o ConnectTimeout=$(ssh_policy_get connect_timeout_sec) -r "$@"
            ;;
        probe)
            pocket_log_event "event=ssh_probe host=${TARGET_HOST}"
            ssh ${FLAGS} -n -o BatchMode=yes "${TARGET_HOST}" true >/dev/null 2>&1
            ;;
        *)
            die "Unsupported SSH action: ${ACTION}"
            ;;
    esac
}

if [ "${1:-}" = "--run" ]; then
    shift
    pocket_ssh_run "$@"
fi

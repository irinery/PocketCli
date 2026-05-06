#!/usr/bin/env sh

set -eu

PROJECT_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
TMP_ROOT=$(mktemp -d)
trap 'rm -rf "${TMP_ROOT}"' EXIT INT TERM

fail() {
    printf 'FAIL %s\n' "$1" >&2
    exit 1
}

assert_rc() {
    expected=$1
    actual=$2
    label=$3
    [ "$expected" -eq "$actual" ] || fail "$label: expected rc $expected, got $actual"
}

assert_contains() {
    file=$1
    needle=$2
    label=$3
    if ! grep -F "$needle" "$file" >/dev/null 2>&1; then
        printf '%s\n' '--- output ---' >&2
        cat "$file" >&2 || true
        fail "$label: missing $needle"
    fi
}

assert_not_contains() {
    file=$1
    needle=$2
    label=$3
    if grep -F "$needle" "$file" >/dev/null 2>&1; then
        printf '%s\n' '--- output ---' >&2
        cat "$file" >&2 || true
        fail "$label: unexpected $needle"
    fi
}

new_repo() {
    dir=$(mktemp -d "${TMP_ROOT}/repo.XXXXXX")
    mkdir -p "$dir"
    printf '%s\n' "$dir"
}

run_scanner() {
    script=$1
    repo=$2
    out=$3
    err=$4
    shift 4

    set +e
    REPO_ROOT=$repo bash "$PROJECT_ROOT/$script" "$@" > "$out" 2> "$err"
    rc=$?
    set -e
    return "$rc"
}

write_shell() {
    file=$1
    shift
    mkdir -p "$(dirname "$file")"
    {
        printf '#!/usr/bin/env sh\n'
        printf '%s\n' "$@"
    } > "$file"
    chmod 750 "$file"
}

test_secret_scanner() {
    out=$TMP_ROOT/secret.out
    err=$TMP_ROOT/secret.err
    dollar='$'

    repo=$(new_repo)
    github_prefix='ghp_'
    github_body='ABCDEF1234567890abcdef1234567890ab'
    write_shell "$repo/github.sh" "TOKEN=${github_prefix}${github_body}"
    run_scanner scripts/security/01_secret_leak_scanner.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 1 "${rc:-0}" T01-01
    assert_contains "$out" "GITHUB_TOKEN:CRITICAL" T01-01
    unset rc

    repo=$(new_repo)
    aws_prefix='AKIA'
    aws_body='IOSFODNN7EXAMPLE'
    write_shell "$repo/aws.sh" "AWS_ACCESS_KEY_ID=${aws_prefix}${aws_body}"
    run_scanner scripts/security/01_secret_leak_scanner.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 1 "${rc:-0}" T01-02
    assert_contains "$out" "AWS_ACCESS_KEY:CRITICAL" T01-02
    unset rc

    repo=$(new_repo)
    printf '%s\n' 'DATABASE_URL=postgres://user:PLACEHOLDER@host/db' > "$repo/app.env.example"
    run_scanner scripts/security/01_secret_leak_scanner.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 0 "${rc:-0}" T01-03
    assert_not_contains "$out" "GENERIC_SECRET" T01-03
    unset rc

    repo=$(new_repo)
    pem_prefix='-----BEGIN'
    pem_suffix='-----'
    write_shell "$repo/key.sh" "${pem_prefix} RSA PRIVATE KEY${pem_suffix}"
    run_scanner scripts/security/01_secret_leak_scanner.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 1 "${rc:-0}" T01-04
    assert_contains "$out" "PRIVATE_KEY:CRITICAL" T01-04
    unset rc

    repo=$(new_repo)
    mkdir -p "$repo/.git"
    write_shell "$repo/.git/token.sh" "TOKEN=${github_prefix}${github_body}"
    run_scanner scripts/security/01_secret_leak_scanner.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 0 "${rc:-0}" T01-05
    assert_not_contains "$out" ".git/token.sh" T01-05
    unset rc

    repo=$(new_repo)
    printf '%s\n' 'export API_KEY=sk-1234abcd' > "$repo/README.md"
    run_scanner scripts/security/01_secret_leak_scanner.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 0 "${rc:-0}" T01-06
    assert_contains "$out" "No secrets found." T01-06
    unset rc

    repo=$(new_repo)
    write_shell "$repo/clean.sh" 'printf "%s\n" ok'
    run_scanner scripts/security/01_secret_leak_scanner.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 0 "${rc:-0}" T01-07
    assert_contains "$out" "No secrets found." T01-07
    unset rc

    missing=$TMP_ROOT/missing-secret-root
    run_scanner scripts/security/01_secret_leak_scanner.sh "$missing" "$out" "$err" || rc=$?
    assert_rc 2 "${rc:-0}" T01-08
    assert_contains "$err" "ERROR: REPO_ROOT not found: $missing" T01-08
    unset rc

    repo=$(new_repo)
    write_shell "$repo/pass.sh" "password=\"${dollar}(cat /etc/shadow)\""
    run_scanner scripts/security/01_secret_leak_scanner.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 1 "${rc:-0}" T01-09
    assert_contains "$out" "HARDCODED_PASSWORD:HIGH" T01-09
    unset rc
}

test_shell_injection_scanner() {
    out=$TMP_ROOT/injection.out
    err=$TMP_ROOT/injection.err
    dollar='$'
    shell_word='sh'

    repo=$(new_repo)
    write_shell "$repo/rm.sh" "rm -rf ${dollar}USER_INPUT"
    run_scanner scripts/security/02_shell_injection_audit.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 1 "${rc:-0}" T02-01
    assert_contains "$out" "UNQUOTED_VAR_DANGEROUS_CMD:HIGH" T02-01
    unset rc

    repo=$(new_repo)
    write_shell "$repo/eval-var.sh" "eval \"${dollar}USER_CMD\""
    run_scanner scripts/security/02_shell_injection_audit.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 1 "${rc:-0}" T02-02
    assert_contains "$out" "EVAL_WITH_VARIABLE:HIGH" T02-02
    unset rc

    repo=$(new_repo)
    write_shell "$repo/eval-lit.sh" 'eval "echo hello"'
    run_scanner scripts/security/02_shell_injection_audit.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 0 "${rc:-0}" T02-03
    assert_contains "$out" "EVAL_LITERAL:MEDIUM" T02-03
    unset rc

    repo=$(new_repo)
    set_eu_line='set -'
    set_eu_line=${set_eu_line}eu
    and_status='&'
    and_status=${and_status}'& STATUS=ok'
    write_shell "$repo/seteu.sh" "$set_eu_line" "result=${dollar}(ssh host cmd) ${and_status}"
    run_scanner scripts/security/02_shell_injection_audit.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 1 "${rc:-0}" T02-04
    assert_contains "$out" "SET_EU_AND_PATTERN:HIGH" T02-04
    unset rc

    repo=$(new_repo)
    write_shell "$repo/safe.sh" "cat \"${dollar}SAFE_VAR\""
    run_scanner scripts/security/02_shell_injection_audit.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 0 "${rc:-0}" T02-05
    assert_contains "$out" "No injection risks found." T02-05
    unset rc

    repo=$(new_repo)
    write_shell "$repo/ssh.sh" "ssh ${dollar}HOST ${dollar}CMD"
    run_scanner scripts/security/02_shell_injection_audit.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 1 "${rc:-0}" T02-06
    assert_contains "$out" "UNQUOTED_VAR_NETWORK_CMD:HIGH" T02-06
    unset rc

    repo=$(new_repo)
    write_shell "$repo/cp.sh" "cp ${dollar}1 /etc/cron.d/"
    run_scanner scripts/security/02_shell_injection_audit.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 1 "${rc:-0}" T02-07
    assert_contains "$out" "PATH_TRAVERSAL_RISK:HIGH" T02-07
    unset rc

    repo=$(new_repo)
    write_shell "$repo/source.sh" "source ${dollar}CONFIG_FILE"
    run_scanner scripts/security/02_shell_injection_audit.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 1 "${rc:-0}" T02-08
    assert_contains "$out" "UNSAFE_SOURCE:HIGH" T02-08
    unset rc

    repo=$(new_repo)
    write_shell "$repo/loop.sh" "for f in ${dollar}( ls ); do echo \"${dollar}f\"; done"
    run_scanner scripts/security/02_shell_injection_audit.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 0 "${rc:-0}" T02-09
    assert_contains "$out" "UNQUOTED_COMMAND_SUBSTITUTION:MEDIUM" T02-09
    unset rc

    repo=$(new_repo)
    write_shell "$repo/curl.sh" "curl -s \"${dollar}URL\" | ba${shell_word}"
    run_scanner scripts/security/02_shell_injection_audit.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 1 "${rc:-0}" T02-10
    assert_contains "$out" "PIPE_TO_SHELL:CRITICAL" T02-10
    unset rc

    repo=$(new_repo)
    write_shell "$repo/wget.sh" "wget -O- ${dollar}URL | ${shell_word}"
    run_scanner scripts/security/02_shell_injection_audit.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 1 "${rc:-0}" T02-11
    assert_contains "$out" "PIPE_TO_SHELL:CRITICAL" T02-11
    unset rc
}

test_permission_scanner() {
    out=$TMP_ROOT/perm.out
    err=$TMP_ROOT/perm.err

    repo=$(new_repo)
    write_shell "$repo/scripts/connect.sh" 'printf ok'
    chmod 755 "$repo/scripts/connect.sh"
    run_scanner scripts/security/03_filesystem_permission_audit.sh "$repo" "$out" "$err" repo || rc=$?
    assert_rc 0 "${rc:-0}" T03-01
    assert_contains "$out" "SCRIPT_WORLD_EXECUTABLE:MEDIUM" T03-01
    unset rc

    repo=$(new_repo)
    printf 'x\n' > "$repo/open.txt"
    chmod 777 "$repo/open.txt"
    run_scanner scripts/security/03_filesystem_permission_audit.sh "$repo" "$out" "$err" repo || rc=$?
    assert_rc 1 "${rc:-0}" T03-02
    assert_contains "$out" "WORLD_WRITABLE:HIGH" T03-02
    unset rc

    repo=$(new_repo)
    printf 'x\n' > "$repo/.env"
    chmod 644 "$repo/.env"
    run_scanner scripts/security/03_filesystem_permission_audit.sh "$repo" "$out" "$err" repo || rc=$?
    assert_rc 1 "${rc:-0}" T03-03
    assert_contains "$out" "SENSITIVE_FILE_WORLD_READABLE:HIGH" T03-03
    unset rc

    repo=$(new_repo)
    printf 'readme\n' > "$repo/README.md"
    chmod 755 "$repo/README.md"
    run_scanner scripts/security/03_filesystem_permission_audit.sh "$repo" "$out" "$err" repo || rc=$?
    assert_rc 0 "${rc:-0}" T03-04
    assert_contains "$out" "NON_SHELL_EXECUTABLE:LOW" T03-04
    unset rc

    repo=$(new_repo)
    write_shell "$repo/scripts/install.sh" 'printf ok'
    chmod 750 "$repo/scripts/install.sh"
    run_scanner scripts/security/03_filesystem_permission_audit.sh "$repo" "$out" "$err" repo || rc=$?
    assert_rc 0 "${rc:-0}" T03-05
    assert_contains "$out" "No permission issues found." T03-05
    unset rc

    repo=$(new_repo)
    home=$(mktemp -d "${TMP_ROOT}/home.XXXXXX")
    mkdir -p "$home/.ssh"
    printf 'Host *\n' > "$home/.ssh/config"
    chmod 644 "$home/.ssh/config"
    set +e
    HOME=$home REPO_ROOT=$repo bash "$PROJECT_ROOT/scripts/security/03_filesystem_permission_audit.sh" local > "$out" 2> "$err"
    rc=$?
    set -e
    assert_rc 1 "$rc" T03-06
    assert_contains "$out" "SSH_CONFIG_TOO_OPEN:HIGH" T03-06

    repo=$(new_repo)
    home=$(mktemp -d "${TMP_ROOT}/home.XXXXXX")
    mkdir -p "$home/.pocketcli"
    chmod 755 "$home/.pocketcli"
    set +e
    HOME=$home REPO_ROOT=$repo bash "$PROJECT_ROOT/scripts/security/03_filesystem_permission_audit.sh" local > "$out" 2> "$err"
    rc=$?
    set -e
    assert_rc 1 "$rc" T03-07
    assert_contains "$out" "POCKETCLI_DIR_TOO_OPEN:HIGH" T03-07

    repo=$(new_repo)
    mkdir -p "$repo/logs"
    printf 'session\n' > "$repo/logs/sessions.log"
    chmod 644 "$repo/logs/sessions.log"
    run_scanner scripts/security/03_filesystem_permission_audit.sh "$repo" "$out" "$err" repo || rc=$?
    assert_rc 0 "${rc:-0}" T03-08
    assert_contains "$out" "LOG_FILE_WORLD_READABLE:MEDIUM" T03-08
    unset rc

    repo=$(new_repo)
    write_shell "$repo/good.sh" 'printf ok'
    chmod 750 "$repo/good.sh"
    run_scanner scripts/security/03_filesystem_permission_audit.sh "$repo" "$out" "$err" repo || rc=$?
    assert_rc 0 "${rc:-0}" T03-09
    assert_contains "$out" "No permission issues found." T03-09
    unset rc

    repo=$(new_repo)
    printf 'secret\n' > "$repo/mycredential.txt"
    chmod 777 "$repo/mycredential.txt"
    run_scanner scripts/security/03_filesystem_permission_audit.sh "$repo" "$out" "$err" repo || rc=$?
    assert_rc 1 "${rc:-0}" T03-10
    assert_contains "$out" "WORLD_WRITABLE:HIGH" T03-10
    assert_contains "$out" "SENSITIVE_FILE_WORLD_READABLE:HIGH" T03-10
    unset rc
}

test_hardening_scanner() {
    out=$TMP_ROOT/hardening.out
    err=$TMP_ROOT/hardening.err
    dollar='$'
    sshpass_cmd='sshpass'
    ts_prefix='tskey-auth-'
    ts_body='ABCDEF1234567890'

    repo=$(new_repo)
    strict_key='StrictHostKeyChecking'
    strict_value='no'
    write_shell "$repo/strict.sh" "ssh -o ${strict_key}=${strict_value} user@host"
    run_scanner scripts/security/04_ssh_tailscale_hardening.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 1 "${rc:-0}" T04-01
    assert_contains "$out" "SSH_STRICT_HOST_DISABLED:HIGH" T04-01
    unset rc

    repo=$(new_repo)
    strict_key='StrictHostKeyChecking'
    strict_value='no'
    printf '%s %s\n' "$strict_key" "$strict_value" > "$repo/ssh_config"
    run_scanner scripts/security/04_ssh_tailscale_hardening.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 1 "${rc:-0}" T04-02
    assert_contains "$out" "SSH_CONFIG_STRICT_HOST_DISABLED:HIGH" T04-02
    unset rc

    repo=$(new_repo)
    write_shell "$repo/password.sh" "${sshpass_cmd} -p \"minhasenha\" ssh user@host"
    run_scanner scripts/security/04_ssh_tailscale_hardening.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 1 "${rc:-0}" T04-03
    assert_contains "$out" "SSH_PASSWORD_IN_SCRIPT:CRITICAL" T04-03
    unset rc

    repo=$(new_repo)
    write_shell "$repo/key.sh" 'ssh -i /home/user/.ssh/id_rsa_prod user@host'
    run_scanner scripts/security/04_ssh_tailscale_hardening.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 0 "${rc:-0}" T04-04
    assert_contains "$out" "SSH_HARDCODED_KEY_PATH:MEDIUM" T04-04
    unset rc

    repo=$(new_repo)
    write_shell "$repo/tailscale.sh" "tailscale up --authkey=${ts_prefix}${ts_body}"
    run_scanner scripts/security/04_ssh_tailscale_hardening.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 1 "${rc:-0}" T04-05
    assert_contains "$out" "TAILSCALE_KEY_INLINE:CRITICAL" T04-05
    unset rc

    repo=$(new_repo)
    write_shell "$repo/tmux-bad.sh" 'tmux new-session -d -s my_session'
    run_scanner scripts/security/04_ssh_tailscale_hardening.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 0 "${rc:-0}" T04-06
    assert_contains "$out" "TMUX_SESSION_NONSTANDARD_NAME:LOW" T04-06
    unset rc

    repo=$(new_repo)
    write_shell "$repo/tmux-good.sh" 'tmux new-session -d -s pocket_deploy'
    run_scanner scripts/security/04_ssh_tailscale_hardening.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 0 "${rc:-0}" T04-07
    assert_contains "$out" "No SSH/Tailscale/tmux issues found." T04-07
    unset rc

    repo=$(new_repo)
    write_shell "$repo/tmux-kill.sh" 'tmux kill-server'
    run_scanner scripts/security/04_ssh_tailscale_hardening.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 1 "${rc:-0}" T04-08
    assert_contains "$out" "TMUX_KILL_SERVER_UNCONDITIONAL:HIGH" T04-08
    unset rc

    repo=$(new_repo)
    forward_key='ForwardAgent'
    forward_value='yes'
    printf '%s %s\n' "$forward_key" "$forward_value" > "$repo/ssh_config"
    run_scanner scripts/security/04_ssh_tailscale_hardening.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 0 "${rc:-0}" T04-09
    assert_contains "$out" "SSH_FORWARD_AGENT:MEDIUM" T04-09
    unset rc

    repo=$(new_repo)
    write_shell "$repo/safe.sh" 'ssh -o StrictHostKeyChecking=yes user@host true' 'tmux new-session -d -s pocket_ops'
    run_scanner scripts/security/04_ssh_tailscale_hardening.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 0 "${rc:-0}" T04-10
    assert_contains "$out" "No SSH/Tailscale/tmux issues found." T04-10
    unset rc

    repo=$(new_repo)
    write_shell "$repo/tailscale-env.sh" "tailscale up --authkey=${dollar}TAILSCALE_KEY"
    run_scanner scripts/security/04_ssh_tailscale_hardening.sh "$repo" "$out" "$err" || rc=$?
    assert_rc 0 "${rc:-0}" T04-11
    assert_contains "$out" "No SSH/Tailscale/tmux issues found." T04-11
    unset rc
}

test_security_gate() {
    results=$TMP_ROOT/results
    report=$TMP_ROOT/security-report.txt
    summary=$TMP_ROOT/summary.md
    outputs=$TMP_ROOT/github-output.txt
    mkdir -p "$results"

    printf '%s\n' 'app.sh:1:GITHUB_TOKEN:CRITICAL:GitHub token found' > "$results/01.out"
    printf '1\n' > "$results/01.rc"
    : > "$results/01.err"
    for id in 02 03 04; do
        printf 'No findings.\n' > "$results/$id.out"
        printf '0\n' > "$results/$id.rc"
        : > "$results/$id.err"
    done
    GITHUB_OUTPUT=$outputs GITHUB_STEP_SUMMARY=$summary SECURITY_RESULTS_DIR=$results SECURITY_REPORT_FILE=$report bash "$PROJECT_ROOT/scripts/security/aggregate_results.sh" >/dev/null
    assert_contains "$outputs" "has_blocking_findings=true" T05-01
    assert_contains "$summary" "GITHUB_TOKEN" T05-01

    rm -rf "$results"
    mkdir -p "$results"
    : > "$outputs"
    for id in 01 02 03 04; do
        printf 'No findings.\n' > "$results/$id.out"
        printf '0\n' > "$results/$id.rc"
        : > "$results/$id.err"
    done
    GITHUB_OUTPUT=$outputs GITHUB_STEP_SUMMARY=$summary SECURITY_RESULTS_DIR=$results SECURITY_REPORT_FILE=$report bash "$PROJECT_ROOT/scripts/security/aggregate_results.sh" >/dev/null
    assert_contains "$outputs" "has_blocking_findings=false" T05-02
    assert_contains "$summary" "✅ No critical findings" T05-02
    assert_contains "$report" "No findings." T05-06

    rm -rf "$results"
    mkdir -p "$results"
    : > "$outputs"
    printf '%s\n' 'script.sh:0755:SCRIPT_WORLD_EXECUTABLE:MEDIUM:warn only' > "$results/03.out"
    printf '0\n' > "$results/03.rc"
    : > "$results/03.err"
    for id in 01 02 04; do
        printf 'No findings.\n' > "$results/$id.out"
        printf '0\n' > "$results/$id.rc"
        : > "$results/$id.err"
    done
    GITHUB_OUTPUT=$outputs GITHUB_STEP_SUMMARY=$summary SECURITY_RESULTS_DIR=$results SECURITY_REPORT_FILE=$report bash "$PROJECT_ROOT/scripts/security/aggregate_results.sh" >/dev/null
    assert_contains "$outputs" "has_blocking_findings=false" T05-03
    assert_contains "$summary" "WARN" T05-03

    rm -rf "$results"
    mkdir -p "$results"
    : > "$outputs"
    for id in 01 03 04; do
        printf 'No findings.\n' > "$results/$id.out"
        printf '0\n' > "$results/$id.rc"
        : > "$results/$id.err"
    done
    : > "$results/02.out"
    printf '2\n' > "$results/02.rc"
    printf 'boom\n' > "$results/02.err"
    GITHUB_OUTPUT=$outputs GITHUB_STEP_SUMMARY=$summary SECURITY_RESULTS_DIR=$results SECURITY_REPORT_FILE=$report bash "$PROJECT_ROOT/scripts/security/aggregate_results.sh" >/dev/null
    assert_contains "$outputs" "has_blocking_findings=true" T05-04
    assert_contains "$summary" "Scanner 02 execution error" T05-04

    rm -rf "$results"
    mkdir -p "$results"
    printf '%s\n' 'app.sh:1:GITHUB_TOKEN:CRITICAL:GitHub token found' > "$results/01.out"
    printf '%s\n' 'ssh.sh:1:SSH_STRICT_HOST_DISABLED:HIGH:strict disabled' > "$results/04.out"
    printf '1\n' > "$results/01.rc"
    printf '1\n' > "$results/04.rc"
    : > "$results/01.err"
    : > "$results/04.err"
    for id in 02 03; do
        printf 'No findings.\n' > "$results/$id.out"
        printf '0\n' > "$results/$id.rc"
        : > "$results/$id.err"
    done
    GITHUB_OUTPUT=$outputs GITHUB_STEP_SUMMARY=$summary SECURITY_RESULTS_DIR=$results SECURITY_REPORT_FILE=$report bash "$PROJECT_ROOT/scripts/security/aggregate_results.sh" >/dev/null
    assert_contains "$report" "GITHUB_TOKEN" T05-05
    assert_contains "$report" "SSH_STRICT_HOST_DISABLED" T05-05

    assert_contains "$PROJECT_ROOT/.github/workflows/security-gate.yml" "timeout-minutes: 5" T05-07
    assert_contains "$PROJECT_ROOT/docs/security-gate.md" "Security Gate / security-gate" T05-08
}

test_secret_scanner
test_shell_injection_scanner
test_permission_scanner
test_hardening_scanner
test_security_gate

printf 'security scanner contract tests passed\n'

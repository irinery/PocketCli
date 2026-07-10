#!/usr/bin/env sh
set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)

make_install_tree() {
    root=$1
    mkdir -p "$root/scripts/setup"
    cp "$REPO_ROOT/scripts/install_requirements.sh" "$root/scripts/install_requirements.sh"
    cp "$REPO_ROOT/scripts/setup/requirements.yml" "$root/scripts/setup/requirements.yml"
    chmod +x "$root/scripts/install_requirements.sh"
}

run_ansible_preferred_test() {
    WORKDIR=$(mktemp -d)
    APP_DIR="$WORKDIR/app"
    MOCKBIN="$WORKDIR/mockbin"
    LOG_FILE="$WORKDIR/install.log"
    mkdir -p "$MOCKBIN"
    make_install_tree "$APP_DIR"

    cat > "$APP_DIR/scripts/install_deps.sh" <<'EOS'
#!/usr/bin/env sh
set -eu
printf 'fallback:%s:%s\n' "$1" "$2" >> "$POCKETCLI_TEST_LOG"
EOS
    chmod +x "$APP_DIR/scripts/install_deps.sh"

    cat > "$MOCKBIN/ansible-playbook" <<'EOS'
#!/usr/bin/env sh
set -eu
printf 'ansible:%s\n' "$*" >> "$POCKETCLI_TEST_LOG"
EOS
    chmod +x "$MOCKBIN/ansible-playbook"

    env PATH="$MOCKBIN:/usr/bin:/bin" POCKETCLI_TEST_LOG="$LOG_FILE" \
        sh "$APP_DIR/scripts/install_requirements.sh" debian viewer >/tmp/pocketcli-install-req-ansible.out

    grep -F 'ansible:-i localhost, -c local' "$LOG_FILE" >/dev/null 2>&1
    grep -F 'pocketcli_os=debian' "$LOG_FILE" >/dev/null 2>&1
    grep -F 'pocketcli_mode=viewer' "$LOG_FILE" >/dev/null 2>&1
    ! grep -F 'fallback:' "$LOG_FILE" >/dev/null 2>&1
    printf 'PASS install_requirements prefers Ansible when available\n'
}

run_agent_bootstrap_test() {
    WORKDIR=$(mktemp -d)
    APP_DIR="$WORKDIR/app"
    MOCKBIN="$WORKDIR/mockbin"
    LOG_FILE="$WORKDIR/install.log"
    mkdir -p "$MOCKBIN"
    make_install_tree "$APP_DIR"

    cat > "$APP_DIR/scripts/install_deps.sh" <<'EOS'
#!/usr/bin/env sh
set -eu
printf 'fallback:%s:%s\n' "$1" "$2" >> "$POCKETCLI_TEST_LOG"
cat > "$POCKETCLI_TEST_MOCKBIN/ansible-playbook" <<'EOA'
#!/usr/bin/env sh
set -eu
printf 'ansible:%s\n' "$*" >> "$POCKETCLI_TEST_LOG"
EOA
chmod +x "$POCKETCLI_TEST_MOCKBIN/ansible-playbook"
EOS
    chmod +x "$APP_DIR/scripts/install_deps.sh"

    env PATH="$MOCKBIN:/usr/bin:/bin" POCKETCLI_TEST_LOG="$LOG_FILE" POCKETCLI_TEST_MOCKBIN="$MOCKBIN" \
        sh "$APP_DIR/scripts/install_requirements.sh" debian agent >/tmp/pocketcli-install-req-agent.out

    grep -F 'fallback:debian:agent' "$LOG_FILE" >/dev/null 2>&1
    grep -F 'pocketcli_mode=agent' "$LOG_FILE" >/dev/null 2>&1
    printf 'PASS install_requirements bootstraps Ansible before agent playbook\n'
}

run_viewer_no_ansible_runtime_test() {
    WORKDIR=$(mktemp -d)
    APP_DIR="$WORKDIR/app"
    MOCKBIN="$WORKDIR/mockbin"
    LOG_FILE="$WORKDIR/install.log"
    mkdir -p "$MOCKBIN"
    make_install_tree "$APP_DIR"

    cat > "$APP_DIR/scripts/install_deps.sh" <<'EOS'
#!/usr/bin/env sh
set -eu
printf 'fallback:%s:%s\n' "$1" "$2" >> "$POCKETCLI_TEST_LOG"
EOS
    chmod +x "$APP_DIR/scripts/install_deps.sh"

    env PATH="$MOCKBIN:/usr/bin:/bin" POCKETCLI_TEST_LOG="$LOG_FILE" \
        sh "$APP_DIR/scripts/install_requirements.sh" alpine viewer >/tmp/pocketcli-install-req-viewer.out

    grep -F 'fallback:alpine:viewer' "$LOG_FILE" >/dev/null 2>&1
    grep -F 'Ansible is not required at runtime' /tmp/pocketcli-install-req-viewer.out >/dev/null 2>&1
    printf 'PASS install_requirements allows viewer runtime without Ansible\n'
}

run_ansible_preferred_test
run_agent_bootstrap_test
run_viewer_no_ansible_runtime_test

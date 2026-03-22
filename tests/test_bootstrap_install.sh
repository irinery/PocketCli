#!/usr/bin/env sh
# =============================================================================
# PocketCli — tests/test_bootstrap_install.sh
# Validates the bootstrap/download flow with mocked commands and fixture data.
# Run: sh tests/test_bootstrap_install.sh
# =============================================================================

set -eu

REPO_ROOT=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
PASS_COUNT=0
FAIL_COUNT=0

pass() {
    PASS_COUNT=$((PASS_COUNT + 1))
    printf 'PASS %s\n' "$1"
}

fail() {
    FAIL_COUNT=$((FAIL_COUNT + 1))
    printf 'FAIL %s\n' "$1" >&2
}

assert_file_contains() {
    FILE="$1"
    PATTERN="$2"
    MESSAGE="$3"
    if grep -F "$PATTERN" "$FILE" >/dev/null 2>&1; then
        pass "$MESSAGE"
    else
        printf '  expected to find: %s\n' "$PATTERN" >&2
        printf '  in file: %s\n' "$FILE" >&2
        fail "$MESSAGE"
    fi
}

assert_equals() {
    EXPECTED="$1"
    ACTUAL="$2"
    MESSAGE="$3"
    if [ "$EXPECTED" = "$ACTUAL" ]; then
        pass "$MESSAGE"
    else
        printf '  expected: %s\n' "$EXPECTED" >&2
        printf '  actual:   %s\n' "$ACTUAL" >&2
        fail "$MESSAGE"
    fi
}

run_bootstrap_test() {
    TEST_NAME="$1"
    WORKDIR=$(mktemp -d)
    HOME_DIR="$WORKDIR/home"
    MOCKBIN="$WORKDIR/mockbin"
    FIXTURE_REPO="$WORKDIR/fixture-repo"
    LOG_FILE="$WORKDIR/git.log"
    mkdir -p "$HOME_DIR" "$MOCKBIN" "$FIXTURE_REPO/scripts" "$FIXTURE_REPO/config"

    cat > "$FIXTURE_REPO/install.sh" <<'EOS'
#!/usr/bin/env sh
set -eu
printf 'fixture install invoked\n' > "${HOME}/install-ran"
EOS
    chmod +x "$FIXTURE_REPO/install.sh"
    mkdir -p "$FIXTURE_REPO/.git"

    cat > "$MOCKBIN/curl" <<'EOS'
#!/usr/bin/env sh
exit 0
EOS
    chmod +x "$MOCKBIN/curl"

    cat > "$MOCKBIN/git" <<'EOS'
#!/usr/bin/env sh
set -eu
printf '%s\n' "$*" >> "$POCKETCLI_TEST_GIT_LOG"
if [ "$1" = "clone" ]; then
    DEST=${6}
    mkdir -p "$DEST"
    cp -R "$POCKETCLI_TEST_FIXTURE_REPO"/. "$DEST"/
    exit 0
fi
if [ "$1" = "-C" ]; then
    exit 0
fi
printf 'unexpected git invocation: %s\n' "$*" >&2
exit 1
EOS
    chmod +x "$MOCKBIN/git"

    env \
        HOME="$HOME_DIR" \
        SHELL="/bin/sh" \
        PATH="$MOCKBIN:/usr/bin:/bin" \
        POCKETCLI_TEST_FIXTURE_REPO="$FIXTURE_REPO" \
        POCKETCLI_TEST_GIT_LOG="$LOG_FILE" \
        bash "$REPO_ROOT/bootstrap.sh" >/tmp/pocketcli-bootstrap.out 2>/tmp/pocketcli-bootstrap.err

    if [ "$TEST_NAME" = "clone" ]; then
        assert_file_contains "$LOG_FILE" "clone --quiet --branch main https://github.com/irinery/PocketCli.git $HOME_DIR/.pocketcli" "bootstrap faz clone do repositório na primeira execução"
        assert_file_contains "$HOME_DIR/install-ran" "fixture install invoked" "bootstrap executa install.sh após o clone"
    else
        mkdir -p "$HOME_DIR/.pocketcli/.git"
        cat > "$HOME_DIR/.pocketcli/install.sh" <<'EOS'
#!/usr/bin/env sh
set -eu
printf 'update install invoked\n' > "${HOME}/install-ran"
EOS
        chmod +x "$HOME_DIR/.pocketcli/install.sh"
        : > "$LOG_FILE"
        env \
            HOME="$HOME_DIR" \
            SHELL="/bin/sh" \
            PATH="$MOCKBIN:/usr/bin:/bin" \
            POCKETCLI_TEST_FIXTURE_REPO="$FIXTURE_REPO" \
            POCKETCLI_TEST_GIT_LOG="$LOG_FILE" \
            bash "$REPO_ROOT/bootstrap.sh" >/tmp/pocketcli-bootstrap.out 2>/tmp/pocketcli-bootstrap.err

        assert_file_contains "$LOG_FILE" "fetch --quiet origin" "bootstrap faz fetch quando o repositório já existe"
        assert_file_contains "$LOG_FILE" "checkout --quiet main" "bootstrap faz checkout da versão configurada"
        assert_file_contains "$LOG_FILE" "pull --quiet --ff-only" "bootstrap faz pull fast-forward no repositório existente"
        assert_file_contains "$HOME_DIR/install-ran" "update install invoked" "bootstrap executa install.sh após atualização"
    fi
}

prepare_install_fixture() {
    HOME_DIR="$1"
    MOCKBIN="$2"
    INSTALL_DIR="$HOME_DIR/.pocketcli"

    mkdir -p "$HOME_DIR" "$INSTALL_DIR/scripts/lib" "$INSTALL_DIR/profile" "$MOCKBIN"
    cp "$REPO_ROOT/install.sh" "$INSTALL_DIR/install.sh"
    cp "$REPO_ROOT/detect_os.sh" "$INSTALL_DIR/detect_os.sh"

    cat > "$INSTALL_DIR/profile/tmux.conf" <<'EOS'
set -g mouse on
EOS
    cat > "$INSTALL_DIR/profile/starship.toml" <<'EOS'
add_newline = false
EOS
    cat > "$INSTALL_DIR/profile/shellrc" <<'EOS'
alias pocket='pocket'
EOS
    cat > "$INSTALL_DIR/profile/zshrc" <<'EOS'
. "${POCKETCLI_DIR}/profile/shellrc"
setopt SHARE_HISTORY
EOS
    cat > "$INSTALL_DIR/pocket" <<'EOS'
#!/usr/bin/env sh
exit 0
EOS
    chmod +x "$INSTALL_DIR/pocket"

    cat > "$INSTALL_DIR/scripts/install_deps.sh" <<'EOS'
#!/usr/bin/env sh
set -eu
printf 'install_deps:%s:%s\n' "$1" "$2" >> "$POCKETCLI_TEST_LOG"
EOS
    chmod +x "$INSTALL_DIR/scripts/install_deps.sh"

    cat > "$INSTALL_DIR/scripts/tailscale_daemon.sh" <<'EOS'
#!/usr/bin/env sh
set -eu
printf 'tailscale_daemon:%s\n' "$1" >> "$POCKETCLI_TEST_LOG"
EOS
    chmod +x "$INSTALL_DIR/scripts/tailscale_daemon.sh"

    cat > "$INSTALL_DIR/scripts/start_viewer.sh" <<'EOS'
#!/usr/bin/env sh
set -eu
printf 'start_viewer PATH=%s\n' "$PATH" >> "$POCKETCLI_TEST_LOG"
EOS
    chmod +x "$INSTALL_DIR/scripts/start_viewer.sh"

    cat > "$INSTALL_DIR/scripts/start_agent.sh" <<'EOS'
#!/usr/bin/env sh
set -eu
printf 'start_agent PATH=%s\n' "$PATH" >> "$POCKETCLI_TEST_LOG"
EOS
    chmod +x "$INSTALL_DIR/scripts/start_agent.sh"

    cat > "$MOCKBIN/uname" <<'EOS'
#!/usr/bin/env sh
if [ "${1:-}" = "-s" ]; then
    printf 'Linux\n'
else
    printf 'Linux\n'
fi
EOS
    chmod +x "$MOCKBIN/uname"

    cat > "$MOCKBIN/zsh" <<'EOS'
#!/usr/bin/env sh
exit 0
EOS
    chmod +x "$MOCKBIN/zsh"

    cat > "$MOCKBIN/chmod" <<'EOS'
#!/usr/bin/env sh
exit 0
EOS
    chmod +x "$MOCKBIN/chmod"

    cat > "$MOCKBIN/find" <<'EOS'
#!/usr/bin/env sh
exit 0
EOS
    chmod +x "$MOCKBIN/find"
}

run_install_test() {
    WORKDIR=$(mktemp -d)
    HOME_DIR="$WORKDIR/home"
    INSTALL_DIR="$HOME_DIR/.pocketcli"
    MOCKBIN="$WORKDIR/mockbin"
    LOG_FILE="$WORKDIR/install.log"

    prepare_install_fixture "$HOME_DIR" "$MOCKBIN"

    mkdir -p "$HOME_DIR/.config/tmux"
    env \
        HOME="$HOME_DIR" \
        PATH="$MOCKBIN:/usr/bin:/bin" \
        POCKETCLI_MODE_CHOICE="1" \
        POCKETCLI_TEST_LOG="$LOG_FILE" \
        sh "$INSTALL_DIR/install.sh" >/tmp/pocketcli-install.out 2>/tmp/pocketcli-install.err

    assert_file_contains "$LOG_FILE" "install_deps:debian:viewer" "install.sh instala dependências para o modo viewer detectado"
    assert_file_contains "$LOG_FILE" "tailscale_daemon:setup" "install.sh executa o setup do tailscale"
    assert_file_contains "$LOG_FILE" "start_viewer PATH=$INSTALL_DIR:" "install.sh inicia o viewer com PATH já ajustado"
    assert_file_contains "$HOME_DIR/.profile" "export POCKETCLI_DIR=\"$INSTALL_DIR\"" "install.sh injeta POCKETCLI_DIR no perfil"
    assert_file_contains "$HOME_DIR/.profile" "export PATH=\"$INSTALL_DIR:\$PATH\"" "install.sh injeta PATH no perfil"
    assert_file_contains "$HOME_DIR/.bashrc" "export POCKETCLI_DIR=\"$INSTALL_DIR\"" "install.sh injeta POCKETCLI_DIR também no bashrc"
    assert_file_contains "$HOME_DIR/.profile" ". \"$INSTALL_DIR/profile/shellrc\"" "install.sh usa shellrc POSIX no .profile"
    assert_file_contains "$HOME_DIR/.bashrc" ". \"$INSTALL_DIR/profile/shellrc\"" "install.sh usa shellrc POSIX no .bashrc"
    if grep -qF "profile/zshrc" "$HOME_DIR/.profile"; then
        fail "install.sh não deve injetar zshrc dentro do .profile"
    fi
    assert_equals "set -g mouse on" "$(cat "$HOME_DIR/.config/tmux/tmux.conf")" "install.sh copia a configuração do tmux"
    assert_equals "add_newline = false" "$(cat "$HOME_DIR/.config/starship.toml")" "install.sh copia a configuração do starship"
}

run_agent_config_modes_test() {
    WORKDIR=$(mktemp -d)
    HOME_DIR="$WORKDIR/home"
    INSTALL_DIR="$HOME_DIR/.pocketcli"
    MOCKBIN="$WORKDIR/mockbin"
    LOG_FILE="$WORKDIR/install.log"

    prepare_install_fixture "$HOME_DIR" "$MOCKBIN"
    mkdir -p "$HOME_DIR/.config/tmux" "$HOME_DIR/.config"
    cat > "$HOME_DIR/.config/tmux/tmux.conf" <<'EOS'
set -g status off
EOS
    cat > "$HOME_DIR/.config/starship.toml" <<'EOS'
format = "$character"
EOS

    env \
        HOME="$HOME_DIR" \
        PATH="$MOCKBIN:/usr/bin:/bin" \
        POCKETCLI_MODE_CHOICE="2" \
        POCKETCLI_AGENT_CONFIG_CHOICE="3" \
        POCKETCLI_TEST_LOG="$LOG_FILE" \
        sh "$INSTALL_DIR/install.sh" >/tmp/pocketcli-agent-install.out 2>/tmp/pocketcli-agent-install.err

    assert_file_contains "$LOG_FILE" "install_deps:debian:agent" "install.sh instala dependências para o modo agent"
    assert_file_contains "$LOG_FILE" "start_agent PATH=$INSTALL_DIR:" "install.sh inicia o agent com PATH já ajustado"
    assert_file_contains "/tmp/pocketcli-agent-install.out" "Comparing host config with PocketCli project config" "install.sh mostra a comparação entre host e projeto antes da escolha"
    assert_file_contains "/tmp/pocketcli-agent-install.out" "status: different" "install.sh destaca quando encontra diferenças de config"
    assert_equals "set -g status off" "$(cat "$INSTALL_DIR/managed/host/tmux.conf")" "install.sh guarda o tmux original do host"
    assert_equals "set -g mouse on" "$(cat "$INSTALL_DIR/managed/project/tmux.conf")" "install.sh guarda o tmux do projeto"
    assert_file_contains "$HOME_DIR/.profile" "POCKETCLI_CONFIG_MODE=\"project\"" "modo de teste ativa a config do projeto por padrão"
    assert_file_contains "$HOME_DIR/.zshrc" ". \"$INSTALL_DIR/profile/zshrc\"" "install.sh mantém o zshrc apenas no arquivo do Zsh"

    env HOME="$HOME_DIR" PATH="$MOCKBIN:/usr/bin:/bin" sh "$INSTALL_DIR/scripts/switch_config.sh" host >/tmp/pocketcli-switch-host.out 2>/tmp/pocketcli-switch-host.err
    assert_equals "set -g status off" "$(cat "$HOME_DIR/.config/tmux/tmux.conf")" "switch_config.sh restaura o tmux do host"
    assert_equals 'format = "$character"' "$(cat "$HOME_DIR/.config/starship.toml")" "switch_config.sh restaura o starship do host"
    assert_file_contains "$HOME_DIR/.profile" "POCKETCLI_CONFIG_MODE=\"host\"" "switch_config.sh atualiza o modo ativo para host"

    env HOME="$HOME_DIR" PATH="$MOCKBIN:/usr/bin:/bin" sh "$INSTALL_DIR/scripts/switch_config.sh" project >/tmp/pocketcli-switch-project.out 2>/tmp/pocketcli-switch-project.err
    assert_equals "set -g mouse on" "$(cat "$HOME_DIR/.config/tmux/tmux.conf")" "switch_config.sh aplica novamente o tmux do projeto"
    assert_equals "add_newline = false" "$(cat "$HOME_DIR/.config/starship.toml")" "switch_config.sh aplica novamente o starship do projeto"
    assert_file_contains "$HOME_DIR/.profile" "POCKETCLI_CONFIG_MODE=\"project\"" "switch_config.sh atualiza o modo ativo para project"
}

printf '== Testes bootstrap ==\n'
run_bootstrap_test clone
run_bootstrap_test update
printf '\n== Testes install ==\n'
run_install_test
run_agent_config_modes_test

printf '\nResumo: %s passou, %s falhou.\n' "$PASS_COUNT" "$FAIL_COUNT"
[ "$FAIL_COUNT" -eq 0 ]

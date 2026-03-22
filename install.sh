#!/usr/bin/env sh
# =============================================================================
# PocketCli — install.sh
# Orchestrates the full installation flow.
# =============================================================================

set -eu

INSTALL_DIR="${HOME}/.pocketcli"
SCRIPTS_DIR="${INSTALL_DIR}/scripts"
PROFILE_DIR="${INSTALL_DIR}/profile"
MANAGED_DIR="${INSTALL_DIR}/managed"
HOST_CONFIG_DIR="${MANAGED_DIR}/host"
PROJECT_CONFIG_DIR="${MANAGED_DIR}/project"
ACTIVE_CONFIG_DIR="${MANAGED_DIR}/active"
SWITCHER_SCRIPT="${SCRIPTS_DIR}/switch_config.sh"
PROFILE_MARKER_START="# -- PocketCli ------------------------------------------------"
PROFILE_MARKER_END="# --------------------------------------------------------------"

info()    { printf '[PocketCli] %s\n' "$*"; }
success() { printf '[PocketCli] OK: %s\n' "$*"; }
die()     { printf '[PocketCli] ERROR: %s\n' "$*" >&2; exit 1; }

[ -z "${HOME:-}" ] && die "\$HOME is not set."

prompt_choice() {
    VAR_NAME="$1"
    PROMPT="$2"

    case "$VAR_NAME" in
        ''|*[!A-Za-z0-9_]*|[0-9]*)
            die "Invalid variable name for prompt_choice: $VAR_NAME"
        ;;
    esac

    if [ -n "${3:-}" ]; then
        eval "$VAR_NAME=\$3"
    else
        printf '%s' "$PROMPT"
        eval "read -r $VAR_NAME < /dev/tty"
    fi
}

backup_file() {
    SRC="$1"
    DEST="$2"
    mkdir -p "$(dirname "$DEST")"
    if [ -f "$SRC" ]; then
        cp "$SRC" "$DEST"
    else
        : > "$DEST"
    fi
}

copy_if_present() {
    SRC="$1"
    DEST="$2"
    mkdir -p "$(dirname "$DEST")"
    if [ -f "$SRC" ]; then
        cp "$SRC" "$DEST"
    else
        : > "$DEST"
    fi
}

compare_config_file() {
    LABEL="$1"
    HOST_FILE="$2"
    PROJECT_FILE="$3"

    printf '  - %s\n' "$LABEL"
    if [ -f "$HOST_FILE" ] && [ -f "$PROJECT_FILE" ]; then
        if cmp -s "$HOST_FILE" "$PROJECT_FILE"; then
            printf '      status: identical\n'
        else
            printf '      status: different\n'
            if command -v diff >/dev/null 2>&1; then
                diff -u "$HOST_FILE" "$PROJECT_FILE" 2>/dev/null | sed -n '1,12p' | sed 's/^/      /' || true
            fi
        fi
    elif [ -f "$HOST_FILE" ]; then
        printf '      status: host-only\n'
    elif [ -f "$PROJECT_FILE" ]; then
        printf '      status: project-only\n'
    else
        printf '      status: absent on both sides\n'
    fi
}

show_agent_config_comparison() {
    echo ""
    echo "  Comparing host config with PocketCli project config:"
    compare_config_file "tmux" "${HOME}/.config/tmux/tmux.conf" "${PROFILE_DIR}/tmux.conf"
    compare_config_file "starship" "${HOME}/.config/starship.toml" "${PROFILE_DIR}/starship.toml"
    printf '  - shell integration\n'
    if grep -qF "PocketCli" "${HOME}/.profile" 2>/dev/null; then
        printf '      status: host profile already contains PocketCli hooks\n'
    else
        printf '      status: PocketCli will append a managed shell block to .profile/.bashrc'
        command -v zsh >/dev/null 2>&1 && printf '/.zshrc'
        printf '\n'
    fi
    echo ""
    echo "  Choose after reviewing the differences above."
}

write_profile_managed_block() {
    RC="$1"
    MODE_NAME="$2"
    mkdir -p "$(dirname "$RC")"
    [ -f "$RC" ] || : > "$RC"
    TMP_FILE=$(mktemp)
    awk -v start="$PROFILE_MARKER_START" -v end="$PROFILE_MARKER_END" '
        $0 == start { skip=1; next }
        $0 == end { skip=0; next }
        skip != 1 { print }
    ' "$RC" > "$TMP_FILE"
    mv "$TMP_FILE" "$RC"
    {
        printf '\n%s\n' "$PROFILE_MARKER_START"
        printf 'export POCKETCLI_DIR="%s"\n' "$INSTALL_DIR"
        printf 'export PATH="%s:$PATH"\n' "$INSTALL_DIR"
        printf 'export POCKETCLI_CONFIG_MODE="%s"\n' "$MODE_NAME"
        case "$(basename "$RC")" in
            .zshrc)
                printf '. "%s/profile/zshrc"\n' "$INSTALL_DIR"
                ;;
            *)
                printf '. "%s/profile/shellrc"\n' "$INSTALL_DIR"
                ;;
        esac
        printf '%s\n' "$PROFILE_MARKER_END"
    } >> "$RC"
    info "PocketCli block updated in ${RC}"
}

write_switcher_script() {
    cat > "$SWITCHER_SCRIPT" <<EOF2
#!/usr/bin/env sh
set -eu
INSTALL_DIR="${INSTALL_DIR}"
PROFILE_DIR="${PROFILE_DIR}"
MANAGED_DIR="${MANAGED_DIR}"
HOST_CONFIG_DIR="${HOST_CONFIG_DIR}"
PROJECT_CONFIG_DIR="${PROJECT_CONFIG_DIR}"
ACTIVE_CONFIG_DIR="${ACTIVE_CONFIG_DIR}"
PROFILE_MARKER_START="${PROFILE_MARKER_START}"
PROFILE_MARKER_END="${PROFILE_MARKER_END}"

info() { printf '[PocketCli] %s\n' "\$*"; }
die()  { printf '[PocketCli] ERROR: %s\n' "\$*" >&2; exit 1; }

copy_if_present() {
    SRC="\$1"
    DEST="\$2"
    mkdir -p "\$(dirname "\$DEST")"
    if [ -f "\$SRC" ]; then
        cp "\$SRC" "\$DEST"
    else
        : > "\$DEST"
    fi
}

write_profile_managed_block() {
    RC="\$1"
    MODE_NAME="\$2"
    mkdir -p "\$(dirname "\$RC")"
    [ -f "\$RC" ] || : > "\$RC"
    TMP_FILE=\$(mktemp)
    awk -v start="\$PROFILE_MARKER_START" -v end="\$PROFILE_MARKER_END" '
        \$0 == start { skip=1; next }
        \$0 == end { skip=0; next }
        skip != 1 { print }
    ' "\$RC" > "\$TMP_FILE"
    mv "\$TMP_FILE" "\$RC"
    {
        printf '\n%s\n' "\$PROFILE_MARKER_START"
        printf 'export POCKETCLI_DIR="%s"\n' "\$INSTALL_DIR"
        printf 'export PATH="%s:$PATH"\n' "\$INSTALL_DIR"
        printf 'export POCKETCLI_CONFIG_MODE="%s"\n' "\$MODE_NAME"
        case "\$(basename "\$RC")" in
            .zshrc)
                printf '. "%s/profile/zshrc"\n' "\$INSTALL_DIR"
                ;;
            *)
                printf '. "%s/profile/shellrc"\n' "\$INSTALL_DIR"
                ;;
        esac
        printf '%s\n' "\$PROFILE_MARKER_END"
    } >> "\$RC"
}

apply_mode() {
    MODE_NAME="\$1"
    case "\$MODE_NAME" in
        host|project) ;;
        *) die "Unknown mode '\$MODE_NAME'. Use: host or project" ;;
    esac

    SOURCE_DIR="\$MANAGED_DIR/\$MODE_NAME"
    [ -d "\$SOURCE_DIR" ] || die "Managed config directory not found: \$SOURCE_DIR"

    copy_if_present "\$SOURCE_DIR/tmux.conf" "\$HOME/.config/tmux/tmux.conf"
    copy_if_present "\$SOURCE_DIR/starship.toml" "\$HOME/.config/starship.toml"
    copy_if_present "\$SOURCE_DIR/profile" "\$ACTIVE_CONFIG_DIR/profile.active"

    write_profile_managed_block "\$HOME/.profile" "\$MODE_NAME"
    write_profile_managed_block "\$HOME/.bashrc" "\$MODE_NAME"
    if command -v zsh >/dev/null 2>&1; then
        write_profile_managed_block "\$HOME/.zshrc" "\$MODE_NAME"
    fi

    info "Config mode active: \$MODE_NAME"
    info "Reload with: . ~/.profile"
}

apply_mode "\${1:-project}"
EOF2
    chmod 700 "$SWITCHER_SCRIPT"
}

# ---------------------------------------------------------------------------
# Detect OS — source the script (POSIX . instead of bash source)
# ---------------------------------------------------------------------------
. "${INSTALL_DIR}/detect_os.sh"
detect_os
info "Detected OS: ${OS}"

# ---------------------------------------------------------------------------
# Choose install mode
# ---------------------------------------------------------------------------
echo ""
echo "  Select install mode:"
echo ""
echo "    1) Viewer  ->  iPad or lightweight terminal (SSH client only)"
echo "    2) Agent   ->  full environment on the machine"
echo ""

if [ -n "${POCKETCLI_MODE_CHOICE:-}" ]; then
    MODE_CHOICE="${POCKETCLI_MODE_CHOICE}"
else
    prompt_choice MODE_CHOICE "  Choice [1/2]: "
fi

action_mode=""
case "$MODE_CHOICE" in
    1) action_mode="viewer" ;;
    2) action_mode="agent" ;;
    *) die "Invalid choice. Use 1 or 2." ;;
esac

info "Selected mode: ${action_mode}"

# Install deps
PATH="${INSTALL_DIR}:${PATH}"; export PATH
sh "${SCRIPTS_DIR}/install_deps.sh" "${OS}" "${action_mode}"

# Tailscale setup only if not iSH? external script handles it
sh "${SCRIPTS_DIR}/tailscale_daemon.sh" setup || true

mkdir -p "${HOME}/.config/tmux" "${HOME}/.config" "${MANAGED_DIR}" "${ACTIVE_CONFIG_DIR}"

# Snapshot current host and project-managed files
backup_file "${HOME}/.config/tmux/tmux.conf" "${HOST_CONFIG_DIR}/tmux.conf"
backup_file "${HOME}/.config/starship.toml" "${HOST_CONFIG_DIR}/starship.toml"
backup_file "${HOME}/.profile" "${HOST_CONFIG_DIR}/profile"

copy_if_present "${PROFILE_DIR}/tmux.conf" "${PROJECT_CONFIG_DIR}/tmux.conf"
copy_if_present "${PROFILE_DIR}/starship.toml" "${PROJECT_CONFIG_DIR}/starship.toml"
copy_if_present "${HOME}/.profile" "${PROJECT_CONFIG_DIR}/profile"

write_switcher_script

if [ "${action_mode}" = "agent" ]; then
    show_agent_config_comparison
    echo "    1) Keep host config        -> preserve current tmux/starship/profile behavior"
    echo "    2) Apply project config    -> use PocketCli profile defaults immediately"
    echo "    3) Test both (recommended) -> keep both and enable quick switching"
    echo ""

    if [ -n "${POCKETCLI_AGENT_CONFIG_CHOICE:-}" ]; then
        AGENT_CONFIG_CHOICE="${POCKETCLI_AGENT_CONFIG_CHOICE}"
    else
        prompt_choice AGENT_CONFIG_CHOICE "  Choice [1/2/3]: "
    fi

    case "$AGENT_CONFIG_CHOICE" in
        1)
            write_profile_managed_block "${HOME}/.profile" "host"
            write_profile_managed_block "${HOME}/.bashrc" "host"
            command -v zsh >/dev/null 2>&1 && write_profile_managed_block "${HOME}/.zshrc" "host" || true
        ;;
        2|3)
            cp "${PROFILE_DIR}/tmux.conf" "${HOME}/.config/tmux/tmux.conf"
            cp "${PROFILE_DIR}/starship.toml" "${HOME}/.config/starship.toml"
            write_profile_managed_block "${HOME}/.profile" "project"
            write_profile_managed_block "${HOME}/.bashrc" "project"
            command -v zsh >/dev/null 2>&1 && write_profile_managed_block "${HOME}/.zshrc" "project" || true
        ;;
        *) die "Invalid choice. Use 1, 2 or 3." ;;
    esac
else
    cp "${PROFILE_DIR}/tmux.conf" "${HOME}/.config/tmux/tmux.conf"
    cp "${PROFILE_DIR}/starship.toml" "${HOME}/.config/starship.toml"
    write_profile_managed_block "${HOME}/.profile" "project"
    write_profile_managed_block "${HOME}/.bashrc" "project"
    command -v zsh >/dev/null 2>&1 && write_profile_managed_block "${HOME}/.zshrc" "project" || true
fi

if [ "${action_mode}" = "viewer" ]; then
    sh "${SCRIPTS_DIR}/start_viewer.sh"
else
    sh "${SCRIPTS_DIR}/start_agent.sh"
fi

success "Installation completed."
printf '    Reload your shell with:\n'
printf '    . ~/.profile\n'

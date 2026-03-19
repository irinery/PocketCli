#!/usr/bin/env sh
# =============================================================================
# PocketCli — install.sh
# Orchestrates the full installation flow.
# =============================================================================

set -eu

INSTALL_DIR="${HOME}/.pocketcli"
SCRIPTS_DIR="${INSTALL_DIR}/scripts"
CONFIG_DIR="${INSTALL_DIR}/config"
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
    if [ -n "${3:-}" ]; then
        eval "$VAR_NAME=\$3"
    else
        printf '%s' "$PROMPT"
        read -r "$VAR_NAME" < /dev/tty
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
    compare_config_file "tmux" "${HOME}/.config/tmux/tmux.conf" "${CONFIG_DIR}/tmux.conf"
    compare_config_file "starship" "${HOME}/.config/starship.toml" "${CONFIG_DIR}/starship.toml"
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
        printf '. "%s/config/zshrc"\n' "$INSTALL_DIR"
        printf '%s\n' "$PROFILE_MARKER_END"
    } >> "$RC"
    info "PocketCli block updated in ${RC}"
}

write_switcher_script() {
    cat > "$SWITCHER_SCRIPT" <<EOF2
#!/usr/bin/env sh
set -eu
INSTALL_DIR="${INSTALL_DIR}"
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
        printf '. "%s/config/zshrc"\n' "\$INSTALL_DIR"
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
echo "    2) Agent   ->  server or remote machine (full environment)"
echo ""
prompt_choice MODE_CHOICE "  Choice [1/2]: " "${POCKETCLI_MODE_CHOICE:-}"

case "${MODE_CHOICE}" in
    1) MODE="viewer" ;;
    2) MODE="agent"  ;;
    *) die "Invalid choice '${MODE_CHOICE}'. Re-run the installer." ;;
esac

info "Mode: ${MODE}"
CONFIG_MODE="project"
if [ "${MODE}" = "agent" ]; then
    show_agent_config_comparison
    echo ""
    echo "  Agent config mode:"
    echo ""
    echo "    1) Keep host config        -> preserve current tmux/starship/profile behavior"
    echo "    2) Use project config      -> apply PocketCli config from this repository"
    echo "    3) Test original mode      -> keep both configs and enable automated switching"
    echo ""
    prompt_choice CONFIG_CHOICE "  Choice [1/2/3]: " "${POCKETCLI_AGENT_CONFIG_CHOICE:-}"
    case "${CONFIG_CHOICE}" in
        1) CONFIG_MODE="host" ;;
        2) CONFIG_MODE="project" ;;
        3) CONFIG_MODE="test-original" ;;
        *) die "Invalid config choice '${CONFIG_CHOICE}'. Re-run the installer." ;;
    esac
    info "Agent config mode: ${CONFIG_MODE}"
fi

# ---------------------------------------------------------------------------
# Install dependencies
# ---------------------------------------------------------------------------
sh "${SCRIPTS_DIR}/install_deps.sh" "${OS}" "${MODE}"

# ---------------------------------------------------------------------------
# Install Tailscale
# ---------------------------------------------------------------------------
sh "${SCRIPTS_DIR}/tailscale_daemon.sh" setup

# ---------------------------------------------------------------------------
# Apply config files
# ---------------------------------------------------------------------------
info "Applying configuration files..."
mkdir -p "${HOST_CONFIG_DIR}" "${PROJECT_CONFIG_DIR}" "${ACTIVE_CONFIG_DIR}" "${HOME}/.config/tmux" "${HOME}/.config"

backup_file "${HOME}/.config/tmux/tmux.conf" "${HOST_CONFIG_DIR}/tmux.conf"
backup_file "${HOME}/.config/starship.toml" "${HOST_CONFIG_DIR}/starship.toml"
backup_file "${HOME}/.profile" "${HOST_CONFIG_DIR}/profile"
backup_file "${HOME}/.bashrc" "${HOST_CONFIG_DIR}/bashrc"
backup_file "${HOME}/.zshrc" "${HOST_CONFIG_DIR}/zshrc"

copy_if_present "${CONFIG_DIR}/tmux.conf" "${PROJECT_CONFIG_DIR}/tmux.conf"
copy_if_present "${CONFIG_DIR}/starship.toml" "${PROJECT_CONFIG_DIR}/starship.toml"
copy_if_present "${CONFIG_DIR}/zshrc" "${PROJECT_CONFIG_DIR}/zshrc"
copy_if_present "${HOME}/.profile" "${PROJECT_CONFIG_DIR}/profile"
copy_if_present "${HOME}/.bashrc" "${PROJECT_CONFIG_DIR}/bashrc"
copy_if_present "${HOME}/.zshrc" "${PROJECT_CONFIG_DIR}/zshrc"

write_switcher_script

case "${CONFIG_MODE}" in
    host)
        sh "${SWITCHER_SCRIPT}" host
        ;;
    project)
        cp "${CONFIG_DIR}/tmux.conf" "${HOME}/.config/tmux/tmux.conf"
        cp "${CONFIG_DIR}/starship.toml" "${HOME}/.config/starship.toml"
        write_profile_managed_block "${HOME}/.profile" "project"
        write_profile_managed_block "${HOME}/.bashrc" "project"
        command -v zsh >/dev/null 2>&1 && write_profile_managed_block "${HOME}/.zshrc" "project" || true
        ;;
    test-original)
        cp "${CONFIG_DIR}/tmux.conf" "${HOME}/.config/tmux/tmux.conf"
        cp "${CONFIG_DIR}/starship.toml" "${HOME}/.config/starship.toml"
        write_profile_managed_block "${HOME}/.profile" "project"
        write_profile_managed_block "${HOME}/.bashrc" "project"
        command -v zsh >/dev/null 2>&1 && write_profile_managed_block "${HOME}/.zshrc" "project" || true
        info "Test mode enabled. Switch anytime with:"
        info "  sh ${SWITCHER_SCRIPT} host"
        info "  sh ${SWITCHER_SCRIPT} project"
        ;;
esac

success "Config files applied."

# Apply right now for this session (don't require shell restart)
export PATH="${INSTALL_DIR}:${PATH}"

# Make pocket binary executable
chmod 700 "${INSTALL_DIR}/pocket"

# ---------------------------------------------------------------------------
# Harden permissions
# ---------------------------------------------------------------------------
info "Hardening permissions..."
chmod -R o-rwx "${INSTALL_DIR}"
find "${INSTALL_DIR}" -name "*.sh" -exec chmod 700 {} \;
success "Permissions hardened."

# ---------------------------------------------------------------------------
# Post-install tip
# ---------------------------------------------------------------------------
echo ""
echo "  ================================="
success "PocketCli installed!"
echo "  ================================="
echo ""
echo "  PATH already active in this session."
echo "  For new shells, run:"
echo ""
printf '    . ~/.profile\n'
echo ""
if [ "${CONFIG_MODE}" = "test-original" ]; then
    echo "  Config switcher available:"
    printf '    sh %s host\n' "${SWITCHER_SCRIPT}"
    printf '    sh %s project\n' "${SWITCHER_SCRIPT}"
    echo ""
fi
echo "  Then use:  pocket help"
echo ""

# ---------------------------------------------------------------------------
# Start environment
# ---------------------------------------------------------------------------
case "${MODE}" in
    viewer) exec env PATH="${INSTALL_DIR}:${PATH}" sh "${SCRIPTS_DIR}/start_viewer.sh" ;;
    agent)  exec env PATH="${INSTALL_DIR}:${PATH}" sh "${SCRIPTS_DIR}/start_agent.sh"  ;;
esac

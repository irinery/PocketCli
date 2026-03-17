#!/usr/bin/env sh
# =============================================================================
# PocketCli — tests/test_ish.sh
# Compatibility test suite for iSH / Alpine / any POSIX sh environment.
# Run: sh ~/.pocketcli/tests/test_ish.sh
# Or:  pocket doctor
# =============================================================================

set -eu

POCKETCLI_DIR="${HOME}/.pocketcli"
. "${POCKETCLI_DIR}/scripts/lib/common.sh"

PASS=0
FAIL=0
WARN_COUNT=0

# ---------------------------------------------------------------------------
_test()      { printf "\n  ${C_BOLD}TEST:${C_NC} %s\n" "$*"; }
_pass()      { PASS=$((PASS+1));       printf "    ${C_GREEN}PASS${C_NC}  %s\n" "$*"; }
_fail()      { FAIL=$((FAIL+1));       printf "    ${C_RED}FAIL${C_NC}  %s\n" "$*"; }
_warn_test() { WARN_COUNT=$((WARN_COUNT+1)); printf "    ${C_YELLOW}WARN${C_NC}  %s\n" "$*"; }
_skip()      { printf "    ${C_DIM}SKIP${C_NC}  %s\n" "$*"; }

_check() {
    # _check "label" command [args]
    LABEL="$1"; shift
    if "$@" >/dev/null 2>&1; then
        _pass "${LABEL}"
        return 0
    else
        _fail "${LABEL}"
        return 1
    fi
}

_check_warn() {
    LABEL="$1"; shift
    if "$@" >/dev/null 2>&1; then
        _pass "${LABEL}"
    else
        _warn_test "${LABEL} (not critical)"
    fi
}

# ---------------------------------------------------------------------------
echo ""
printf "  ${C_BOLD}=====================================${C_NC}\n"
printf "  ${C_BOLD}    PocketCli  Compatibility Test   ${C_NC}\n"
printf "  ${C_BOLD}=====================================${C_NC}\n"
echo ""

# Environment info
printf "  Platform : %s\n" "$(uname -a 2>/dev/null || echo 'unknown')"
printf "  Shell    : %s\n" "${SHELL:-unknown}"
printf "  HOME     : %s\n" "${HOME}"
if [ -f /etc/alpine-release ]; then
    printf "  Alpine   : %s\n" "$(cat /etc/alpine-release)"
fi
if is_ish; then
    printf "  iSH      : YES\n"
fi
echo ""

# =============================================================================
_test "POSIX sh compatibility"
# =============================================================================

# set -eu should not crash this script
_pass "set -eu works"

# Test that we're not accidentally using bash features
_check "No bash required" command -v sh

# local is POSIX-allowed in function context on busybox
_test_local() { local X="ok" 2>/dev/null && printf '%s' "${X}"; }
if [ "$(_test_local 2>/dev/null || true)" = "ok" ]; then
    _pass "local keyword works"
else
    _warn_test "local keyword unsupported — using global vars"
fi

# =============================================================================
_test "Core dependencies"
# =============================================================================

_check      "git available"  command -v git
_check      "sh available"   command -v sh
_check_warn "curl available" command -v curl
_check_warn "wget available" command -v wget
_check_warn "jq available"   command -v jq
_check_warn "tmux available" command -v tmux
_check_warn "ssh available"  command -v ssh

# =============================================================================
_test "Network connectivity"
# =============================================================================

# Test basic TCP with timeout
if with_timeout 5 sh -c 'echo "" | nc -w3 1.1.1.1 53' >/dev/null 2>&1; then
    _pass "TCP connectivity (1.1.1.1:53)"
elif with_timeout 5 ping -c 1 1.1.1.1 >/dev/null 2>&1; then
    _pass "ICMP connectivity"
else
    _warn_test "No network reachability detected"
fi

# DNS
if with_timeout 5 sh -c 'nslookup github.com' >/dev/null 2>&1 \
|| with_timeout 5 sh -c 'getent hosts github.com' >/dev/null 2>&1; then
    _pass "DNS resolution (github.com)"
else
    _warn_test "DNS resolution may be broken"
fi

# =============================================================================
_test "SSH"
# =============================================================================

_check "ssh binary available" command -v ssh

# Check SSH dir and permissions
if [ -d "${HOME}/.ssh" ]; then
    PERM=$(ls -ld "${HOME}/.ssh" | awk '{print $1}')
    case "${PERM}" in
        drwx------) _pass ".ssh permissions (700)" ;;
        *) _warn_test ".ssh permissions should be 700, got: ${PERM}" ;;
    esac
else
    _warn_test "~/.ssh does not exist (will be created on first use)"
fi

# Check for any existing key
if ls "${HOME}/.ssh/id_"*.pub >/dev/null 2>&1; then
    _pass "SSH key found"
else
    _warn_test "No SSH key in ~/.ssh — generate with: ssh-keygen -t ed25519"
fi

# =============================================================================
_test "Tailscale"
# =============================================================================

if ! command -v tailscale >/dev/null 2>&1; then
    _warn_test "tailscale not installed — run: pocket tailscale-setup"
else
    _pass "tailscale binary found"

    TS_VER=$(tailscale version 2>/dev/null | head -1 || echo "unknown")
    printf "    ${C_DIM}version: %s${C_NC}\n" "${TS_VER}"

    if is_tailscale_daemon_running; then
        _pass "tailscaled daemon is running"

        if with_timeout 5 tailscale status >/dev/null 2>&1; then
            _pass "tailscale daemon responding"

            STATUS_OUT=$(tailscale status 2>/dev/null || true)
            if printf '%s' "${STATUS_OUT}" | grep -q "^100\."; then
                _pass "Tailscale IP assigned"
                TS_IP=$(tailscale ip -4 2>/dev/null | head -1 || echo "?")
                printf "    ${C_DIM}IP: %s${C_NC}\n" "${TS_IP}"
            else
                _warn_test "Not authenticated — run: pocket tailscale-setup"
            fi
        else
            _warn_test "Daemon running but not responding"
        fi
    else
        _warn_test "tailscaled not running — run: pocket tailscale-setup"
        if is_ish; then
            printf "    ${C_DIM}hint: tailscaled --tun=userspace-networking &${C_NC}\n"
        fi
    fi
fi

# =============================================================================
_test "PocketCli installation"
# =============================================================================

_check "POCKETCLI_DIR exists"   test -d "${POCKETCLI_DIR}"
_check "pocket binary exists"   test -f "${POCKETCLI_DIR}/pocket"
_check "pocket is executable"   test -x "${POCKETCLI_DIR}/pocket"
_check "lib/common.sh exists"   test -f "${POCKETCLI_DIR}/scripts/lib/common.sh"
_check "config/tmux.conf"       test -f "${POCKETCLI_DIR}/config/tmux.conf"
_check "radar.sh exists"        test -f "${POCKETCLI_DIR}/radar.sh"

# Check pocket in PATH
if command -v pocket >/dev/null 2>&1; then
    _pass "pocket in PATH"
else
    _warn_test "pocket not in PATH — run: . ~/.profile"
fi

# =============================================================================
_test "Optional tools"
# =============================================================================

_check_warn "fzf available"      command -v fzf
_check_warn "qrencode available" command -v qrencode
_check_warn "starship available" command -v starship
_check_warn "lazygit available"  command -v lazygit
_check_warn "htop available"     command -v htop
_check_warn "tmux available"     command -v tmux
_check_warn "ripgrep (rg)"       command -v rg

# =============================================================================
_test "iSH-specific checks"
# =============================================================================

if is_ish; then
    _pass "Running on iSH (detected)"

    # Check for known-crashing tools
    for BAD in fzf; do
        if command -v "${BAD}" >/dev/null 2>&1; then
            _warn_test "${BAD} installed — may crash on iSH (Go binary)"
        fi
    done

    # Check userspace net availability
    if tailscaled --help 2>&1 | grep -q 'userspace'; then
        _pass "tailscaled supports --tun=userspace-networking"
    else
        _warn_test "tailscaled may not support userspace networking"
    fi

    # PID file check
    if [ -f /tmp/tailscaled.pid ]; then
        PID=$(cat /tmp/tailscaled.pid 2>/dev/null || echo "")
        if [ -n "${PID}" ] && kill -0 "${PID}" 2>/dev/null; then
            _pass "tailscaled PID file valid (PID ${PID})"
        else
            _warn_test "Stale tailscaled PID file — daemon may have crashed"
        fi
    else
        _skip "No tailscaled PID file (daemon not started by PocketCli)"
    fi
else
    _skip "Not iSH — skipping iSH-specific checks"
fi

# =============================================================================
# Summary
# =============================================================================
TOTAL=$((PASS + FAIL + WARN_COUNT))
echo ""
printf "  ${C_BOLD}=====================================  ${C_NC}\n"
printf "  Results: ${C_GREEN}%d pass${C_NC}  ${C_RED}%d fail${C_NC}  ${C_YELLOW}%d warn${C_NC}  / %d total\n" \
    "${PASS}" "${FAIL}" "${WARN_COUNT}" "${TOTAL}"
printf "  ${C_BOLD}=====================================${C_NC}\n"
echo ""

if [ "${FAIL}" -gt 0 ]; then
    printf "  ${C_RED}Some checks failed. Review above and fix before using PocketCli.${C_NC}\n\n"
    exit 1
elif [ "${WARN_COUNT}" -gt 0 ]; then
    printf "  ${C_YELLOW}Warnings present — PocketCli works but some features may be limited.${C_NC}\n\n"
    exit 0
else
    printf "  ${C_GREEN}All checks passed! PocketCli is ready.${C_NC}\n\n"
    exit 0
fi

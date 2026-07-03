package tailscale

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

var ErrCLIUnavailable = errors.New("tailscale CLI unavailable")

// CLIPath discovers both normal PATH installs and native macOS/Windows apps.
// POCKETCLI_TAILSCALE_CLI is authoritative when set, matching the shell CLI.
func CLIPath(lookPath func(string) (string, error)) (string, error) {
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	if override := strings.TrimSpace(os.Getenv("POCKETCLI_TAILSCALE_CLI")); override != "" {
		return resolveCLI(override, lookPath)
	}

	for _, name := range []string{"tailscale", "tailscale.exe"} {
		if _, err := lookPath(name); err == nil {
			return name, nil
		}
	}

	candidates := []string{
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
		"/mnt/c/Program Files/Tailscale/tailscale.exe",
		"/c/Program Files/Tailscale/tailscale.exe",
		"/cygdrive/c/Program Files/Tailscale/tailscale.exe",
	}
	if programFiles := strings.TrimSpace(os.Getenv("ProgramFiles")); programFiles != "" {
		candidates = append(candidates, filepath.Join(programFiles, "Tailscale", "tailscale.exe"))
	}
	if localAppData := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); localAppData != "" {
		candidates = append(candidates, filepath.Join(localAppData, "Tailscale", "tailscale.exe"))
	}

	for _, candidate := range candidates {
		if executableFile(candidate) {
			return candidate, nil
		}
	}
	return "", ErrCLIUnavailable
}

func resolveCLI(candidate string, lookPath func(string) (string, error)) (string, error) {
	if strings.ContainsAny(candidate, `/\\`) {
		if executableFile(candidate) {
			return candidate, nil
		}
		return "", ErrCLIUnavailable
	}
	if _, err := lookPath(candidate); err == nil {
		return candidate, nil
	}
	return "", ErrCLIUnavailable
}

func executableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0
}

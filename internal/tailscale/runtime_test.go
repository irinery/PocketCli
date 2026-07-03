package tailscale

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCLIPathUsesExplicitNativeOverrideOutsidePath(t *testing.T) {
	cli := filepath.Join(t.TempDir(), "tailscale.exe")
	if err := os.WriteFile(cli, []byte("stub"), 0o755); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("POCKETCLI_TAILSCALE_CLI", cli)

	got, err := CLIPath(func(string) (string, error) { return "", errors.New("not in PATH") })
	if err != nil {
		t.Fatalf("CLIPath returned error: %v", err)
	}
	if got != cli {
		t.Fatalf("CLIPath = %q, want %q", got, cli)
	}
}

func TestCLIPathTreatsInvalidOverrideAsAuthoritative(t *testing.T) {
	t.Setenv("POCKETCLI_TAILSCALE_CLI", filepath.Join(t.TempDir(), "missing"))

	_, err := CLIPath(func(string) (string, error) { return "/usr/bin/tailscale", nil })
	if !errors.Is(err, ErrCLIUnavailable) {
		t.Fatalf("CLIPath error = %v, want ErrCLIUnavailable", err)
	}
}

func TestCLIPathKeepsPathCommandName(t *testing.T) {
	t.Setenv("POCKETCLI_TAILSCALE_CLI", "")
	got, err := CLIPath(func(name string) (string, error) {
		if name == "tailscale" {
			return "/usr/bin/tailscale", nil
		}
		return "", errors.New("missing")
	})
	if err != nil {
		t.Fatalf("CLIPath returned error: %v", err)
	}
	if got != "tailscale" {
		t.Fatalf("CLIPath = %q, want tailscale", got)
	}
}

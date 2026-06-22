package insights

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"pocketcli/internal/ledger"
)

func TestComputeDetectsHostTimeouts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	store, err := ledger.NewStore()
	if err != nil {
		t.Fatalf("NewStore returned error: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := store.Append(ledger.Event{
			Type:      ledger.EventSSHProbe,
			Timestamp: time.Now().UTC().Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
			SessionID: "s1",
			HostID:    "host-a",
			Status:    "timeout",
			Payload:   ledger.Payload{Message: "[REDACTED]"},
		}); err != nil {
			t.Fatalf("Append returned error: %v", err)
		}
	}
	result, err := Compute(Request{Scope: "active", HostID: "host-a", TimeWindowMinutes: 60})
	if err != nil {
		t.Fatalf("Compute returned error: %v", err)
	}
	found := false
	for _, item := range result.Insights {
		if item.Kind == "host_unstable" && item.Severity == "warning" {
			found = true
			if item.Evidence[0].Preview != "[REDACTED]" {
				t.Fatalf("expected redacted preview, got %#v", item.Evidence)
			}
		}
	}
	if !found {
		t.Fatalf("expected host_unstable insight, got %#v", result)
	}
}

func TestComputeDetectsMissingTmuxForAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("POCKETCLI_MODE_REQUESTED", "agent")
	bin := t.TempDir()
	for _, name := range []string{"ssh", "scp"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("WriteFile returned error: %v", err)
		}
	}
	t.Setenv("PATH", bin)
	result, err := Compute(Request{Scope: "active", TimeWindowMinutes: 60})
	if err != nil {
		t.Fatalf("Compute returned error: %v", err)
	}
	for _, item := range result.Insights {
		if item.Kind == "missing_capability" && item.RecommendedAction == "install_tmux_or_use_viewer" {
			return
		}
	}
	t.Fatalf("expected missing tmux insight, got %#v", result)
}

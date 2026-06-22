package capabilities

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectReportsMissingTailscaleAndWritesNoSecrets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	bin := t.TempDir()
	for _, name := range []string{"ssh", "scp", "git"} {
		path := filepath.Join(bin, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("WriteFile returned error: %v", err)
		}
	}
	t.Setenv("PATH", bin)
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".env"), []byte("TOKEN=secret\n"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	manifest, err := Detect("")
	if err != nil {
		t.Fatalf("Detect returned error: %v", err)
	}
	if manifest.SchemaVersion != 1 {
		t.Fatalf("unexpected schema: %d", manifest.SchemaVersion)
	}
	if !manifest.Capabilities.HasSSH {
		t.Fatal("expected has_ssh=true")
	}
	if manifest.Capabilities.HasTailscale {
		t.Fatal("expected has_tailscale=false")
	}
	if !containsReason(manifest.DegradationReasons, "tailscale_missing") {
		t.Fatalf("expected tailscale_missing, got %#v", manifest.DegradationReasons)
	}
}

func TestResolveLayoutCompactAtFortyColumns(t *testing.T) {
	if got := resolveLayout(true, 40); got != LayoutCompact {
		t.Fatalf("expected compact, got %q", got)
	}
}

func containsReason(reasons []string, wanted string) bool {
	for _, reason := range reasons {
		if reason == wanted {
			return true
		}
	}
	return false
}

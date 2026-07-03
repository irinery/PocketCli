package main

import (
	"os"
	"strings"
	"testing"
)

func TestCatalogueListCommandShowsBuiltinRecipes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := newCatalogueListCommand()
	out, err := captureCommandOutput(t, cmd, []string{"--category", "ssh"})
	if err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}
	if !strings.Contains(out, "ssh.public-key") {
		t.Fatalf("expected ssh.public-key in output, got %q", out)
	}
}

func TestCatalogueRunDestructiveRequiresApply(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := newCatalogueRunCommand()
	out, err := captureCommandOutput(t, cmd, []string{"git.reset-hard"})
	if err == nil {
		t.Fatalf("expected apply-required error")
	}
	if !strings.Contains(err.Error(), "ERR_APPLY_REQUIRED") {
		t.Fatalf("expected ERR_APPLY_REQUIRED, got %v", err)
	}
	if !strings.Contains(out, "dry-run") {
		t.Fatalf("expected dry-run output, got %q", out)
	}
}

func TestRootDispatchCatalogueAlias(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	withArgs(t, []string{"pocket", "cmd", "search", "tailscale"})
	root := newRootCommand()
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
}

func TestSSHForwardDryRunRendersCommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cmd := newSSHForwardCommand("ssh.forward", false)
	out, err := captureCommandOutput(t, cmd, []string{"pocket-dev", "49152", "--dry-run"})
	if err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}
	if !strings.Contains(out, "127.0.0.1:49152:127.0.0.1:49152") {
		t.Fatalf("expected forward mapping, got %q", out)
	}
}

func TestCatalogueDocsBlocksOverwriteWithoutFlag(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	oldCwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldCwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir: %v", err)
	}
	if err := os.MkdirAll("docs/generated", 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile("docs/generated/catalogue.md", []byte("old"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	cmd := newCatalogueDocsCommand()
	_, err := captureCommandOutput(t, cmd, []string{"--output", "docs/generated/catalogue.md"})
	if err == nil || !strings.Contains(err.Error(), "ERR_OUTPUT_EXISTS_OVERWRITE_REQUIRED") {
		t.Fatalf("expected overwrite error, got %v", err)
	}
}

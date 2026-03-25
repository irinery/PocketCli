package main

import (
	"errors"
	"os"
	"testing"
)

func withArgs(t *testing.T, args []string) {
	t.Helper()
	orig := os.Args
	os.Args = append([]string(nil), args...)
	t.Cleanup(func() { os.Args = orig })
}

func TestRootCommand_NoSubcommandShowsHelpWithoutError(t *testing.T) {
	root := newRootCommand()
	withArgs(t, []string{"pocket"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
}

func TestSSHCommand_PropagatesError(t *testing.T) {
	orig := openSSH
	t.Cleanup(func() { openSSH = orig })

	expected := errors.New("boom")
	openSSH = func(host string) error { return expected }

	cmd := newSSHCommand()
	err := cmd.RunE(cmd, []string{"x"})
	if !errors.Is(err, expected) {
		t.Fatalf("expected propagated error, got %v", err)
	}
}

func TestExecCommand_JoinsCommandAndCallsExecSSH(t *testing.T) {
	orig := execSSH
	t.Cleanup(func() { execSSH = orig })

	var gotHost, gotCmd string
	execSSH = func(host, remoteCmd string) error {
		gotHost, gotCmd = host, remoteCmd
		return nil
	}

	cmd := newExecCommand()
	if err := cmd.RunE(cmd, []string{"prod-api", "uname", "-a"}); err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}
	if gotHost != "prod-api" || gotCmd != "uname -a" {
		t.Fatalf("unexpected call: host=%q cmd=%q", gotHost, gotCmd)
	}
}

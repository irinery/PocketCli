package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"pocketcli/internal/connect"
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
	t.Setenv("HOME", t.TempDir())

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
	t.Setenv("HOME", t.TempDir())

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

func TestConnectCommand_UsesOrchestrator(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	orig := newConnectOrchestrator
	t.Cleanup(func() { newConnectOrchestrator = orig })

	called := false
	newConnectOrchestrator = func() *connect.Orchestrator {
		orchestrator := connect.New()
		orchestrator.Out = &bytes.Buffer{}
		orchestrator.Err = &bytes.Buffer{}
		orchestrator.ResolveHostFunc = func(ctx context.Context, host string) (connect.HostInfo, error) {
			called = true
			return connect.HostInfo{
				Name:      host,
				IP:        "100.64.0.10",
				Online:    true,
				Reachable: true,
				Trust:     connect.TrustObserved,
				Source:    connect.SourceTailscale,
				Action:    connect.ActionInteractive,
			}, nil
		}
		orchestrator.ApprovalFunc = func(ctx context.Context, info connect.HostInfo) (bool, error) {
			return false, nil
		}
		orchestrator.LookupPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
		return orchestrator
	}

	cmd := newConnectCommand()
	if err := cmd.RunE(cmd, []string{"devcenter"}); err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}
	if !called {
		t.Fatal("expected orchestrator to resolve host")
	}
}

func TestParseAskInputDefaultsScopeFromWorkingDir(t *testing.T) {
	input, err := parseAskInput([]string{"como", "persistir", "jsonl"}, "/tmp/PocketCli")
	if err != nil {
		t.Fatalf("parseAskInput returned error: %v", err)
	}

	if input.Scope != "project:pocketcli" {
		t.Fatalf("expected default project scope, got %q", input.Scope)
	}
	if input.Kind != "pattern" {
		t.Fatalf("expected default kind pattern, got %q", input.Kind)
	}
	if input.Prompt != "como persistir jsonl" {
		t.Fatalf("unexpected prompt: %q", input.Prompt)
	}
}

func TestParseAskInputParsesInlineFlags(t *testing.T) {
	input, err := parseAskInput([]string{"--kind=decision", "--scope=host:Mac Mini", "--title=Writer", "--tags=memory,writer", "salvar", "memória"}, filepath.Clean("/"))
	if err != nil {
		t.Fatalf("parseAskInput returned error: %v", err)
	}

	if input.Kind != "decision" {
		t.Fatalf("unexpected kind: %q", input.Kind)
	}
	if input.Scope != "host:Mac Mini" {
		t.Fatalf("unexpected scope: %q", input.Scope)
	}
	if input.Title != "Writer" {
		t.Fatalf("unexpected title: %q", input.Title)
	}
	if len(input.Tags) != 2 || input.Tags[0] != "memory" || input.Tags[1] != "writer" {
		t.Fatalf("unexpected tags: %#v", input.Tags)
	}
}

package main

import (
	"errors"
	"os"
	"path/filepath"
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

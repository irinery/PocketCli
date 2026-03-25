package main

import (
	"errors"
	"io"
	"os"
	"testing"
)

func TestIntegration_RootDispatchesHosts(t *testing.T) {
	orig := hostsViewer
	t.Cleanup(func() { hostsViewer = orig })

	called := false
	hostsViewer = func(in io.Reader, out io.Writer) error {
		called = true
		return nil
	}

	root := newRootCommand()
	origArgs := os.Args
	os.Args = []string{"pocket", "hosts"}
	t.Cleanup(func() { os.Args = origArgs })

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !called {
		t.Fatal("expected hostsViewer to be called")
	}
}

func TestIntegration_RootDispatchesExecAndPropagatesError(t *testing.T) {
	orig := execSSH
	t.Cleanup(func() { execSSH = orig })

	execSSH = func(host, remoteCmd string) error {
		if host != "node-1" || remoteCmd != "echo hi" {
			t.Fatalf("unexpected dispatch args host=%q cmd=%q", host, remoteCmd)
		}
		return errors.New("remote failed")
	}

	root := newRootCommand()
	origArgs := os.Args
	os.Args = []string{"pocket", "exec", "node-1", "echo", "hi"}
	t.Cleanup(func() { os.Args = origArgs })

	err := root.Execute()
	if err == nil || err.Error() != "remote failed" {
		t.Fatalf("expected dispatched error, got %v", err)
	}
}

package tailscale

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestMachinesFromStatus_NormalizesAndSorts(t *testing.T) {
	status := Status{Peer: map[string]Peer{
		"b": {HostName: "zeta", TailscaleIPs: []string{"100.64.0.20"}, Online: true, OS: "linux"},
		"a": {HostName: "alpha", TailscaleIPs: nil, Online: false, OS: ""},
	}}

	got := MachinesFromStatus(status)
	want := []Machine{
		{HostName: "alpha", IP: "n/a", Online: false, OS: "?"},
		{HostName: "zeta", IP: "100.64.0.20", Online: true, OS: "linux"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("machines mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestGetStatus_UsesTailscaleJSONOutput(t *testing.T) {
	origExec := execCommand
	t.Cleanup(func() { execCommand = origExec })

	execCommand = func(name string, args ...string) *exec.Cmd {
		if name != "tailscale" {
			t.Fatalf("unexpected binary: %s", name)
		}
		if strings.Join(args, " ") != "status --json" {
			t.Fatalf("unexpected args: %v", args)
		}
		return exec.Command("sh", "-c", `printf '{"Peer":{"x":{"HostName":"dev","TailscaleIPs":["100.64.0.8"],"Online":true,"OS":"linux"}}}'`)
	}

	status, err := GetStatus()
	if err != nil {
		t.Fatalf("GetStatus returned error: %v", err)
	}

	peer := status.Peer["x"]
	if peer.HostName != "dev" || peer.TailscaleIPs[0] != "100.64.0.8" || !peer.Online || peer.OS != "linux" {
		t.Fatalf("unexpected peer parsed: %#v", peer)
	}
}

func TestGetStatus_ReturnsParseError(t *testing.T) {
	origExec := execCommand
	t.Cleanup(func() { execCommand = origExec })

	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("sh", "-c", `printf 'not-json'`)
	}

	_, err := GetStatus()
	if err == nil || !strings.Contains(err.Error(), "failed to parse tailscale status json") {
		t.Fatalf("expected parse error, got: %v", err)
	}
}

func TestStatusOutputBufferCapsMemory(t *testing.T) {
	buffer := newStatusOutputBuffer(4)
	if _, err := buffer.Write([]byte("abcdef")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if buffer.String() != "abcd" || !buffer.Truncated() {
		t.Fatalf("unexpected buffer state output=%q truncated=%t", buffer.String(), buffer.Truncated())
	}
}

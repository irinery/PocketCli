package core

import (
	"bytes"
	"strings"
	"testing"

	"pocketcli/internal/tailscale"
)

func TestPrintHostsTable_RendersHeaderAndRows(t *testing.T) {
	machines := []tailscale.Machine{
		{HostName: "alpha", IP: "100.64.0.1", Online: true, OS: "linux"},
		{HostName: "beta", IP: "100.64.0.2", Online: false, OS: "macOS"},
	}

	var out bytes.Buffer
	PrintHostsTable(&out, machines)

	rendered := out.String()
	for _, expected := range []string{"HOST", "IP", "ONLINE", "OS", "alpha", "100.64.0.1", "yes", "beta", "no", "macOS"} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("expected %q in output, got:\n%s", expected, rendered)
		}
	}
}

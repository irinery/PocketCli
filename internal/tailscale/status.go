package tailscale

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
)

type Status struct {
	Peer map[string]Peer `json:"Peer"`
}

type Peer struct {
	HostName     string   `json:"HostName"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Online       bool     `json:"Online"`
	OS           string   `json:"OS"`
}

type Machine struct {
	HostName string
	IP       string
	Online   bool
	OS       string
}

var execCommand = exec.Command

const maxStatusOutputBytes = 1024 * 1024

func GetStatus() (Status, error) {
	cmd := execCommand("tailscale", "status", "--json")
	out := newStatusOutputBuffer(maxStatusOutputBytes)
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return Status{}, fmt.Errorf("tailscale status --json failed: %w", err)
	}
	if out.Truncated() {
		return Status{}, fmt.Errorf("tailscale status --json exceeded %d bytes", maxStatusOutputBytes)
	}

	var status Status
	if err := json.Unmarshal([]byte(out.String()), &status); err != nil {
		return Status{}, fmt.Errorf("failed to parse tailscale status json: %w", err)
	}

	return status, nil
}

type statusOutputBuffer struct {
	data      []byte
	maxBytes  int
	truncated bool
}

func newStatusOutputBuffer(maxBytes int) statusOutputBuffer {
	return statusOutputBuffer{maxBytes: maxBytes}
}

func (b *statusOutputBuffer) Write(value []byte) (int, error) {
	remaining := b.maxBytes - len(b.data)
	if remaining <= 0 {
		b.truncated = b.truncated || len(value) > 0
		return len(value), nil
	}
	if len(value) > remaining {
		b.data = append(b.data, value[:remaining]...)
		b.truncated = true
		return len(value), nil
	}
	b.data = append(b.data, value...)
	return len(value), nil
}

func (b *statusOutputBuffer) String() string { return string(b.data) }

func (b *statusOutputBuffer) Truncated() bool { return b.truncated }

func MachinesFromStatus(status Status) []Machine {
	machines := make([]Machine, 0, len(status.Peer))
	for _, peer := range status.Peer {
		ip := "n/a"
		if len(peer.TailscaleIPs) > 0 {
			ip = peer.TailscaleIPs[0]
		}

		os := peer.OS
		if os == "" {
			os = "?"
		}

		machines = append(machines, Machine{
			HostName: peer.HostName,
			IP:       ip,
			Online:   peer.Online,
			OS:       os,
		})
	}

	sort.Slice(machines, func(i, j int) bool {
		return machines[i].HostName < machines[j].HostName
	})

	return machines
}

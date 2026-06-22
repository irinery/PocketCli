package tailscale

import (
	"encoding/json"
	"fmt"
	"os"
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
var findCLI = func() (string, error) { return CLIPath(exec.LookPath) }

func GetStatus() (Status, error) {
	cli, err := findCLI()
	if err != nil {
		return Status{}, fmt.Errorf("tailscale status --json failed: %w", err)
	}
	cmd := execCommand(cli, "status", "--json")
	cmd.Env = append(os.Environ(), "TAILSCALE_BE_CLI=1")
	out, err := cmd.Output()
	if err != nil {
		return Status{}, fmt.Errorf("tailscale status --json failed: %w", err)
	}

	var status Status
	if err := json.Unmarshal(out, &status); err != nil {
		return Status{}, fmt.Errorf("failed to parse tailscale status json: %w", err)
	}

	return status, nil
}

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

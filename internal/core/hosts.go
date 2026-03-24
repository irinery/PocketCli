package core

import (
	"fmt"
	"io"
	"text/tabwriter"

	"pocketcli/internal/tailscale"
)

func PrintHostsTable(out io.Writer, machines []tailscale.Machine) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "HOST\tIP\tONLINE\tOS")
	for _, machine := range machines {
		online := "no"
		if machine.Online {
			online = "yes"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", machine.HostName, machine.IP, online, machine.OS)
	}
	_ = tw.Flush()
}

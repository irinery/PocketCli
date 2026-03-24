package main

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"pocketcli/internal/core"
	"pocketcli/internal/ssh"
	"pocketcli/internal/tailscale"
)

type statusFetcher func() (tailscale.Status, error)
type hostOpener func(string) error

func runHostsViewer(in io.Reader, out io.Writer, fetch statusFetcher, openHost hostOpener) error {
	status, err := fetch()
	if err != nil {
		return err
	}

	hosts := tailscale.MachinesFromStatus(status)
	if len(hosts) == 0 {
		fmt.Fprintln(out, "Nenhum host encontrado.")
		return nil
	}

	core.PrintHostsTable(out, hosts)

	reader := bufio.NewReader(in)
	fmt.Fprint(out, "> digite para filtrar: ")
	filter, err := readLine(reader)
	if err != nil {
		return fmt.Errorf("falha ao ler filtro: %w", err)
	}

	filteredHosts := filterHosts(hosts, filter)
	if len(filteredHosts) == 0 {
		return fmt.Errorf("nenhum host corresponde ao filtro %q", filter)
	}

	printHostsList(out, filteredHosts)
	fmt.Fprint(out, "> selecione host (número ou nome): ")
	selection, err := readLine(reader)
	if err != nil {
		return fmt.Errorf("falha ao ler seleção: %w", err)
	}

	host, err := selectHost(filteredHosts, selection)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "> conectar em %s...\n", host.HostName)
	return openHost(host.HostName)
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func filterHosts(hosts []tailscale.Machine, filter string) []tailscale.Machine {
	if strings.TrimSpace(filter) == "" {
		return hosts
	}

	needle := strings.ToLower(strings.TrimSpace(filter))
	filtered := make([]tailscale.Machine, 0, len(hosts))
	for _, host := range hosts {
		if strings.Contains(strings.ToLower(host.HostName), needle) {
			filtered = append(filtered, host)
		}
	}
	return filtered
}

func printHostsList(out io.Writer, hosts []tailscale.Machine) {
	for i, host := range hosts {
		fmt.Fprintf(out, "%d) %s\n", i+1, host.HostName)
	}
}

func selectHost(hosts []tailscale.Machine, selection string) (tailscale.Machine, error) {
	choice := strings.TrimSpace(selection)
	if choice == "" {
		return tailscale.Machine{}, fmt.Errorf("seleção vazia")
	}

	if idx, err := strconv.Atoi(choice); err == nil {
		if idx < 1 || idx > len(hosts) {
			return tailscale.Machine{}, fmt.Errorf("índice inválido: %d", idx)
		}
		return hosts[idx-1], nil
	}

	for _, host := range hosts {
		if strings.EqualFold(host.HostName, choice) {
			return host, nil
		}
	}

	return tailscale.Machine{}, fmt.Errorf("host não encontrado: %s", choice)
}

func defaultHostsViewer(in io.Reader, out io.Writer) error {
	return runHostsViewer(in, out, tailscale.GetStatus, ssh.Open)
}

package main

import (
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"pocketcli/internal/ssh"
	"pocketcli/internal/tailscale"
)

type statusFetcher func() (tailscale.Status, error)
type hostOpener func(string) error

type Host struct {
	Name   string
	IP     string
	OS     string
	Online bool
}

type State struct {
	Hosts    []Host
	Selected int
}

type hostsModel struct {
	state    State
	selected *Host
}

func newHostsModel(hosts []Host) hostsModel {
	return hostsModel{state: State{Hosts: hosts, Selected: 0}}
}

func (m hostsModel) Init() tea.Cmd {
	return nil
}

func (m hostsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.state.Selected > 0 {
				m.state.Selected--
			}
		case "down", "j":
			if m.state.Selected < len(m.state.Hosts)-1 {
				m.state.Selected++
			}
		case "enter":
			if len(m.state.Hosts) == 0 {
				return m, tea.Quit
			}
			h := m.state.Hosts[m.state.Selected]
			m.selected = &h
			return m, tea.Quit
		case "esc", "q", "ctrl+c":
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m hostsModel) View() string {
	var b strings.Builder
	b.WriteString("Pocket hosts\n\n")

	for i, host := range m.state.Hosts {
		cursor := "  "
		if i == m.state.Selected {
			cursor = "> "
		}

		status := "offline"
		if host.Online {
			status = "online"
		}

		fmt.Fprintf(&b, "%s%s  (%s • %s • %s)\n", cursor, host.Name, host.IP, host.OS, status)
	}

	b.WriteString("\n↑/↓ ou j/k para navegar • Enter para conectar • Esc para sair\n")
	return b.String()
}

func toHosts(machines []tailscale.Machine) []Host {
	hosts := make([]Host, 0, len(machines))
	for _, machine := range machines {
		hosts = append(hosts, Host{
			Name:   machine.HostName,
			IP:     machine.IP,
			OS:     machine.OS,
			Online: machine.Online,
		})
	}
	return hosts
}

func runHostsTUI(in io.Reader, out io.Writer, fetch statusFetcher, openHost hostOpener) error {
	status, err := fetch()
	if err != nil {
		return err
	}

	machines := tailscale.MachinesFromStatus(status)
	if len(machines) == 0 {
		fmt.Fprintln(out, "Nenhum host encontrado.")
		return nil
	}

	model := newHostsModel(toHosts(machines))
	program := tea.NewProgram(model, tea.WithInput(in), tea.WithOutput(out))

	finalModel, err := program.Run()
	if err != nil {
		return fmt.Errorf("falha ao iniciar TUI: %w", err)
	}

	result, ok := finalModel.(hostsModel)
	if !ok {
		return fmt.Errorf("modelo final inválido")
	}
	if result.selected == nil {
		return nil
	}

	fmt.Fprintf(out, "Conectando em %s...\n", result.selected.Name)
	return openHost(result.selected.Name)
}

func defaultHostsViewer(in io.Reader, out io.Writer) error {
	return runHostsTUI(in, out, tailscale.GetStatus, ssh.Open)
}

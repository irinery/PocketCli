package main

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"pocketcli/internal/tailscale"
)

func TestHostsModelNavigation_JKAndArrows(t *testing.T) {
	model := newHostsModel([]Host{{Name: "a"}, {Name: "b"}, {Name: "c"}})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m := updated.(hostsModel)
	if m.state.Selected != 1 {
		t.Fatalf("expected selected 1 after j, got %d", m.state.Selected)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(hostsModel)
	if m.state.Selected != 2 {
		t.Fatalf("expected selected 2 after down, got %d", m.state.Selected)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(hostsModel)
	if m.state.Selected != 1 {
		t.Fatalf("expected selected 1 after k, got %d", m.state.Selected)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(hostsModel)
	if m.state.Selected != 0 {
		t.Fatalf("expected selected 0 after up, got %d", m.state.Selected)
	}
}

func TestHostsModelEnterSelectsCurrentHost(t *testing.T) {
	model := hostsModel{state: State{Hosts: []Host{{Name: "alpha"}, {Name: "beta"}}, Selected: 1}}
	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m := updated.(hostsModel)
	if cmd == nil {
		t.Fatal("expected quit command on enter")
	}
	if m.selected == nil || m.selected.Name != "beta" {
		t.Fatalf("expected selected host beta, got %#v", m.selected)
	}
}

func TestRunHostsTUI_ConnectsSelectedHost(t *testing.T) {
	fakeStatus := tailscale.Status{Peer: map[string]tailscale.Peer{
		"a": {HostName: "prod-api", Online: true, OS: "linux", TailscaleIPs: []string{"100.64.0.1"}},
		"b": {HostName: "dev-db", Online: false, OS: "linux", TailscaleIPs: []string{"100.64.0.2"}},
	}}

	var opened string
	err := runHostsTUI(strings.NewReader("j\r"), &bytes.Buffer{}, func() (tailscale.Status, error) {
		return fakeStatus, nil
	}, func(host string) error {
		opened = host
		return nil
	})
	if err != nil {
		t.Fatalf("runHostsTUI returned error: %v", err)
	}
	if opened != "prod-api" {
		t.Fatalf("expected opened host prod-api, got %s", opened)
	}
}

func TestRunHostsTUI_NoHosts(t *testing.T) {
	out := &bytes.Buffer{}
	err := runHostsTUI(strings.NewReader(""), out, func() (tailscale.Status, error) {
		return tailscale.Status{Peer: map[string]tailscale.Peer{}}, nil
	}, func(host string) error { return nil })
	if err != nil {
		t.Fatalf("runHostsTUI returned error: %v", err)
	}
	if !strings.Contains(out.String(), "Nenhum host encontrado.") {
		t.Fatalf("expected no host message, got: %q", out.String())
	}
}

func TestHostsModelView_CompactWidthBreaksDetailsLine(t *testing.T) {
	model := hostsModel{
		state: State{
			Hosts: []Host{
				{Name: "prod-api-very-long-name", IP: "100.64.0.1", OS: "linux", Online: true},
			},
		},
		width: 40,
	}

	view := model.View()
	if !strings.Contains(view, "prod-api") {
		t.Fatalf("expected host name in compact view, got: %q", view)
	}
	if !strings.Contains(view, "100.64.0.1") {
		t.Fatalf("expected details line in compact view, got: %q", view)
	}
}

func TestFitText_TruncatesWithEllipsis(t *testing.T) {
	got := fitText("abcdefghijkl", 6)
	if got != "abcde…" {
		t.Fatalf("expected truncated text with ellipsis, got %q", got)
	}
}

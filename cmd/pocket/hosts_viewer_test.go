package main

import (
	"bytes"
	"strings"
	"testing"

	"pocketcli/internal/tailscale"
)

func TestFilterHosts_BySubstring(t *testing.T) {
	hosts := []tailscale.Machine{
		{HostName: "prod-api"},
		{HostName: "dev-db"},
		{HostName: "staging-api"},
	}

	filtered := filterHosts(hosts, "api")
	if len(filtered) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(filtered))
	}
	if filtered[0].HostName != "prod-api" || filtered[1].HostName != "staging-api" {
		t.Fatalf("unexpected filtered hosts: %+v", filtered)
	}
}

func TestSelectHost_ByNumber(t *testing.T) {
	hosts := []tailscale.Machine{{HostName: "alpha"}, {HostName: "beta"}}

	host, err := selectHost(hosts, "2")
	if err != nil {
		t.Fatalf("selectHost returned error: %v", err)
	}
	if host.HostName != "beta" {
		t.Fatalf("expected beta, got %s", host.HostName)
	}
}

func TestSelectHost_ByNameCaseInsensitive(t *testing.T) {
	hosts := []tailscale.Machine{{HostName: "Prod-API"}, {HostName: "dev-db"}}

	host, err := selectHost(hosts, "prod-api")
	if err != nil {
		t.Fatalf("selectHost returned error: %v", err)
	}
	if host.HostName != "Prod-API" {
		t.Fatalf("expected Prod-API, got %s", host.HostName)
	}
}

func TestRunHostsViewer_CompleteFlow(t *testing.T) {
	fakeStatus := tailscale.Status{Peer: map[string]tailscale.Peer{
		"a": {HostName: "prod-api", Online: true, OS: "linux", TailscaleIPs: []string{"100.64.0.1"}},
		"b": {HostName: "dev-db", Online: false, OS: "linux", TailscaleIPs: []string{"100.64.0.2"}},
	}}

	var opened string
	openHost := func(host string) error {
		opened = host
		return nil
	}

	in := strings.NewReader("prod\n1\n")
	out := &bytes.Buffer{}

	err := runHostsViewer(in, out, func() (tailscale.Status, error) {
		return fakeStatus, nil
	}, openHost)
	if err != nil {
		t.Fatalf("runHostsViewer returned error: %v", err)
	}
	if opened != "prod-api" {
		t.Fatalf("expected opened host prod-api, got %s", opened)
	}

	printed := out.String()
	if !strings.Contains(printed, "> digite para filtrar:") {
		t.Fatalf("expected filter prompt, got: %q", printed)
	}
	if !strings.Contains(printed, "> selecione host (número ou nome):") {
		t.Fatalf("expected selection prompt, got: %q", printed)
	}
	if !strings.Contains(printed, "> conectar em prod-api...") {
		t.Fatalf("expected connect message, got: %q", printed)
	}
}

func TestRunHostsViewer_FilterWithoutMatches(t *testing.T) {
	fakeStatus := tailscale.Status{Peer: map[string]tailscale.Peer{
		"a": {HostName: "prod-api", Online: true, OS: "linux", TailscaleIPs: []string{"100.64.0.1"}},
	}}

	err := runHostsViewer(strings.NewReader("zzz\n"), &bytes.Buffer{}, func() (tailscale.Status, error) {
		return fakeStatus, nil
	}, func(host string) error { return nil })
	if err == nil {
		t.Fatal("expected error when no host matches filter")
	}
}

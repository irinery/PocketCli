package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Actor names come only from profile/investigation-report.json; a missing or
// invalid profile keeps the pseudonyms.
func TestInvestigationRealNamesReadsOnlyTheProfile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".config", "pocketcli", investigationReportProfilePath)
	if investigationRealNames() {
		t.Fatal("sem perfil o padrão é pseudônimo")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		body string
		want bool
	}{
		{`{"version":"1","actor_names":"real"}`, true},
		{`{"version":"1","actor_names":"pseudonym"}`, false},
		{`{"version":"2","actor_names":"real"}`, false},
		{`{"version":"1","actor_names":"real","extra":1}`, false},
		{`não é json`, false},
	} {
		if err := os.WriteFile(path, []byte(test.body), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := investigationRealNames(); got != test.want {
			t.Fatalf("%s: nomes reais = %v, esperado %v", test.body, got, test.want)
		}
	}
}

func TestPublishPartialFlagRequiresReason(t *testing.T) {
	f, err := parseInvestigationFlags([]string{"--publish-partial", "auditoria parcial aceita"})
	if err != nil || f.publishPartial != "auditoria parcial aceita" {
		t.Fatalf("flag: %+v %v", f, err)
	}
	if _, err := parseInvestigationFlags([]string{"--publish-partial= "}); err == nil {
		t.Fatal("motivo vazio aceito")
	}
}

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"pocketcli/internal/investigation/insightcontract"
)

func TestConfiguredInsightProviderNeedsExplicitPolicyProfileAndCredential(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("POCKETCLI_TEST_OPENAI_KEY", "")
	cfg := insightcontract.AnalysisConfig{
		Provider: insightcontract.ProviderConfig{Kind: "configured", Model: "model-test", EndpointProfile: "work"},
		Policy:   insightcontract.Policy{ExternalProcessingAllowed: true},
	}
	provider, err := configuredInsightProvider(cfg)
	if err != nil || provider != nil {
		t.Fatalf("sem perfil não deve habilitar provider: provider=%T err=%v", provider, err)
	}
	path := filepath.Join(home, ".config", "pocketcli", insightProviderProfilePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	profile := insightProviderProfileFile{Version: insightProviderProfileVersion, Profiles: []insightProviderProfile{{ID: "work", Adapter: "openai-responses", Model: "model-test", CredentialEnv: "POCKETCLI_TEST_OPENAI_KEY"}}}
	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	provider, err = configuredInsightProvider(cfg)
	if err != nil || provider != nil {
		t.Fatalf("sem credencial não deve habilitar provider: provider=%T err=%v", provider, err)
	}
	t.Setenv("POCKETCLI_TEST_OPENAI_KEY", "synthetic-key")
	provider, err = configuredInsightProvider(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if provider == nil {
		t.Fatal("perfil, política e credencial explícitos devem construir provider sem executar chamada")
	}
	cfg.Policy.ExternalProcessingAllowed = false
	provider, err = configuredInsightProvider(cfg)
	if err != nil || provider != nil {
		t.Fatalf("política restritiva não pode construir provider: provider=%T err=%v", provider, err)
	}
}

func TestConfiguredInsightProviderRejectsProfileMismatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("POCKETCLI_TEST_OPENAI_KEY", "synthetic-key")
	path := filepath.Join(home, ".config", "pocketcli", insightProviderProfilePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	profile := insightProviderProfileFile{Version: insightProviderProfileVersion, Profiles: []insightProviderProfile{{ID: "work", Adapter: "openai-responses", Model: "approved-model", CredentialEnv: "POCKETCLI_TEST_OPENAI_KEY"}}}
	data, _ := json.Marshal(profile)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := insightcontract.AnalysisConfig{Provider: insightcontract.ProviderConfig{Kind: "configured", Model: "other-model", EndpointProfile: "work"}, Policy: insightcontract.Policy{ExternalProcessingAllowed: true}}
	if _, err := configuredInsightProvider(cfg); err == nil {
		t.Fatal("model divergente do perfil deve falhar antes da chamada externa")
	}
}

func TestReadAnalysisConfigRejectsUnknownFieldsAndTrailingJSON(t *testing.T) {
	for name, content := range map[string]string{
		"unknown field":  `{ "version":"investigate.insights.v1", "api_key":"secret" }`,
		"trailing value": `{ "version":"investigate.insights.v1" } {}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "analysis.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := readAnalysisConfig(path); err == nil {
				t.Fatal("configuração inválida aceita")
			}
		})
	}
}

func TestParseInsightAnalysisArgs(t *testing.T) {
	for name, args := range map[string][]string{
		"separate":  {"--snapshot", "snap-1", "--config", "analysis.json"},
		"equals":    {"--snapshot=snap-1", "--config=analysis.json"},
		"duplicate": {"--snapshot", "old", "--snapshot", "snap-1", "--config", "analysis.json"},
	} {
		t.Run(name, func(t *testing.T) {
			snapshot, config, err := parseInsightAnalysisArgs(args)
			if err != nil || snapshot != "snap-1" || config != "analysis.json" {
				t.Fatalf("parseInsightAnalysisArgs()=(%q,%q,%v)", snapshot, config, err)
			}
		})
	}
	if _, _, err := parseInsightAnalysisArgs([]string{"--snapshot", "snap-1", "--config"}); err == nil {
		t.Fatal("flag sem valor deve falhar")
	}
	if _, _, err := parseInsightAnalysisArgs([]string{"--snapshot", "snap-1", "--config", "cfg", "--token", "secret"}); err == nil {
		t.Fatal("flag desconhecida deve falhar")
	}
}

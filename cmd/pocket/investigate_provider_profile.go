package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"pocketcli/internal/investigation/decisionengine"
	"pocketcli/internal/investigation/insightcontract"
	"pocketcli/internal/pocketpath"
)

const (
	insightProviderProfilePath    = "profile/investigation-providers.json"
	insightProviderProfileVersion = "1"
)

var environmentVariableName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

type insightProviderProfileFile struct {
	Version  string                   `json:"version"`
	Profiles []insightProviderProfile `json:"profiles"`
}

type insightProviderProfile struct {
	ID            string `json:"id"`
	Adapter       string `json:"adapter"`
	Model         string `json:"model"`
	CredentialEnv string `json:"credential_env"`
	BaseURL       string `json:"base_url,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
	ProviderID    string `json:"provider_id,omitempty"`
	ProfileID     string `json:"profile_id,omitempty"`
}

func configuredInsightProvider(cfg insightcontract.AnalysisConfig) (insightcontract.ReasoningProvider, error) {
	if cfg.Provider.Kind != "configured" || !cfg.Policy.ExternalProcessingAllowed {
		return nil, nil
	}
	configDir, err := pocketpath.ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("ERR_CONFIG: resolver diretório de configuração: %w", err)
	}
	path := filepath.Join(configDir, insightProviderProfilePath)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ERR_CONFIG: ler perfis de provider: %w", err)
	}
	if len(raw) > 64*1024 {
		return nil, errors.New("ERR_CONFIG: arquivo de perfis de provider excede 64 KiB")
	}
	var file insightProviderProfileFile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("ERR_CONFIG: arquivo de perfis de provider inválido: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("ERR_CONFIG: conteúdo extra no arquivo de perfis de provider")
	}
	if file.Version != insightProviderProfileVersion {
		return nil, fmt.Errorf("ERR_CONFIG: versão de perfis de provider não suportada: %q", file.Version)
	}
	var selected *insightProviderProfile
	for i := range file.Profiles {
		profile := &file.Profiles[i]
		if profile.ID != cfg.Provider.EndpointProfile {
			continue
		}
		if selected != nil {
			return nil, fmt.Errorf("ERR_CONFIG: perfil de provider duplicado: %q", profile.ID)
		}
		selected = profile
	}
	if selected == nil {
		return nil, nil
	}
	if selected.ID == "" || selected.Adapter != "middlewareauth-llm" || selected.Model == "" || !environmentVariableName.MatchString(selected.CredentialEnv) {
		return nil, fmt.Errorf("ERR_CONFIG: perfil %q exige adapter middlewareauth-llm, model e credential_env válidos", selected.ID)
	}
	if cfg.Provider.Model != selected.Model {
		return nil, fmt.Errorf("ERR_CONFIG: o model da análise não corresponde ao perfil %q", selected.ID)
	}
	if cfg.Provider.AdapterVersion != "" && cfg.Provider.AdapterVersion != "middlewareauth-llm-v1" {
		return nil, fmt.Errorf("ERR_CONFIG: adapter_version incompatível com o perfil %q", selected.ID)
	}
	clientToken := strings.TrimSpace(os.Getenv(selected.CredentialEnv))
	if clientToken == "" {
		return nil, nil
	}
	return decisionengine.NewMiddlewareAuthProvider(selected.BaseURL, clientToken, selected.ProjectID, selected.ProviderID, selected.ProfileID, selected.Model, nil)
}

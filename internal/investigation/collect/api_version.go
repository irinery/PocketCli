package collect

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"time"

	"pocketcli/internal/investigation/contract"
)

const githubVersionCheckKind = "githubversions"
const githubVersionCheckTTL = 24 * time.Hour

type githubVersionCheck struct {
	BaseURL         string    `json:"base_url"`
	Version         string    `json:"version"`
	SelectedVersion string    `json:"selected_version,omitempty"`
	CheckedAt       time.Time `json:"checked_at"`
}

func (c *Client) cachedGitHubVersion() (bool, error) {
	if c.cacheStore == nil {
		return false, &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "storage da versão da API ausente"}
	}
	if _, err := c.providerHost("github"); err != nil {
		return false, err
	}
	key := principalHash("github-version", c.GitHubURL, githubAPIVersion)
	var check githubVersionCheck
	err := c.cacheStore.Get(githubVersionCheckKind, key, &check)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil || check.BaseURL != c.GitHubURL || check.Version != githubAPIVersion || check.CheckedAt.IsZero() || (check.SelectedVersion != "" && check.SelectedVersion != githubAPIVersion) {
		return false, &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "verificação da versão GitHub corrompida"}
	}
	age := time.Since(check.CheckedAt)
	if age < -5*time.Minute {
		return false, &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "horário da versão GitHub inválido"}
	}
	return age < githubVersionCheckTTL, nil
}

func (c *Client) GitHubVersionLookupNeeded(_ context.Context) (bool, error) {
	if !c.resolvePrincipals {
		return false, nil
	}
	fresh, err := c.cachedGitHubVersion()
	return !fresh, err
}

func (c *Client) VerifyGitHubVersion(ctx context.Context) (string, error) {
	if !c.resolvePrincipals {
		return "", nil
	}
	if fresh, err := c.cachedGitHubVersion(); err != nil {
		return "", err
	} else if fresh {
		return githubAPIVersion, nil
	}
	endpoint, err := url.JoinPath(c.GitHubURL, "versions")
	if err != nil {
		return "", &contract.ErrorResponse{Code: "ERR_UNSUPPORTED_SOURCE", Message: "host GitHub inválido"}
	}
	body, headers, _, err := c.GetGitHub(ctx, endpoint)
	if err != nil {
		return "", err
	}
	var versions []string
	if json.Unmarshal(body, &versions) != nil || len(versions) == 0 {
		return "", &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "resposta de versões GitHub inválida"}
	}
	supported := false
	for _, version := range versions {
		if version == githubAPIVersion {
			supported = true
			break
		}
	}
	if !supported {
		return "", &contract.ErrorResponse{Code: "ERR_SCHEMA_VERSION_UNSUPPORTED", Message: "versão REST GitHub configurada não é suportada pelo servidor"}
	}
	selected := headers.Get("X-Github-Api-Version-Selected")
	if selected != "" && selected != githubAPIVersion {
		return "", &contract.ErrorResponse{Code: "ERR_SCHEMA_VERSION_UNSUPPORTED", Message: "servidor GitHub selecionou outra versão REST"}
	}
	key := principalHash("github-version", c.GitHubURL, githubAPIVersion)
	check := githubVersionCheck{BaseURL: c.GitHubURL, Version: githubAPIVersion, SelectedVersion: selected, CheckedAt: time.Now().UTC()}
	if err := c.cacheStore.Put(githubVersionCheckKind, key, check); err != nil {
		return "", &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "falha ao guardar versão GitHub verificada"}
	}
	return githubAPIVersion, nil
}

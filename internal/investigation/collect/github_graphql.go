package collect

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"pocketcli/internal/investigation/contract"
)

// GitHubQueryWithBytes executes a fixed, read-only GraphQL query and reports
// transferred bytes even when GraphQL returns errors with HTTP 200.
func (c *Client) GitHubQueryWithBytes(ctx context.Context, query string, variables map[string]any) (json.RawMessage, int64, error) {
	if !strings.HasPrefix(strings.TrimSpace(query), "query ") {
		return nil, 0, &contract.ErrorResponse{Code: "ERR_VALIDATION", Message: "apenas query GraphQL fixa"}
	}
	base, err := url.Parse(c.GitHubURL)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return nil, 0, &contract.ErrorResponse{Code: "ERR_UNSUPPORTED_SOURCE", Message: "GitHub URL inválida"}
	}
	switch {
	case base.Host == "api.github.com" && base.Path == "":
		base.Path = "/graphql"
	case strings.HasSuffix(base.Path, "/api/v3"):
		base.Path = strings.TrimSuffix(base.Path, "/api/v3") + "/api/graphql"
	default:
		return nil, 0, &contract.ErrorResponse{Code: "ERR_UNSUPPORTED_SOURCE", Message: "GraphQL GitHub indisponível para este endpoint"}
	}
	base.RawQuery, base.Fragment = "", ""
	body, err := json.Marshal(struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}{query, variables})
	if err != nil {
		return nil, 0, &contract.ErrorResponse{Code: "ERR_VALIDATION", Message: "variáveis GraphQL inválidas"}
	}
	data, _, _, wireBytes, err := c.request(ctx, "github", "POST", base.String(), strings.NewReader(string(body)))
	if err != nil {
		return nil, wireBytes, err
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message    string `json:"message"`
			Extensions struct {
				Code string `json:"code"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	if json.Unmarshal(data, &envelope) != nil {
		return nil, wireBytes, &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "GraphQL GitHub inválido"}
	}
	if len(envelope.Errors) > 0 {
		for _, issue := range envelope.Errors {
			if issue.Extensions.Code == "RATE_LIMITED" {
				return nil, wireBytes, &contract.ErrorResponse{Code: "ERR_RATE_LIMITED", Message: "limite GraphQL GitHub atingido", Retryable: true}
			}
		}
		return nil, wireBytes, &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "GraphQL GitHub retornou erro parcial"}
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil, wireBytes, &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "GraphQL GitHub sem dados"}
	}
	return envelope.Data, wireBytes, nil
}

func githubGraphQLRateLimited(body []byte) bool {
	var envelope struct {
		Errors []struct {
			Type       string `json:"type"`
			Extensions struct {
				Code string `json:"code"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return false
	}
	for _, issue := range envelope.Errors {
		if strings.EqualFold(issue.Type, "RATE_LIMITED") || strings.EqualFold(issue.Extensions.Code, "RATE_LIMITED") {
			return true
		}
	}
	return false
}

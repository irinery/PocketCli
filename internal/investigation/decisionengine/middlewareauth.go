package decisionengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"pocketcli/internal/investigation/insightcontract"
)

const defaultMiddlewareAuthURL = "http://127.0.0.1:18787"

var middlewareID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// MiddlewareAuthProvider sends semantic requests through the local credential
// broker. The broker owns the upstream credential and provider adapter.
type MiddlewareAuthProvider struct {
	baseURL, token, projectID, providerID, profileID, model string
	client                                                  *http.Client
}

func NewMiddlewareAuthProvider(baseURL, token, projectID, providerID, profileID, model string, client *http.Client) (*MiddlewareAuthProvider, error) {
	if baseURL == "" {
		baseURL = defaultMiddlewareAuthURL
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Hostname() != "127.0.0.1" || net.ParseIP(u.Hostname()) == nil {
		return nil, errors.New("decisionengine: baseURL do MiddlewareAuth deve ser HTTP local em 127.0.0.1")
	}
	if strings.TrimSpace(token) == "" || !middlewareID.MatchString(projectID) || !middlewareID.MatchString(providerID) || !middlewareID.MatchString(profileID) || strings.TrimSpace(model) == "" {
		return nil, errors.New("decisionengine: perfil MiddlewareAuth incompleto")
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &MiddlewareAuthProvider{baseURL: strings.TrimRight(baseURL, "/"), token: token, projectID: projectID, providerID: providerID, profileID: profileID, model: model, client: client}, nil
}

func (p *MiddlewareAuthProvider) ProviderID() string     { return "middlewareauth:" + p.providerID }
func (p *MiddlewareAuthProvider) ModelName() string      { return p.model }
func (p *MiddlewareAuthProvider) AdapterVersion() string { return "middlewareauth-llm-v1" }
func (p *MiddlewareAuthProvider) IsLocal() bool          { return false }

func (p *MiddlewareAuthProvider) Evaluate(ctx context.Context, req insightcontract.ReasoningRequest) (insightcontract.ReasoningResponse, error) {
	if p == nil || p.token == "" {
		return insightcontract.ReasoningResponse{}, errors.New("decisionengine: MiddlewareAuth não configurado")
	}
	if req.Task != "extract" && req.Task != "reconcile" && req.Task != "assess" {
		return insightcontract.ReasoningResponse{}, errors.New("decisionengine: task inválida")
	}
	if req.Budget.MaxRequests < 1 || req.Budget.MaxInputTokens < 1 || req.Budget.MaxOutputTokens < 1 || req.Budget.MaxElapsedMS < 1 {
		return insightcontract.ReasoningResponse{}, errors.New("decisionengine: budget incompleto")
	}
	if req.Budget.MaxCost != nil {
		return insightcontract.ReasoningResponse{}, errors.New("decisionengine: limite de custo requer preço verificável")
	}
	input, err := json.Marshal(req)
	if err != nil {
		return insightcontract.ReasoningResponse{}, fmt.Errorf("decisionengine: serializar request: %w", err)
	}
	if int64(len(input)) > req.Budget.MaxInputTokens {
		return insightcontract.ReasoningResponse{}, errors.New("decisionengine: entrada excede budget conservador")
	}
	request := struct {
		ProviderID      string          `json:"providerId"`
		ProfileID       string          `json:"profileId"`
		Model           string          `json:"model"`
		Instructions    string          `json:"instructions"`
		Input           []openAIMessage `json:"input"`
		Store           bool            `json:"store"`
		MaxOutputTokens int64           `json:"max_output_tokens"`
	}{p.providerID, p.profileID, p.model, decisionSystemPrompt, []openAIMessage{{Role: "user", Content: string(input)}}, false, req.Budget.MaxOutputTokens}
	body, err := json.Marshal(request)
	if err != nil {
		return insightcontract.ReasoningResponse{}, fmt.Errorf("decisionengine: serializar chamada: %w", err)
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(req.Budget.MaxElapsedMS)*time.Millisecond)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, p.baseURL+"/v1/projects/"+p.projectID+"/llm/responses", bytes.NewReader(body))
	if err != nil {
		return insightcontract.ReasoningResponse{}, errors.New("decisionengine: chamada MiddlewareAuth inválida")
	}
	httpReq.Header.Set("Authorization", "Bearer "+p.token)
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Content-Type", "application/json")
	client := *p.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	started := time.Now()
	resp, err := client.Do(httpReq)
	if err != nil {
		return insightcontract.ReasoningResponse{}, errors.New("decisionengine: MiddlewareAuth indisponível")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return insightcontract.ReasoningResponse{}, fmt.Errorf("decisionengine: MiddlewareAuth retornou HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
	if err != nil || len(data) > 16<<20 {
		return insightcontract.ReasoningResponse{}, errors.New("decisionengine: resposta MiddlewareAuth inválida ou grande demais")
	}
	var wire struct {
		OutputText string `json:"outputText"`
		Usage      struct {
			InputTokens  *int64 `json:"inputTokens"`
			OutputTokens *int64 `json:"outputTokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &wire); err != nil || wire.OutputText == "" {
		return insightcontract.ReasoningResponse{}, errors.New("decisionengine: MiddlewareAuth não retornou JSON de análise")
	}
	var analysis insightcontract.DecisionAnalysis
	dec := json.NewDecoder(strings.NewReader(wire.OutputText))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&analysis); err != nil {
		return insightcontract.ReasoningResponse{}, errors.New("decisionengine: JSON de análise inválido")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return insightcontract.ReasoningResponse{}, errors.New("decisionengine: conteúdo extra após JSON de análise")
	}
	return insightcontract.ReasoningResponse{Analysis: analysis, Usage: insightcontract.Usage{Requests: 1, ElapsedMS: time.Since(started).Milliseconds(), InputTokens: wire.Usage.InputTokens, OutputTokens: wire.Usage.OutputTokens}}, nil
}

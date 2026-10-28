package decisionengine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"pocketcli/internal/investigation/insightcontract"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenAIResponsesAdapterUsesDocumentedContractOffline(t *testing.T) {
	key := "secret-for-test-only"
	inputTokens, outputTokens := int64(81), int64(34)
	analysis := insightcontract.DecisionAnalysis{Version: insightcontract.Version, ID: "analysis-test", SnapshotID: "snap-test", Status: "partial", Claims: []insightcontract.Claim{}, ClaimRelations: []insightcontract.ClaimRelation{}, Decisions: []insightcontract.DecisionRecord{}, Relations: []insightcontract.DecisionRelation{}, Cases: []insightcontract.DecisionCase{}, Dimensions: []insightcontract.DimensionStatus{}, Limitations: []string{}}
	analysisJSON, err := json.Marshal(analysis)
	if err != nil {
		t.Fatal(err)
	}
	apiJSON, err := json.Marshal(openAIResponse{Status: "completed", Output: []openAIOutputItem{{Type: "message", Content: []openAIOutputContent{{Type: "output_text", Text: string(analysisJSON)}}}}, Usage: struct {
		InputTokens  *int64 `json:"input_tokens"`
		OutputTokens *int64 `json:"output_tokens"`
	}{InputTokens: &inputTokens, OutputTokens: &outputTokens}})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.String() != openAIResponsesURL {
			t.Fatalf("endpoint incorreto: %s %s", req.Method, req.URL)
		}
		if req.Header.Get("Authorization") != "Bearer "+key {
			t.Fatal("header de autenticação ausente")
		}
		body, _ := io.ReadAll(req.Body)
		if strings.Contains(string(body), key) {
			t.Fatal("chave apareceu no payload")
		}
		var sent openAIRequest
		if err := json.Unmarshal(body, &sent); err != nil {
			t.Fatal(err)
		}
		if sent.Model != "gpt-explicit-test" || sent.MaxOutputTokens != 900 || sent.Store || sent.Text.Format.Type != "json_object" {
			t.Fatalf("request API inesperado: %#v", sent)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(apiJSON)))}, nil
	})}
	provider, err := NewOpenAIResponsesProvider(key, "gpt-explicit-test", client)
	if err != nil {
		t.Fatal(err)
	}
	got, err := provider.Evaluate(context.Background(), insightcontract.ReasoningRequest{Task: "extract", SnapshotID: "snap-test", Budget: insightcontract.Budget{MaxRequests: 1, MaxInputTokens: 10000, MaxOutputTokens: 900, MaxElapsedMS: 5000}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Analysis.ID != analysis.ID || got.Usage.Requests != 1 || got.Usage.InputTokens == nil || *got.Usage.InputTokens != inputTokens || got.Usage.Cost != nil {
		t.Fatalf("resposta/uso inesperados: %#v", got)
	}
}

func TestOpenAIResponsesAdapterRejectsEmptyConfigAndHTTPError(t *testing.T) {
	if _, err := NewOpenAIResponsesProvider("", "model", nil); err == nil {
		t.Fatal("aceitou chave vazia")
	}
	if _, err := NewOpenAIResponsesProvider("key", "", nil); err == nil {
		t.Fatal("aceitou model vazio")
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(strings.NewReader(`{"error":"not included in surfaced error"}`))}, nil
	})}
	p, _ := NewOpenAIResponsesProvider("key", "model", client)
	_, err := p.Evaluate(context.Background(), insightcontract.ReasoningRequest{Task: "assess", Budget: insightcontract.Budget{MaxRequests: 1, MaxInputTokens: 100, MaxOutputTokens: 10, MaxElapsedMS: 5000}})
	if err == nil || strings.Contains(err.Error(), "not included") || strings.Contains(err.Error(), "key") {
		t.Fatalf("erro não foi saneado: %v", err)
	}
}

func TestAnalyzeRunsConfiguredOpenAIAdapterThroughContractValidation(t *testing.T) {
	f := loadFixture(t)
	f.Config.Provider.Kind = "configured"
	f.Config.Provider.Model = "model-under-test"
	f.Config.Policy.ContentMode = "export_excerpts"
	f.Config.Policy.ExternalProcessingAllowed = true
	analysis := insightcontract.DecisionAnalysis{Version: insightcontract.Version, ID: "model-result", SnapshotID: f.Source.Snapshot.ID, Status: "partial", Claims: []insightcontract.Claim{}, ClaimRelations: []insightcontract.ClaimRelation{}, Decisions: []insightcontract.DecisionRecord{}, Relations: []insightcontract.DecisionRelation{}, Cases: []insightcontract.DecisionCase{}, Dimensions: []insightcontract.DimensionStatus{{ObjectiveID: "OBJ-DIRECTION", Dimension: "direction", Status: "indeterminate", Reason: "golden sem meta mensurável", MetricIDs: []string{}, CaseIDs: []string{}, Missing: []string{"target null"}}}, Limitations: []string{"synthetic API response"}}
	encoded, err := json.Marshal(analysis)
	if err != nil {
		t.Fatal(err)
	}
	responseBody := `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":` + string(mustJSON(t, string(encoded))) + `}]}],"usage":{"input_tokens":120,"output_tokens":80}}`
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(responseBody))}, nil
	})}
	provider, err := NewOpenAIResponsesProvider("test-key", f.Config.Provider.Model, client)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Analyze(context.Background(), f.Source, f.Facts, f.Config, provider)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "partial" || got.ID != "model-result" || got.Provenance.Provider != "openai-responses" || got.Provenance.Model != f.Config.Provider.Model || got.Usage.InputTokens == nil || *got.Usage.InputTokens != 120 {
		t.Fatalf("pipeline configurado não preservou contrato/uso: %#v", got)
	}
}

func mustJSON(t *testing.T, value string) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

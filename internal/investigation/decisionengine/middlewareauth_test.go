package decisionengine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"pocketcli/internal/investigation/insightcontract"
)

func TestMiddlewareAuthProviderContractAndSecretIsolation(t *testing.T) {
	analysis := insightcontract.DecisionAnalysis{Version: insightcontract.Version, ID: "semantic-test", SnapshotID: "snapshot-test", Status: "partial"}
	encoded, err := json.Marshal(analysis)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/pocketcli/llm/responses" || r.Method != http.MethodPost {
			t.Errorf("rota inesperada: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer local-token" {
			t.Error("token interno ausente")
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "local-token") {
			t.Error("token vazou no corpo")
		}
		var request struct {
			ProviderID      string          `json:"providerId"`
			ProfileID       string          `json:"profileId"`
			Model           string          `json:"model"`
			Instructions    string          `json:"instructions"`
			Input           []openAIMessage `json:"input"`
			Store           bool            `json:"store"`
			MaxOutputTokens int64           `json:"max_output_tokens"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
		}
		if request.ProviderID != "openai" || request.ProfileID != "default" || request.Model != "approved-model" || request.Instructions == "" || len(request.Input) != 1 || request.Store || request.MaxOutputTokens != 900 {
			t.Errorf("request inválido: %#v", request)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"outputText": string(encoded), "usage": map[string]int64{"inputTokens": 12, "outputTokens": 7}})
	}))
	defer server.Close()
	provider, err := NewMiddlewareAuthProvider(server.URL, "local-token", "pocketcli", "openai", "default", "approved-model", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	got, err := provider.Evaluate(context.Background(), insightcontract.ReasoningRequest{Task: "extract", SnapshotID: "snapshot-test", Budget: insightcontract.Budget{MaxRequests: 1, MaxInputTokens: 10000, MaxOutputTokens: 900, MaxElapsedMS: 5000}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Analysis.ID != analysis.ID || got.Usage.InputTokens == nil || *got.Usage.InputTokens != 12 || got.Usage.Cost != nil {
		t.Fatalf("resposta inválida: %#v", got)
	}
}

func TestMiddlewareAuthProviderRejectsUnsafeEndpointAndSanitizesError(t *testing.T) {
	for _, endpoint := range []string{"https://example.org", "http://localhost:18787", "http://127.0.0.1:18787/path", "http://token@127.0.0.1:18787"} {
		if _, err := NewMiddlewareAuthProvider(endpoint, "token", "pocketcli", "openai", "default", "model", nil); err == nil {
			t.Errorf("endpoint aceito: %s", endpoint)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"secret reflected by provider"}}`))
	}))
	defer server.Close()
	provider, err := NewMiddlewareAuthProvider(server.URL, "token", "pocketcli", "openai", "default", "model", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = provider.Evaluate(context.Background(), insightcontract.ReasoningRequest{Task: "assess", Budget: insightcontract.Budget{MaxRequests: 1, MaxInputTokens: 1000, MaxOutputTokens: 10, MaxElapsedMS: 5000}})
	if err == nil || strings.Contains(err.Error(), "secret reflected") {
		t.Fatalf("erro não saneado: %v", err)
	}
}

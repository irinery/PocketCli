package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/secrets"
)

func TestInvestigationJSONErrorsKeepCodeAndHideUnknownDetails(t *testing.T) {
	if !investigationJSONRequested([]string{"investigate", "refresh", "id", "--json"}) || investigationJSONRequested([]string{"ask", "--json"}) {
		t.Fatal("detecção de modo JSON incorreta")
	}
	for _, tc := range []struct {
		name, code, message string
		err                 error
	}{
		{"typed", "ERR_PERIOD_REQUIRED", "período obrigatório", &contract.ErrorResponse{Code: "ERR_PERIOD_REQUIRED", Message: "período obrigatório"}},
		{"generic", "ERR_UNAVAILABLE", "falha ao executar investigate", errors.New("falha com segredo-ficticio")},
		{"flag", "ERR_VALIDATION", "parâmetros inválidos", errors.New("flag --bad inválida")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := writeInvestigationJSONError(&output, tc.err); err != nil {
				t.Fatal(err)
			}
			var response contract.ErrorResponse
			if err := json.Unmarshal(output.Bytes(), &response); err != nil || response.OK || response.Code != tc.code || response.Message != tc.message || response.Fields == nil {
				t.Fatalf("erro estruturado: %+v decode=%v", response, err)
			}
			if strings.Contains(output.String(), "segredo-ficticio") {
				t.Fatalf("erro expôs detalhe genérico: %s", output.String())
			}
		})
	}
}

func TestInvestigationJSONCredentialsRequireEnvironmentWithoutPrompt(t *testing.T) {
	t.Setenv("POCKETCLI_INVESTIGATE_GITHUB_TOKEN", "")
	t.Setenv("PATH", t.TempDir()) // Isola a prova de credencial ausente do gh da máquina.
	store := secrets.NewSessionStore()
	err := loadCredentials(store, contract.PlanInput{GitHubEnabled: true}, false)
	var response *contract.ErrorResponse
	if !errors.As(err, &response) || response.Code != "ERR_CREDENTIAL_REQUIRED" {
		t.Fatalf("credencial ausente: %v", err)
	}
	if err := store.WithSecret(context.Background(), "github", func([]byte) error { return nil }); err == nil {
		t.Fatal("fonte recebeu uma credencial sem variável de ambiente")
	}
}

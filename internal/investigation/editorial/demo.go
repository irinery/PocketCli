package editorial

import (
	"context"
	"errors"

	"pocketcli/internal/investigation/contract"
)

// NewWithDemoProvider wires a local, offline provider for preview scenarios
// only (cmd/investigate-preview --scenario ...). It never opens a network
// connection and never reads the stored key's bytes beyond the normal
// SecretProvider flow. Production composition must use New.
//
// Supported behaviors:
//   - "invalid_output": the model answers with non-JSON text, exercising the
//     ERR_LLM_OUTPUT_INVALID path (RFC 02 C.9 llm_invalid_output).
func NewWithDemoProvider(core contract.Core, notes annotationReader, secrets secretProvider, dataDir, behavior string) (*Service, error) {
	if behavior != "invalid_output" {
		return nil, errors.New("comportamento de provedor demo desconhecido")
	}
	s, err := New(core, notes, secrets, dataDir)
	if err != nil {
		return nil, err
	}
	s.provider = demoProvider{behavior: behavior}
	return s, nil
}

type demoProvider struct{ behavior string }

func (demoProvider) Models(context.Context, []byte) ([]Model, error) {
	return []Model{{ID: "demo-editorial", DisplayName: "Provedor fictício local (DEMO)"}}, nil
}

func (p demoProvider) Message(context.Context, []byte, string, string) (providerResult, error) {
	return providerResult{Text: "Claro! Aqui está a redação melhorada, sem JSON.", RequestID: "demo-" + p.behavior, StopReason: "end_turn", InputTokens: 1, OutputTokens: 1}, nil
}

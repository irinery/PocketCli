package secrets

import (
	"context"
	"sync"

	"pocketcli/internal/investigation/contract"
)

// SessionStore never writes credentials to disk. It deliberately has no keychain fallback.
type SessionStore struct {
	mu     sync.Mutex
	values map[string][]byte
	status map[string]contract.CredentialStatus
}

func NewSessionStore() *SessionStore {
	return &SessionStore{values: make(map[string][]byte), status: make(map[string]contract.CredentialStatus)}
}

func (s *SessionStore) Put(_ context.Context, in contract.CredentialInput) (contract.CredentialStatus, error) {
	if in.Source != "github" && in.Source != "linear" && in.Source != "claude" {
		return contract.CredentialStatus{}, &contract.ErrorResponse{Code: "ERR_VALIDATION", Message: "fonte de credencial inválida"}
	}
	if in.Persistence != "session" {
		return contract.CredentialStatus{}, &contract.ErrorResponse{Code: "ERR_SECRET_STORE_UNAVAILABLE", Message: "keychain não configurado; escolha session explicitamente"}
	}
	if in.Secret == "" {
		return contract.CredentialStatus{}, &contract.ErrorResponse{Code: "ERR_CREDENTIAL_REQUIRED", Message: "credencial vazia"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old := s.values[in.Source]; old != nil {
		clear(old)
	}
	s.values[in.Source] = []byte(in.Secret)
	st := contract.CredentialStatus{Source: in.Source, Configured: true, Storage: "session", MissingPermissions: []string{}}
	s.status[in.Source] = st
	return st, nil
}

func (s *SessionStore) Status(_ context.Context, source string) (contract.CredentialStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if st, ok := s.status[source]; ok {
		return st, nil
	}
	return contract.CredentialStatus{Source: source, Storage: "session", MissingPermissions: []string{}}, nil
}

func (s *SessionStore) Delete(_ context.Context, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.values[source])
	delete(s.values, source)
	delete(s.status, source)
	return nil
}

func (s *SessionStore) WithSecret(_ context.Context, source string, fn func([]byte) error) error {
	s.mu.Lock()
	value := s.values[source]
	if len(value) == 0 {
		s.mu.Unlock()
		return &contract.ErrorResponse{Code: "ERR_CREDENTIAL_REQUIRED", Message: "credencial não configurada"}
	}
	tmp := append([]byte(nil), value...)
	s.mu.Unlock()
	defer clear(tmp)
	return fn(tmp)
}

var _ contract.CredentialStore = (*SessionStore)(nil)
var _ contract.SecretProvider = (*SessionStore)(nil)

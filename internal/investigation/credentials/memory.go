package credentials

import (
	"context"
	"errors"
	"strings"
	"sync"

	"pocketcli/internal/investigation/contract"
)

// MemoryStore keeps local secrets only for the lifetime of the preview process.
type MemoryStore struct {
	mu      sync.RWMutex
	secrets map[string][]byte
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{secrets: map[string][]byte{}} }
func (s *MemoryStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, b := range s.secrets {
		zero(b)
		delete(s.secrets, k)
	}
}
func (s *MemoryStore) Put(_ context.Context, in contract.CredentialInput) (contract.CredentialStatus, error) {
	if in.Source != "github" && in.Source != "linear" && in.Source != "claude" {
		return contract.CredentialStatus{}, errors.New("ERR_VALIDATION: fonte de credencial inválida")
	}
	if strings.TrimSpace(in.Secret) == "" {
		return contract.CredentialStatus{}, errors.New("ERR_VALIDATION: credencial vazia")
	}
	if in.Persistence != "session" && in.Persistence != "memory" {
		return contract.CredentialStatus{}, errors.New("ERR_VALIDATION: preview aceita apenas credencial durante esta sessão")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if old := s.secrets[in.Source]; old != nil {
		zero(old)
	}
	s.secrets[in.Source] = []byte(in.Secret)
	return contract.CredentialStatus{Source: in.Source, Configured: true, Storage: "session", Identity: "não validada", MissingPermissions: []string{}}, nil
}
func (s *MemoryStore) Status(_ context.Context, source string) (contract.CredentialStatus, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.secrets[source]
	return contract.CredentialStatus{Source: source, Configured: ok, Validated: false, Storage: map[bool]string{true: "session", false: "none"}[ok], Identity: "não validada", MissingPermissions: []string{}}, nil
}
func (s *MemoryStore) Delete(_ context.Context, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b := s.secrets[source]; b != nil {
		zero(b)
		delete(s.secrets, source)
	}
	return nil
}
func (s *MemoryStore) WithSecret(_ context.Context, source string, fn func([]byte) error) error {
	s.mu.RLock()
	secret := append([]byte(nil), s.secrets[source]...)
	s.mu.RUnlock()
	if len(secret) == 0 {
		return errors.New("ERR_CREDENTIAL_REQUIRED: informe a chave de API na aba Claude")
	}
	defer zero(secret)
	return fn(secret)
}
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

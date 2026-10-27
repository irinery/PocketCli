package collect

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"syscall"
	"time"

	"pocketcli/internal/investigation/contract"
)

const principalBindingKind = "principalbindings"

type principalLookupKey struct{}

type principalBinding struct {
	CredentialKey string `json:"credential_key"`
	Source        string `json:"source"`
	Host          string `json:"host"`
	PrincipalID   string `json:"principal_id"`
	PrincipalKey  string `json:"principal_key"`
	Login         string `json:"login,omitempty"`
	Version       int    `json:"version,omitempty"`
}

func (c *Client) EnablePrincipalResolution() { c.resolvePrincipals = true }

func principalHash(parts ...string) string {
	h := sha256.New()
	for _, part := range parts {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func principalCredentialKey(source, host string, secret []byte) string {
	h := sha256.New()
	h.Write([]byte(source))
	h.Write([]byte{0})
	h.Write([]byte(host))
	h.Write([]byte{0})
	h.Write(secret)
	h.Write([]byte{0})
	return hex.EncodeToString(h.Sum(nil))
}

func (c *Client) providerHost(source string) (string, error) {
	endpoint := c.GitHubURL
	if source == "linear" {
		endpoint = c.LinearURL
	} else if source != "github" {
		return "", &contract.ErrorResponse{Code: "ERR_UNSUPPORTED_SOURCE", Message: "fonte inválida"}
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return "", &contract.ErrorResponse{Code: "ERR_UNSUPPORTED_SOURCE", Message: "host da fonte inválido"}
	}
	return u.Host, nil
}

func (c *Client) bindingFor(source string, secret []byte) (principalBinding, bool, error) {
	var binding principalBinding
	if c.cacheStore == nil {
		return binding, false, &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "storage da fila ausente"}
	}
	host, err := c.providerHost(source)
	if err != nil {
		return binding, false, err
	}
	key := principalCredentialKey(source, host, secret)
	err = c.cacheStore.Get(principalBindingKind, key, &binding)
	if errors.Is(err, os.ErrNotExist) {
		return principalBinding{}, false, nil
	}
	if err != nil || binding.CredentialKey != key || binding.Source != source || binding.Host != host || !validPrincipalID(binding.PrincipalID) || binding.PrincipalKey != principalHash(source, host, binding.PrincipalID) || binding.Version < 0 || binding.Version > 1 || (binding.Login != "" && (source != "github" || !validGitHubLogin(binding.Login))) {
		return principalBinding{}, false, &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "identidade da fonte corrompida"}
	}
	return binding, true, nil
}

func (c *Client) principalRateKey(source string, secret []byte) (string, error) {
	if !c.resolvePrincipals {
		return rateHoldKey(source, secret), nil
	}
	binding, found, err := c.bindingFor(source, secret)
	if err != nil {
		return "", err
	}
	if found {
		return binding.PrincipalKey, nil
	}
	return rateHoldKey(source, secret), nil // Identity request before binding.
}

func (c *Client) lockProvider(ctx context.Context, source string, shared bool) (func(), error) {
	if c.cacheStore == nil {
		return nil, &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "storage da fila ausente"}
	}
	host, err := c.providerHost(source)
	if err != nil {
		return nil, err
	}
	id := "http-provider-" + principalHash(source, host)
	for {
		var release func()
		if shared {
			release, err = c.cacheStore.LockShared(id)
		} else {
			release, err = c.cacheStore.Lock(id)
		}
		if err == nil {
			return release, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return nil, &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "falha ao coordenar identidade da fonte"}
		}
		if err := waitUntil(ctx, time.Now().Add(25*time.Millisecond)); err != nil {
			return nil, err
		}
	}
}

func (c *Client) ensurePrincipal(ctx context.Context, source string) error {
	if !c.resolvePrincipals {
		return nil
	}
	var alreadyBound bool
	err := c.Secrets.WithSecret(ctx, source, func(secret []byte) error {
		binding, found, lookupErr := c.bindingFor(source, secret)
		alreadyBound = found && !principalBindingNeedsRefresh(binding)
		return lookupErr
	})
	if err != nil || alreadyBound {
		return err
	}
	release, err := c.lockProvider(ctx, source, false)
	if err != nil {
		return err
	}
	defer release()
	return c.Secrets.WithSecret(ctx, source, func(secret []byte) error {
		previous, found, err := c.bindingFor(source, secret)
		if err != nil || (found && !principalBindingNeedsRefresh(previous)) {
			return err
		}
		if err := c.waitProviderRateHold(ctx, source); err != nil {
			return err
		}
		lookupCtx := context.WithValue(ctx, principalLookupKey{}, true)
		var id, login string
		if source == "github" {
			endpoint, err := url.JoinPath(c.GitHubURL, "user")
			if err != nil {
				return &contract.ErrorResponse{Code: "ERR_UNSUPPORTED_SOURCE", Message: "host da fonte inválido"}
			}
			body, _, _, err := c.GetGitHub(lookupCtx, endpoint)
			if err != nil {
				return err
			}
			var actor struct {
				ID    int64  `json:"id"`
				Login string `json:"login"`
			}
			if json.Unmarshal(body, &actor) != nil || actor.ID <= 0 || (actor.Login != "" && !validGitHubLogin(actor.Login)) {
				return &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "identidade GitHub inválida"}
			}
			id = strconv.FormatInt(actor.ID, 10)
			login = actor.Login
		} else {
			body, err := c.LinearQuery(lookupCtx, "query PocketCliViewer { viewer { id } }", nil)
			if err != nil {
				return err
			}
			var actor struct {
				Viewer struct {
					ID string `json:"id"`
				} `json:"viewer"`
			}
			if json.Unmarshal(body, &actor) != nil || !validPrincipalID(actor.Viewer.ID) {
				return &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "identidade Linear inválida"}
			}
			id = actor.Viewer.ID
		}
		host, err := c.providerHost(source)
		if err != nil {
			return err
		}
		if found && previous.PrincipalID != id {
			return &contract.ErrorResponse{Code: "ERR_PLAN_CHANGED", Message: "identidade da credencial mudou"}
		}
		binding := principalBinding{CredentialKey: principalCredentialKey(source, host, secret), Source: source, Host: host, PrincipalID: id, PrincipalKey: principalHash(source, host, id), Login: login, Version: 1}
		if err := c.mergeLegacyRateHold(ctx, source, secret, binding.PrincipalKey); err != nil {
			return err
		}
		if err := c.cacheStore.Put(principalBindingKind, binding.CredentialKey, binding); err != nil {
			return &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "falha ao persistir identidade da fonte"}
		}
		return nil
	})
}

func validPrincipalID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, ch := range id {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			continue
		}
		return false
	}
	return true
}

func principalBindingNeedsRefresh(binding principalBinding) bool {
	return binding.Source == "github" && binding.Version == 0
}

func validGitHubLogin(login string) bool {
	if login == "" || len(login) > 39 || login[0] == '-' || login[len(login)-1] == '-' {
		return false
	}
	for _, ch := range login {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' {
			continue
		}
		return false
	}
	return true
}

func (c *Client) mergeLegacyRateHold(ctx context.Context, source string, secret []byte, principalKey string) error {
	legacyKey := rateHoldKey(source, secret)
	var legacy rateHold
	err := c.cacheStore.Get(rateHoldKind, legacyKey, &legacy)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || legacy.Key != legacyKey || legacy.Source != source || legacy.Until.IsZero() {
		return &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "espera da credencial corrompida"}
	}
	var shared rateHold
	err = c.cacheStore.Get(rateHoldKind, principalKey, &shared)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "espera do principal corrompida"}
	}
	if err == nil && (shared.Key != principalKey || shared.Source != source || shared.Until.IsZero()) {
		return &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "espera do principal corrompida"}
	}
	if err == nil && !legacy.Until.After(shared.Until) {
		return nil
	}
	if err := c.cacheStore.Put(rateHoldKind, principalKey, rateHold{Key: principalKey, Source: source, Until: legacy.Until}); err != nil {
		return &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "falha ao migrar espera do principal"}
	}
	if err := c.saveProviderRateHold(ctx, source, time.Until(legacy.Until)); err != nil {
		return err
	}
	return nil
}

func (c *Client) PrincipalID(ctx context.Context, source string) (string, error) {
	if !c.resolvePrincipals {
		return "", nil
	}
	var id string
	err := c.Secrets.WithSecret(ctx, source, func(secret []byte) error {
		binding, found, err := c.bindingFor(source, secret)
		if err != nil {
			return err
		}
		if found {
			id = binding.PrincipalID
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("principal não resolvido")
	}
	return id, nil
}

func (c *Client) PrincipalLogin(ctx context.Context, source string) (string, error) {
	if !c.resolvePrincipals {
		return "", nil
	}
	var login string
	err := c.Secrets.WithSecret(ctx, source, func(secret []byte) error {
		binding, found, err := c.bindingFor(source, secret)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("principal não resolvido")
		}
		login = binding.Login
		return nil
	})
	return login, err
}

func (c *Client) PrincipalLookupNeeded(ctx context.Context, source string) (bool, error) {
	if !c.resolvePrincipals {
		return false, nil
	}
	var needed bool
	err := c.Secrets.WithSecret(ctx, source, func(secret []byte) error {
		binding, found, err := c.bindingFor(source, secret)
		needed = !found || principalBindingNeedsRefresh(binding)
		return err
	})
	return needed, err
}

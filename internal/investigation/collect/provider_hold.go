package collect

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"

	"pocketcli/internal/investigation/contract"
)

const providerHoldKind = "providerholds"

func (c *Client) providerHoldKey(source string) (string, error) {
	host, err := c.providerHost(source)
	if err != nil {
		return "", err
	}
	return principalHash(source, host), nil
}

func (c *Client) readProviderHold(source, key string) (rateHold, bool, error) {
	var hold rateHold
	err := c.cacheStore.Get(providerHoldKind, key, &hold)
	if errors.Is(err, os.ErrNotExist) {
		return rateHold{}, false, nil
	}
	if err != nil || hold.Key != key || hold.Source != source || hold.Until.IsZero() {
		return rateHold{}, false, &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "espera do fornecedor corrompida"}
	}
	return hold, true, nil
}

func (c *Client) waitProviderRateHold(ctx context.Context, source string) error {
	key, err := c.providerHoldKey(source)
	if err != nil {
		return err
	}
	hold, found, err := c.readProviderHold(source, key)
	if err != nil || !found {
		return err
	}
	remaining := time.Until(hold.Until)
	if remaining <= 0 {
		return nil
	}
	if remaining > 30*time.Second {
		return &contract.ErrorResponse{Code: "ERR_RATE_LIMITED", Message: "nova credencial aguarda limite do fornecedor antes de identificar a conta", Retryable: true}
	}
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < remaining {
		return &contract.ErrorResponse{Code: "ERR_RATE_LIMITED", Message: "identificação da conta excede sessão restante", Retryable: true}
	}
	return waitUntil(ctx, hold.Until)
}

func (c *Client) saveProviderRateHold(ctx context.Context, source string, delay time.Duration) error {
	if !c.resolvePrincipals || delay <= 0 {
		return nil
	}
	key, err := c.providerHoldKey(source)
	if err != nil {
		return err
	}
	lockID := "http-provider-hold-" + key
	var release func()
	for {
		release, err = c.cacheStore.Lock(lockID)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "falha ao coordenar espera do fornecedor"}
		}
		if err := waitUntil(ctx, time.Now().Add(25*time.Millisecond)); err != nil {
			return err
		}
	}
	defer release()
	previous, found, err := c.readProviderHold(source, key)
	if err != nil {
		return err
	}
	until := time.Now().UTC().Add(delay)
	if found && previous.Until.After(until) {
		until = previous.Until
	}
	if err := c.cacheStore.Put(providerHoldKind, key, rateHold{Key: key, Source: source, Until: until}); err != nil {
		return &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "falha ao auditar espera do fornecedor"}
	}
	return nil
}

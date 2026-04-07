package router

import (
	stdctx "context"
	"errors"
	"fmt"
	"strings"
	"time"

	"pocketcli/internal/contextcollector"
)

const (
	ModeLocal  = "local"
	ModeAuto   = "auto"
	ModeRemote = "remote"

	BackendLocal  = "local"
	BackendRemote = "remote"
	BackendNone   = "none"

	DefaultLocalTimeout  = 8 * time.Second
	DefaultRemoteTimeout = 15 * time.Second
)

var (
	ErrInvalidMode = errors.New("modo inválido")
	ErrConnection  = errors.New("erro de conexão")
)

type Probe func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error

type Request struct {
	Context       contextcollector.TaskContext
	LocalProbe    Probe
	RemoteProbe   Probe
	LocalTimeout  time.Duration
	RemoteTimeout time.Duration
}

type Router struct {
	LocalProbe    Probe
	RemoteProbe   Probe
	LocalTimeout  time.Duration
	RemoteTimeout time.Duration
}

type Decision struct {
	SelectedBackend     string  `json:"selected_backend"`
	FallbackOccurred    bool    `json:"fallback_occurred"`
	Reason              string  `json:"reason"`
	NotifyUser          bool    `json:"notify_user"`
	NotificationMessage *string `json:"notification_message"`
}

func Route(mode string, request Request) (Decision, error) {
	router := Router{
		LocalProbe:    request.LocalProbe,
		RemoteProbe:   request.RemoteProbe,
		LocalTimeout:  request.LocalTimeout,
		RemoteTimeout: request.RemoteTimeout,
	}

	return router.Route(mode, request.Context)
}

func (r Router) Route(mode string, collectedContext contextcollector.TaskContext) (Decision, error) {
	normalizedMode, err := normalizeMode(mode)
	if err != nil {
		return Decision{}, err
	}

	r = r.withDefaults()

	switch normalizedMode {
	case ModeLocal:
		return r.routeLocal(collectedContext), nil
	case ModeRemote:
		return r.routeRemote(collectedContext), nil
	case ModeAuto:
		return r.routeAuto(collectedContext), nil
	default:
		return Decision{}, fmt.Errorf("%w: %s", ErrInvalidMode, normalizedMode)
	}
}

func (r Router) routeLocal(collectedContext contextcollector.TaskContext) Decision {
	if err := probeBackend(r.LocalTimeout, r.LocalProbe, collectedContext); err != nil {
		return newDecision(
			BackendNone,
			false,
			"backend local indisponível — modo local não permite fallback",
			true,
		)
	}

	return newDecision(BackendLocal, false, "modo local", false)
}

func (r Router) routeRemote(collectedContext contextcollector.TaskContext) Decision {
	if err := probeBackend(r.RemoteTimeout, r.RemoteProbe, collectedContext); err != nil {
		return newDecision(BackendNone, false, "backend remoto indisponível", true)
	}

	return newDecision(BackendRemote, false, "modo remote", false)
}

func (r Router) routeAuto(collectedContext contextcollector.TaskContext) Decision {
	err := probeBackend(r.LocalTimeout, r.LocalProbe, collectedContext)
	switch classifyProbeError(err) {
	case probeOK:
		return newDecision(BackendLocal, false, "local disponível", false)
	case probeTimeout:
		return r.routeAutoFallback(collectedContext, "local timeout")
	case probeConnectionError:
		return r.routeAutoFallback(collectedContext, "local indisponível")
	default:
		return r.routeAutoFallback(collectedContext, "local indisponível")
	}
}

func (r Router) routeAutoFallback(collectedContext contextcollector.TaskContext, reason string) Decision {
	if err := probeBackend(r.RemoteTimeout, r.RemoteProbe, collectedContext); err != nil {
		return newDecision(BackendNone, true, "nenhum backend disponível — exibindo contexto coletado", true)
	}

	return newDecision(BackendRemote, true, reason, true)
}

func (r Router) withDefaults() Router {
	if r.LocalTimeout <= 0 {
		r.LocalTimeout = DefaultLocalTimeout
	}
	if r.RemoteTimeout <= 0 {
		r.RemoteTimeout = DefaultRemoteTimeout
	}
	return r
}

type probeState int

const (
	probeOK probeState = iota
	probeTimeout
	probeConnectionError
	probeUnavailable
)

func classifyProbeError(err error) probeState {
	switch {
	case err == nil:
		return probeOK
	case errors.Is(err, stdctx.DeadlineExceeded):
		return probeTimeout
	case errors.Is(err, ErrConnection):
		return probeConnectionError
	default:
		return probeUnavailable
	}
}

func probeBackend(timeout time.Duration, probe Probe, collectedContext contextcollector.TaskContext) error {
	if probe == nil {
		return errors.New("probe não configurado")
	}

	ctx, cancel := stdctx.WithTimeout(stdctx.Background(), timeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- probe(ctx, collectedContext)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func normalizeMode(mode string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(mode))
	if normalized == "" {
		return ModeAuto, nil
	}

	switch normalized {
	case ModeLocal, ModeAuto, ModeRemote:
		return normalized, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrInvalidMode, mode)
	}
}

func newDecision(selectedBackend string, fallbackOccurred bool, reason string, notifyUser bool) Decision {
	decision := Decision{
		SelectedBackend:  selectedBackend,
		FallbackOccurred: fallbackOccurred,
		Reason:           reason,
		NotifyUser:       notifyUser,
	}

	if notifyUser {
		message := formatNotification(selectedBackend, reason)
		decision.NotificationMessage = &message
	}

	return decision
}

func formatNotification(selectedBackend, reason string) string {
	return fmt.Sprintf("[backend: %s — %s]", selectedBackend, reason)
}

package router

import (
	stdctx "context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"pocketcli/internal/contextcollector"
)

func TestRouteRT01LocalModeUsesLocalWhenAvailable(t *testing.T) {
	var localCalls int
	var remoteCalls int

	decision, err := Route(ModeLocal, Request{
		Context: contextcollector.TaskContext{},
		LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			localCalls++
			return nil
		},
		RemoteProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			remoteCalls++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}

	if decision.SelectedBackend != BackendLocal {
		t.Fatalf("expected selected_backend local, got %q", decision.SelectedBackend)
	}
	if decision.FallbackOccurred {
		t.Fatal("expected no fallback in local mode")
	}
	if decision.NotifyUser {
		t.Fatal("expected no user notification when local backend is available")
	}
	if localCalls != 1 {
		t.Fatalf("expected local probe once, got %d", localCalls)
	}
	if remoteCalls != 0 {
		t.Fatalf("expected remote probe not to run, got %d calls", remoteCalls)
	}
}

func TestRouteRT02LocalModeReturnsControlledErrorWithoutFallback(t *testing.T) {
	var remoteCalls int

	decision, err := Route(ModeLocal, Request{
		Context: contextcollector.TaskContext{},
		LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			return errors.New("local off")
		},
		RemoteProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			remoteCalls++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}

	if decision.SelectedBackend != BackendNone {
		t.Fatalf("expected selected_backend none, got %q", decision.SelectedBackend)
	}
	if decision.FallbackOccurred {
		t.Fatal("expected no fallback in local forced mode")
	}
	if decision.Reason != "backend local indisponível — modo local não permite fallback" {
		t.Fatalf("unexpected reason: %q", decision.Reason)
	}
	if !decision.NotifyUser {
		t.Fatal("expected user notification when local backend is unavailable")
	}
	if got := notification(decision); got != "[backend: none — backend local indisponível — modo local não permite fallback]" {
		t.Fatalf("unexpected notification: %q", got)
	}
	if remoteCalls != 0 {
		t.Fatalf("expected remote probe not to run, got %d calls", remoteCalls)
	}
}

func TestRouteRT03RemoteModeUsesRemoteWithoutCheckingLocal(t *testing.T) {
	var localCalls int
	var remoteCalls int

	decision, err := Route(ModeRemote, Request{
		Context: contextcollector.TaskContext{},
		LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			localCalls++
			return nil
		},
		RemoteProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			remoteCalls++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}

	if decision.SelectedBackend != BackendRemote {
		t.Fatalf("expected selected_backend remote, got %q", decision.SelectedBackend)
	}
	if decision.FallbackOccurred {
		t.Fatal("expected no fallback in remote mode")
	}
	if localCalls != 0 {
		t.Fatalf("expected local probe not to run, got %d calls", localCalls)
	}
	if remoteCalls != 1 {
		t.Fatalf("expected remote probe once, got %d calls", remoteCalls)
	}
}

func TestRouteRT04RemoteModeReturnsControlledErrorWithoutFallback(t *testing.T) {
	var localCalls int

	decision, err := Route(ModeRemote, Request{
		Context: contextcollector.TaskContext{},
		LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			localCalls++
			return nil
		},
		RemoteProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			return errors.New("remote off")
		},
	})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}

	if decision.SelectedBackend != BackendNone {
		t.Fatalf("expected selected_backend none, got %q", decision.SelectedBackend)
	}
	if decision.FallbackOccurred {
		t.Fatal("expected no fallback in remote forced mode")
	}
	if decision.Reason != "backend remoto indisponível" {
		t.Fatalf("unexpected reason: %q", decision.Reason)
	}
	if !decision.NotifyUser {
		t.Fatal("expected user notification when remote backend is unavailable")
	}
	if got := notification(decision); got != "[backend: none — backend remoto indisponível]" {
		t.Fatalf("unexpected notification: %q", got)
	}
	if localCalls != 0 {
		t.Fatalf("expected local probe not to run, got %d calls", localCalls)
	}
}

func TestRouteRT05AutoModeUsesLocalWhenBackendRespondsWithinTimeout(t *testing.T) {
	var remoteCalls int

	decision, err := Route(ModeAuto, Request{
		Context: contextcollector.TaskContext{},
		LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("expected deadline on local probe context")
			}

			remaining := time.Until(deadline)
			if remaining > DefaultLocalTimeout || remaining < DefaultLocalTimeout-250*time.Millisecond {
				t.Fatalf("expected local timeout near %s, got %s", DefaultLocalTimeout, remaining)
			}

			return nil
		},
		RemoteProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			remoteCalls++
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}

	if decision.SelectedBackend != BackendLocal {
		t.Fatalf("expected selected_backend local, got %q", decision.SelectedBackend)
	}
	if decision.FallbackOccurred {
		t.Fatal("expected no fallback when local backend is available")
	}
	if decision.NotifyUser {
		t.Fatal("expected no notification when auto mode keeps local backend")
	}
	if remoteCalls != 0 {
		t.Fatalf("expected remote probe not to run, got %d calls", remoteCalls)
	}
}

func TestRouteRT06AutoModeFallsBackToRemoteAfterLocalTimeout(t *testing.T) {
	var remoteCalls int

	decision, err := Route(ModeAuto, Request{
		Context:      contextcollector.TaskContext{},
		LocalTimeout: 20 * time.Millisecond,
		RemoteProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			remoteCalls++
			return nil
		},
		LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}

	if decision.SelectedBackend != BackendRemote {
		t.Fatalf("expected selected_backend remote, got %q", decision.SelectedBackend)
	}
	if !decision.FallbackOccurred {
		t.Fatal("expected fallback when local backend times out")
	}
	if decision.Reason != "local timeout" {
		t.Fatalf("unexpected reason: %q", decision.Reason)
	}
	if !decision.NotifyUser {
		t.Fatal("expected notification for timeout fallback")
	}
	if got := notification(decision); got != "[backend: remote — local timeout]" {
		t.Fatalf("unexpected notification: %q", got)
	}
	if remoteCalls != 1 {
		t.Fatalf("expected remote probe once, got %d calls", remoteCalls)
	}
}

func TestRouteRT07AutoModeFallsBackImmediatelyOnConnectionError(t *testing.T) {
	startedAt := time.Now()

	decision, err := Route(ModeAuto, Request{
		Context:      contextcollector.TaskContext{},
		LocalTimeout: 250 * time.Millisecond,
		LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			return fmt.Errorf("dial failed: %w", ErrConnection)
		},
		RemoteProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}

	if elapsed := time.Since(startedAt); elapsed >= 150*time.Millisecond {
		t.Fatalf("expected immediate fallback before timeout, got %s", elapsed)
	}
	if decision.SelectedBackend != BackendRemote {
		t.Fatalf("expected selected_backend remote, got %q", decision.SelectedBackend)
	}
	if !decision.FallbackOccurred {
		t.Fatal("expected fallback when local backend returns connection error")
	}
	if decision.Reason != "local indisponível" {
		t.Fatalf("unexpected reason: %q", decision.Reason)
	}
	if got := notification(decision); got != "[backend: remote — local indisponível]" {
		t.Fatalf("unexpected notification: %q", got)
	}
}

func TestRouteRT08AutoModeDegradesWhenBothBackendsAreUnavailable(t *testing.T) {
	decision, err := Route(ModeAuto, Request{
		Context: contextcollector.TaskContext{},
		LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			return ErrConnection
		},
		RemoteProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			return errors.New("remote off")
		},
	})
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}

	if decision.SelectedBackend != BackendNone {
		t.Fatalf("expected selected_backend none, got %q", decision.SelectedBackend)
	}
	if !decision.FallbackOccurred {
		t.Fatal("expected fallback path when both backends are unavailable")
	}
	if decision.Reason != "nenhum backend disponível — exibindo contexto coletado" {
		t.Fatalf("unexpected reason: %q", decision.Reason)
	}
	if !decision.NotifyUser {
		t.Fatal("expected notification when router degrades to collected context")
	}
	if got := notification(decision); got != "[backend: none — nenhum backend disponível — exibindo contexto coletado]" {
		t.Fatalf("unexpected notification: %q", got)
	}
}

func TestRouteRT09IsDeterministicAcrossRepeatedExecutions(t *testing.T) {
	request := Request{
		Context: contextcollector.TaskContext{},
		LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			return ErrConnection
		},
		RemoteProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
			return nil
		},
	}

	first, err := Route(ModeAuto, request)
	if err != nil {
		t.Fatalf("Route returned error: %v", err)
	}

	expected := comparableDecision(first)
	for i := 0; i < 100; i++ {
		decision, err := Route(ModeAuto, request)
		if err != nil {
			t.Fatalf("Route returned error on run %d: %v", i+1, err)
		}
		if !reflect.DeepEqual(expected, comparableDecision(decision)) {
			t.Fatalf("decision changed on run %d: %#v", i+1, decision)
		}
	}
}

func TestRouteRT10FormatsNotificationWhenBackendDiffersFromRequestedMode(t *testing.T) {
	cases := []struct {
		name     string
		mode     string
		request  Request
		expected string
	}{
		{
			name: "local unavailable",
			mode: ModeLocal,
			request: Request{
				Context: contextcollector.TaskContext{},
				LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
					return errors.New("local off")
				},
			},
			expected: "[backend: none — backend local indisponível — modo local não permite fallback]",
		},
		{
			name: "remote unavailable",
			mode: ModeRemote,
			request: Request{
				Context: contextcollector.TaskContext{},
				RemoteProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
					return errors.New("remote off")
				},
			},
			expected: "[backend: none — backend remoto indisponível]",
		},
		{
			name: "auto local timeout",
			mode: ModeAuto,
			request: Request{
				Context:      contextcollector.TaskContext{},
				LocalTimeout: 20 * time.Millisecond,
				LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
					<-ctx.Done()
					return ctx.Err()
				},
				RemoteProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
					return nil
				},
			},
			expected: "[backend: remote — local timeout]",
		},
		{
			name: "auto local unavailable",
			mode: ModeAuto,
			request: Request{
				Context: contextcollector.TaskContext{},
				LocalProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
					return ErrConnection
				},
				RemoteProbe: func(ctx stdctx.Context, collectedContext contextcollector.TaskContext) error {
					return nil
				},
			},
			expected: "[backend: remote — local indisponível]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision, err := Route(tc.mode, tc.request)
			if err != nil {
				t.Fatalf("Route returned error: %v", err)
			}

			if !decision.NotifyUser {
				t.Fatal("expected notification for mismatched backend")
			}

			firstLine := notification(decision)
			if firstLine != tc.expected {
				t.Fatalf("expected notification %q, got %q", tc.expected, firstLine)
			}
		})
	}
}

func notification(decision Decision) string {
	if decision.NotificationMessage == nil {
		return ""
	}
	return *decision.NotificationMessage
}

type decisionSnapshot struct {
	SelectedBackend  string
	FallbackOccurred bool
	Reason           string
	NotifyUser       bool
	Notification     string
}

func comparableDecision(decision Decision) decisionSnapshot {
	return decisionSnapshot{
		SelectedBackend:  decision.SelectedBackend,
		FallbackOccurred: decision.FallbackOccurred,
		Reason:           decision.Reason,
		NotifyUser:       decision.NotifyUser,
		Notification:     notification(decision),
	}
}

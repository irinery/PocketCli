package safety

import "testing"

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

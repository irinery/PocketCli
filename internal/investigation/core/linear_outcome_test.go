package core

import "testing"

// Estados reais do workspace Linear "Somos UNI" (teams ENG, PRD, SUP, AIF e
// CAD), consultados em 27/10/2026. Cada team tem um estado de tipo
// "duplicate"; nomes em português não podem mudar a classificação.
func TestLinearOutcomeUsesRealWorkspaceStateTypes(t *testing.T) {
	cases := []struct{ stateType, name, want string }{
		{"duplicate", "Duplicate", "duplicate"},
		{"duplicate", "Duplicada", "duplicate"},
		{"canceled", "Duplicate", "duplicate"},
		{"completed", "Finalizado/Merged", "completed"},
		{"completed", "Finalizado", "completed"},
		{"completed", "Done", "completed"},
		{"canceled", "Cancelado", "cancelled"},
		{"started", "Reprovado", "active"},
		{"started", "Pronto p/ Merge", "active"},
		{"unstarted", "Bloqueada", "active"},
		{"backlog", "Planned", "active"},
		{"triage", "Triage", "active"},
		{"", "Finalizado", "unclassified"},
		{"unknown_type", "Done", "unclassified"},
	}
	for _, tc := range cases {
		if got := linearIssueOutcome(tc.stateType, tc.name); got != tc.want {
			t.Fatalf("linearIssueOutcome(%q, %q) = %q, esperado %q", tc.stateType, tc.name, got, tc.want)
		}
	}
	if !terminalState("duplicate") {
		t.Fatal("tipo duplicate deve ser terminal")
	}
}

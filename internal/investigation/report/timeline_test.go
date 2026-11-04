package report

import (
	"strings"
	"testing"

	"pocketcli/internal/investigation/insightcontract"
)

func TestDecisionTimelinePDFDistinguishesHypothesisAndExecutor(t *testing.T) {
	l := newPDFLayout()
	renderCaseDetail(l, insightcontract.CaseDetail{Case: insightcontract.DecisionCase{ID: "synthetic-case"}, Timeline: []insightcontract.TimelineItem{{ID: "change", Kind: "observation", Summary: "Estado alterado", ActorLabel: "Pessoa A", Rationale: "Pode indicar adiamento.", RationaleBasis: "hypothesis", Confidence: "low", Alternatives: []string{"Pode ser correção administrativa."}, EvidenceIDs: []string{"synthetic-proof"}}}})
	text := l.text()
	for _, want := range []string{"Hipótese de intenção (confiança baixa)", "Executor registrado: Pessoa A", "Outra explicação possível", "synthetic-proof", "data/ordem desconhecida"} {
		if !strings.Contains(text, want) {
			t.Fatalf("PDF omitted %q", want)
		}
	}
	if strings.Contains(text, "Motivo declarado na fonte") {
		t.Fatal("hypothesis became declared reason")
	}
}

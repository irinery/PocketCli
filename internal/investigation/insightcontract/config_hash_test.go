package insightcontract

import (
	"encoding/json"
	"os"
	"testing"
)

func TestConfigHashCanonicalizesSetsAndIncludesSnapshotVersions(t *testing.T) {
	left := AnalysisConfig{
		Version: Version,
		ID:      "cfg",
		RequestedScope: Scope{Resources: []ResourceRef{
			{Source: "linear", ID: "team-b"},
			{Source: "github", ID: "repo-a"},
		}},
		Objectives: []Objective{
			{ID: "objective-b", EntityIDs: []string{"e2", "e1", "e1"}, EvidenceIDs: []string{"v2", "v1"}},
			{ID: "objective-a"},
		},
	}
	right := left
	right.RequestedScope.Resources = []ResourceRef{{Source: "github", ID: "repo-a"}, {Source: "linear", ID: "team-b"}}
	right.Objectives = []Objective{
		{ID: "objective-a"},
		{ID: "objective-b", EntityIDs: []string{"e1", "e2"}, EvidenceIDs: []string{"v1", "v2"}},
	}
	first, err := ConfigHash(left, "schema-1", "catalog-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ConfigHash(right, "schema-1", "catalog-1")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("reordenação de campos tipo-set alterou hash: %s != %s", first, second)
	}
	for _, versions := range [][2]string{{"schema-2", "catalog-1"}, {"schema-1", "catalog-2"}} {
		changed, err := ConfigHash(left, versions[0], versions[1])
		if err != nil {
			t.Fatal(err)
		}
		if changed == first {
			t.Fatalf("mudar versões do snapshot não alterou hash: %v", versions)
		}
	}
}

func TestPolicyHashChangesWithoutVisibleContentChange(t *testing.T) {
	base := Policy{ID: "policy", Version: "1", ExcerptMaxRunes: 100, ContentMode: "local_excerpts"}
	first, err := PolicyHash(base)
	if err != nil || len(first) != 64 {
		t.Fatalf("hash: %q %v", first, err)
	}
	again, _ := PolicyHash(base)
	if again != first {
		t.Fatal("hash da mesma política não é estável")
	}
	base.Version = "2"
	changed, _ := PolicyHash(base)
	if changed == first {
		t.Fatal("versão diferente de política reutilizou identidade")
	}
}

func TestPolicyHashMatchesSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/fixture_go.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Config AnalysisConfig  `json:"config"`
		Report ExecutiveReport `json:"report"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	got, err := PolicyHash(fixture.Config.Policy)
	if err != nil || got != fixture.Report.PolicyHash {
		t.Fatalf("fixture policy hash divergente: got=%s expected=%s err=%v", got, fixture.Report.PolicyHash, err)
	}
	if len(fixture.Report.Objectives) != len(fixture.Config.Objectives) {
		t.Fatal("fixture perdeu metadados de objetivos")
	}
}

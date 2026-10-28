package insightcontract

import "testing"

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

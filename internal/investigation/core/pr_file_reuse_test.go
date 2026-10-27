package core

import (
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
)

func TestRefreshKeepsOldHeadFileMappings(t *testing.T) {
	prior := query.Dataset{
		Components: []contract.ComponentRecord{
			{ID: "old-file", Kind: "file_change", Path: "services/api/handler.go"},
			{ID: "old-manifest", Kind: "go_module", Path: "services/api/go.mod"},
			{ID: "unrelated-file", Kind: "file_change", Path: "other.go"},
			{ID: "future-manifest", Kind: "go_module", Path: "services/api-next/go.mod"},
		},
		Links: []contract.LinkRecord{
			{ID: "pr-file", SourceID: "pr-1", Relation: "modifies_file", TargetID: "old-file", EvidenceID: "ev-pr-file"},
			{ID: "file-component", SourceID: "old-file", Relation: "maps_to_component", TargetID: "old-manifest", ValidFrom: "2026-01-01T00:00:00Z", EvidenceID: "ev-file-component"},
			{ID: "future-component", SourceID: "old-file", Relation: "maps_to_component", TargetID: "future-manifest", ValidFrom: "2026-03-01T00:00:00Z", EvidenceID: "ev-future"},
			{ID: "other-pr-file", SourceID: "pr-2", Relation: "modifies_file", TargetID: "unrelated-file", EvidenceID: "ev-other"},
		},
		Events: []query.Event{
			{ID: "old-rename", EntityID: "old-file", Type: "file_rename_reported", EventAt: "2026-01-04T00:00:00Z", ObservedAt: "2026-01-10T00:00:00Z", OldValue: "services/api/old.go", NewValue: "services/api/handler.go", EvidenceID: "ev-rename"},
			{ID: "unrelated-rename", EntityID: "unrelated-file", Type: "file_rename_reported", ObservedAt: "2026-01-10T00:00:00Z", EvidenceID: "ev-unrelated-rename"},
		},
	}
	current := query.Dataset{PullRequests: []query.PullRequest{{ID: "pr-1", HeadSHA: "new-head"}}}
	referenced := make(map[string]bool)
	reusePriorFileGraph(&current, prior, "2026-02-01T00:00:00Z", referenced)
	if len(current.Links) != 2 || current.Links[0].ID != "pr-file" || current.Links[1].ID != "file-component" {
		t.Fatalf("vínculos históricos não preservados ou fora da janela: %+v", current.Links)
	}
	if len(current.Components) != 2 || current.Components[0].ID != "old-file" || current.Components[1].ID != "old-manifest" {
		t.Fatalf("componentes históricos não preservados: %+v", current.Components)
	}
	if len(current.Events) != 1 || current.Events[0].ID != "old-rename" || current.Events[0].EventAt != "" {
		t.Fatalf("rename observado no head antigo não preservado: %+v", current.Events)
	}
	if !referenced["ev-pr-file"] || !referenced["ev-file-component"] || !referenced["ev-rename"] || referenced["ev-future"] || referenced["ev-other"] || referenced["ev-unrelated-rename"] {
		t.Fatalf("evidências históricas incorretas: %+v", referenced)
	}
}

func TestRefreshKeepsCommitFileMappings(t *testing.T) {
	prior := query.Dataset{
		Components: []contract.ComponentRecord{{ID: "commit-file", Kind: "file_change"}, {ID: "commit-manifest", Kind: "go_module"}},
		Links: []contract.LinkRecord{
			{ID: "commit-file-link", SourceID: "commit-event", Relation: "modifies_file", TargetID: "commit-file", EvidenceID: "ev-diff"},
			{ID: "commit-map-link", SourceID: "commit-file", Relation: "maps_to_component", TargetID: "commit-manifest", EvidenceID: "ev-map"},
		},
	}
	current := query.Dataset{Events: []query.Event{{ID: "commit-event", Type: "commit_reachable_observed"}}}
	referenced := make(map[string]bool)
	reusePriorFileGraph(&current, prior, "2026-02-01T00:00:00Z", referenced)
	if len(current.Links) != 2 || len(current.Components) != 2 || !referenced["ev-diff"] || !referenced["ev-map"] {
		t.Fatalf("histórico de arquivos do commit perdido: %+v", current)
	}
}

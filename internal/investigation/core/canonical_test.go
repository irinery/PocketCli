package core

import (
	"encoding/json"
	"testing"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

func TestCanonicalDatasetIgnoresCollectionOrder(t *testing.T) {
	base := query.Dataset{
		Issues:       []query.Issue{{ID: "issue-b", Identifier: "T-2"}, {ID: "issue-a", Identifier: "T-1"}},
		PullRequests: []query.PullRequest{{ID: "pr-b"}, {ID: "pr-a"}},
		Evidence:     []contract.Evidence{{ID: "ev-b"}, {ID: "ev-a"}},
		Coverage:     []contract.Coverage{{Source: "linear", ResourceID: "t", Collection: "issues"}, {Source: "github", ResourceID: "r", Collection: "pull_requests"}},
	}
	reordered := query.Dataset{
		Issues:       []query.Issue{base.Issues[1], base.Issues[0]},
		PullRequests: []query.PullRequest{base.PullRequests[1], base.PullRequests[0]},
		Evidence:     []contract.Evidence{base.Evidence[1], base.Evidence[0]},
		Coverage:     []contract.Coverage{base.Coverage[1], base.Coverage[0]},
	}
	canonicalizeDataset(&base)
	canonicalizeDataset(&reordered)
	a, _ := json.Marshal(base)
	b, _ := json.Marshal(reordered)
	if storage.Hash(a) != storage.Hash(b) {
		t.Fatalf("dataset hash changed with API order: %s vs %s", storage.Hash(a), storage.Hash(b))
	}
}

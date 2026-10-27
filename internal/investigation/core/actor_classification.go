package core

import (
	"encoding/json"
	"fmt"
	"strings"

	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

func (c *Core) verifiedRetainedEvidence(id, hash string) (json.RawMessage, error) {
	if id == "" || hash == "" {
		return nil, fault("ERR_AUDIT_FAILED", "hash de evidência anterior indisponível")
	}
	var raw json.RawMessage
	if c.store.Get("objects", id, &raw) != nil || !json.Valid(raw) || storage.Hash(raw) != hash {
		return nil, fault("ERR_AUDIT_FAILED", "objeto de evidência anterior corrompido")
	}
	return raw, nil
}

func (c *Core) classifyRetainedPR(pr query.PullRequest, evidenceHash string) (query.PullRequest, error) {
	if pr.AuthorKind != "" || !strings.HasPrefix(pr.AuthorID, "github-") || pr.EvidenceID == "" {
		return pr, nil
	}
	raw, err := c.verifiedRetainedEvidence(pr.EvidenceID, evidenceHash)
	if err != nil {
		return pr, err
	}
	var item struct {
		User struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"user"`
	}
	if json.Unmarshal(raw, &item) != nil {
		return pr, fault("ERR_AUDIT_FAILED", "evidência de autor do PR inválida")
	}
	pr.AuthorID, pr.AuthorKind = "", ""
	if item.User.ID > 0 {
		pr.AuthorID = fmt.Sprintf("github-%d", item.User.ID)
		pr.AuthorKind = githubAccountKind(item.User.Type)
	}
	return pr, nil
}

func (c *Core) classifyRetainedActorEvent(event query.Event, prs []query.PullRequest, evidenceHash string) (query.Event, error) {
	if event.ActorKind != "" || !strings.HasPrefix(event.ActorID, "github-") || event.EvidenceID == "" {
		return event, nil
	}
	raw, err := c.verifiedRetainedEvidence(event.EvidenceID, evidenceHash)
	if err != nil {
		return event, err
	}
	isReview := event.Type == "reviewed" || event.Type == "human_reviewed" || event.Type == "bot_reviewed" || event.Type == "reviewed_unknown"
	if isReview {
		var item struct {
			State string `json:"state"`
			User  struct {
				ID   int64  `json:"id"`
				Type string `json:"type"`
			} `json:"user"`
		}
		if json.Unmarshal(raw, &item) != nil {
			return event, fault("ERR_AUDIT_FAILED", "evidência de revisão GitHub inválida")
		}
		event.ActorID, event.ActorKind = "", ""
		if item.User.ID > 0 {
			event.ActorID = fmt.Sprintf("github-%d", item.User.ID)
			event.ActorKind = githubAccountKind(item.User.Type)
		}
		author := ""
		for _, pr := range prs {
			if pr.ID == event.EntityID {
				author = pr.AuthorID
				break
			}
		}
		event.Type = githubReviewEventType(event.ActorID, event.ActorKind, author, item.State)
		return event, nil
	}
	var item struct {
		Actor struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"actor"`
	}
	if json.Unmarshal(raw, &item) != nil {
		return event, fault("ERR_AUDIT_FAILED", "evidência de timeline GitHub inválida")
	}
	event.ActorID, event.ActorKind = "", ""
	if item.Actor.ID > 0 {
		event.ActorID = fmt.Sprintf("github-%d", item.Actor.ID)
		event.ActorKind = githubAccountKind(item.Actor.Type)
	}
	return event, nil
}

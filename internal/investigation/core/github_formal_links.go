package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

const githubFormalTimelineSelection = `
      timelineItems(first:100,after:$after,itemTypes:[CONNECTED_EVENT,DISCONNECTED_EVENT]) {
        nodes {
          __typename
          ... on ConnectedEvent { id createdAt source { __typename ... on Issue { number repository { nameWithOwner } } ... on PullRequest { number repository { nameWithOwner } } } subject { __typename ... on Issue { number repository { nameWithOwner } } ... on PullRequest { number repository { nameWithOwner } } } }
          ... on DisconnectedEvent { id createdAt source { __typename ... on Issue { number repository { nameWithOwner } } ... on PullRequest { number repository { nameWithOwner } } } subject { __typename ... on Issue { number repository { nameWithOwner } } ... on PullRequest { number repository { nameWithOwner } } } }
        }
        pageInfo { hasNextPage endCursor }
      }`

const githubFormalLinkQuery = `query PocketCliFormalLinks($owner:String!,$repo:String!,$number:Int!,$after:String) {
  repository(owner:$owner,name:$repo) {
    issue(number:$number) {` + githubFormalTimelineSelection + `}
  }
}`

type githubFormalEndpoint struct {
	Type       string `json:"__typename"`
	Number     int    `json:"number"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

type githubFormalEvent struct {
	Type      string               `json:"__typename"`
	ID        string               `json:"id"`
	CreatedAt string               `json:"createdAt"`
	Source    githubFormalEndpoint `json:"source"`
	Subject   githubFormalEndpoint `json:"subject"`
}

const githubFormalTargetLimit = 200
const githubFormalBatchSize = 5

func (c *Core) collectGitHubFormalLinks(ctx context.Context, plan contract.Plan, r contract.Resource, d *query.Dataset, run *contract.Run) error {
	if !plan.Input.Modules.GitHubIssues {
		d.Coverage = append(d.Coverage, c.coverage(r, "formal_links", "not_selected", 0, "módulo GitHub issues não selecionado"))
		return nil
	}
	parts := strings.Split(r.Name, "/")
	if len(parts) != 2 {
		d.Coverage = append(d.Coverage, c.coverage(r, "formal_links", "unsupported", 0, "nome do repositório inválido"))
		return nil
	}
	targets := make([]githubChildTarget, 0)
	for _, issue := range d.Issues {
		if issue.Source != "github" || !strings.HasPrefix(strings.ToLower(issue.Identifier), strings.ToLower(r.Name)+"#") {
			continue
		}
		var number int
		if _, err := fmt.Sscanf(issue.Identifier, r.Name+"#%d", &number); err == nil && number > 0 {
			targets = append(targets, githubChildTarget{Number: number, EntityID: issue.ID})
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].EntityID < targets[j].EntityID })
	status, reason := "complete", ""
	cursorID := ""
	if len(targets) > githubFormalTargetLimit {
		status, reason = "partial", fmt.Sprintf("fila de vínculos formais: %d issues pendentes", len(targets)-githubFormalTargetLimit)
		scopeID := plan.InvestigationID
		if scopeID == "" {
			scopeID = plan.ID
		}
		auth, err := c.currentAuth(ctx, plan.Input)
		if err != nil {
			return err
		}
		cursorID = storage.Hash([]byte(scopeID + ":" + r.ID + ":formal_links:" + auth.GitHub))
		var cursor githubReconcileCursor
		if err := c.store.Get("reconcile", cursorID, &cursor); err != nil && !os.IsNotExist(err) {
			return fault("ERR_AUDIT_FAILED", "cursor de vínculos formais corrompido")
		}
		start := sort.Search(len(targets), func(i int) bool { return targets[i].EntityID > cursor.LastEntityID })
		if start >= len(targets) {
			start = 0
		}
		ordered := append([]githubChildTarget{}, targets[start:]...)
		targets = append(ordered, targets[:start]...)
		targets = targets[:githubFormalTargetLimit]
	}
	prs := make(map[string]query.PullRequest, len(d.PullRequests))
	for _, pr := range d.PullRequests {
		prs[strings.ToLower(fmt.Sprintf("%s#%d", pr.Repository, pr.Number))] = pr
	}
	var observed int64
	lastComplete := ""
	prefetchErrors := make(map[int]error)
	for start := 0; start < len(targets); start += githubFormalBatchSize {
		end := start + githubFormalBatchSize
		if end > len(targets) {
			end = len(targets)
		}
		groupErrors, err := c.prefetchGitHubFormalPages(ctx, plan, r, targets[start:end], parts[0], parts[1], run)
		if err != nil {
			return err
		}
		for number, groupErr := range groupErrors {
			prefetchErrors[number] = groupErr
		}
	}
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return err
		}
		if prefetchErrors[target.Number] != nil {
			status, reason = "partial", "eventos formais indisponíveis ou inválidos"
			continue
		}
		items, complete, err := c.githubFormalPages(ctx, plan, r, target.Number, parts[0], parts[1], run)
		if err != nil {
			var coded *contract.ErrorResponse
			if errors.As(err, &coded) && (coded.Code == "ERR_AUDIT_FAILED" || coded.Code == "ERR_RESOURCE_EXHAUSTED" || coded.Code == "ERR_RATE_LIMITED") {
				return err
			}
			status, reason = "partial", "eventos formais indisponíveis ou inválidos"
		}
		if !complete {
			status, reason = "partial", "paginação de vínculos formais incompleta"
			continue // a later page can contain a disconnection
		}
		lastComplete = target.EntityID
		observed += int64(len(items))
		latest := make(map[string]githubFormalEvent)
		ambiguous := make(map[string]bool)
		invalid := false
		for _, raw := range items {
			var event githubFormalEvent
			if json.Unmarshal(raw, &event) != nil || event.ID == "" || event.CreatedAt == "" || (event.Type != "ConnectedEvent" && event.Type != "DisconnectedEvent") {
				status, reason = "partial", "evento formal inválido"
				invalid = true
				continue
			}
			eventTime, parseErr := time.Parse(time.RFC3339, event.CreatedAt)
			if parseErr != nil {
				status, reason = "partial", "horário de evento formal inválido"
				invalid = true
				continue
			}
			if !timestampInWindow("", plan.WindowEnd, event.CreatedAt) {
				continue
			}
			issueEndpoint, prEndpoint := event.Source, event.Subject
			if issueEndpoint.Type != "Issue" {
				issueEndpoint, prEndpoint = event.Subject, event.Source
			}
			if issueEndpoint.Type == "Issue" && prEndpoint.Type == "PullRequest" && (issueEndpoint.Repository.NameWithOwner == "" || prEndpoint.Repository.NameWithOwner == "" || issueEndpoint.Number <= 0 || prEndpoint.Number <= 0) {
				status, reason = "partial", "extremos de vínculo formal incompletos"
				invalid = true
				continue
			}
			if issueEndpoint.Type != "Issue" || prEndpoint.Type != "PullRequest" || !strings.EqualFold(issueEndpoint.Repository.NameWithOwner, r.Name) || issueEndpoint.Number != target.Number {
				if event.Source.Type == "" || event.Subject.Type == "" {
					status, reason = "partial", "extremos de vínculo formal indisponíveis"
					invalid = true
				}
				continue
			}
			prKey := strings.ToLower(fmt.Sprintf("%s#%d", prEndpoint.Repository.NameWithOwner, prEndpoint.Number))
			old, exists := latest[prKey]
			oldTime, _ := time.Parse(time.RFC3339, old.CreatedAt)
			if !exists || eventTime.After(oldTime) {
				latest[prKey] = event
				ambiguous[prKey] = false
			} else if eventTime.Equal(oldTime) && event.Type != old.Type {
				ambiguous[prKey] = true
			}
		}
		if invalid {
			continue
		}
		for prKey, event := range latest {
			if ambiguous[prKey] {
				status, reason = "partial", "ordem de conexão e desconexão ambígua"
				continue
			}
			if event.Type != "ConnectedEvent" {
				continue
			}
			pr, ok := prs[prKey]
			if !ok {
				status, reason = "partial", "issue possui vínculo formal com PR fora do escopo coletado"
				continue
			}
			raw, _ := json.Marshal(event)
			evidence, err := c.evidence("github", r.Name+":formal-link:"+event.ID, event.CreatedAt, raw)
			if err != nil {
				return err
			}
			upsertEvidence(d, evidence)
			linkID := "link-" + storage.Hash([]byte(target.EntityID + ":references_pr:" + pr.ID))[:24]
			upsertLink(d, contract.LinkRecord{ID: linkID, SourceID: target.EntityID, Relation: "references_pr", TargetID: pr.ID, Status: "direct", EvidenceID: evidence.ID, ValidFrom: event.CreatedAt})
			run.Items++
			if !c.saveRun(run) {
				return fault("ERR_AUDIT_FAILED", "falha ao auditar vínculo formal")
			}
		}
	}
	if cursorID != "" && lastComplete != "" {
		if err := c.store.Put("reconcile", cursorID, githubReconcileCursor{LastEntityID: lastComplete}); err != nil {
			return fault("ERR_AUDIT_FAILED", "falha ao persistir cursor de vínculos formais")
		}
	}
	d.Coverage = append(d.Coverage, c.coverage(r, "formal_links", status, observed, reason))
	return nil
}

func (c *Core) githubFormalPages(ctx context.Context, plan contract.Plan, r contract.Resource, number int, owner, repo string, run *contract.Run) ([]json.RawMessage, bool, error) {
	collection := "formal_links"
	journalID := storage.Hash([]byte(fmt.Sprintf("%s:%s:%s:%d", plan.ID, r.ID, collection, number)))
	journal := githubJournal{PlanID: plan.ID, ResourceID: r.ID, Collection: collection, Items: []json.RawMessage{}}
	if err := c.store.Get("journals", journalID, &journal); err == nil {
		if journal.PlanID != plan.ID || journal.ResourceID != r.ID || journal.Collection != collection {
			return nil, false, fault("ERR_AUDIT_FAILED", "journal de vínculo incompatível")
		}
	} else if !os.IsNotExist(err) {
		return nil, false, fault("ERR_AUDIT_FAILED", "journal de vínculo corrompido")
	}
	for !journal.Complete && journal.Pages < 10 {
		vars := map[string]any{"owner": owner, "repo": repo, "number": number}
		if journal.Cursor != "" {
			vars["after"] = journal.Cursor
		}
		data, wireBytes, err := c.collector.GitHubQueryWithBytes(ctx, githubFormalLinkQuery, vars)
		run.Requests++
		run.Bytes += wireBytes
		if !c.saveRun(run) {
			return journal.Items, false, fault("ERR_AUDIT_FAILED", "falha ao auditar request GraphQL")
		}
		if run.Bytes > plan.Input.Limits.MaxTransferBytes {
			return journal.Items, false, fault("ERR_RESOURCE_EXHAUSTED", "orçamento de transferência atingido")
		}
		if err != nil {
			return journal.Items, false, err
		}
		var page struct {
			Repository *struct {
				Issue *struct {
					TimelineItems *struct {
						Nodes    []json.RawMessage `json:"nodes"`
						PageInfo *struct {
							HasNextPage bool   `json:"hasNextPage"`
							EndCursor   string `json:"endCursor"`
						} `json:"pageInfo"`
					} `json:"timelineItems"`
				} `json:"issue"`
			} `json:"repository"`
		}
		if json.Unmarshal(data, &page) != nil || page.Repository == nil || page.Repository.Issue == nil || page.Repository.Issue.TimelineItems == nil || page.Repository.Issue.TimelineItems.PageInfo == nil || page.Repository.Issue.TimelineItems.Nodes == nil {
			return journal.Items, false, fmt.Errorf("issue GraphQL indisponível")
		}
		connection := page.Repository.Issue.TimelineItems
		if connection.PageInfo.HasNextPage && connection.PageInfo.EndCursor == "" {
			return journal.Items, false, fmt.Errorf("cursor GraphQL ausente")
		}
		journal.Items = append(journal.Items, connection.Nodes...)
		journal.Pages++
		journal.Complete = !connection.PageInfo.HasNextPage
		journal.Cursor = connection.PageInfo.EndCursor
		if err := c.store.Put("journals", journalID, journal); err != nil {
			return journal.Items, false, fault("ERR_AUDIT_FAILED", "falha ao persistir página GraphQL")
		}
	}
	return journal.Items, journal.Complete, nil
}

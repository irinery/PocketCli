package core

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

var pullURL = regexp.MustCompile(`(?i)https://github\.com/([A-Za-z0-9-]+)/([A-Za-z0-9_.-]+)/pull/([1-9][0-9]*)\b`)
var issueURL = regexp.MustCompile(`(?i)https://github\.com/([A-Za-z0-9-]+)/([A-Za-z0-9_.-]+)/issues/([1-9][0-9]*)\b`)
var linearIdentifier = regexp.MustCompile(`\b[A-Z][A-Z0-9]{1,15}-[1-9][0-9]{0,8}\b`)
var closingReference = regexp.MustCompile(`(?i)\b(?:fixes|closes|resolves)\s+#([1-9][0-9]*)\b`)
var fixingReference = regexp.MustCompile(`(?i)\bfixes\s+#([1-9][0-9]*)\b`)
var pullReference = regexp.MustCompile(`(?i)\b(?:PR|pull request)\s*#([1-9][0-9]*)\b`)
var apiIssueURL = regexp.MustCompile(`(?i)^https://api\.github\.com/repos/([A-Za-z0-9-]+)/([A-Za-z0-9_.-]+)/issues/([1-9][0-9]*)$`)
var apiPullURL = regexp.MustCompile(`(?i)^https://api\.github\.com/repos/([A-Za-z0-9-]+)/([A-Za-z0-9_.-]+)/pulls/([1-9][0-9]*)$`)

func (c *Core) linkDataset(plan contract.Plan, d *query.Dataset) error {
	issues := make(map[string]query.Issue, len(d.Issues))
	prs := make(map[string]query.PullRequest, len(d.PullRequests))
	for _, issue := range d.Issues {
		issues[strings.ToLower(issue.Identifier)] = issue
	}
	for _, pr := range d.PullRequests {
		prs[strings.ToLower(fmt.Sprintf("%s#%d", pr.Repository, pr.Number))] = pr
	}
	linked := make(map[string]bool)
	for _, link := range d.Links {
		linked[link.ID] = true
	}
	addMention := func(issue query.Issue, pr query.PullRequest, evidenceID string) {
		if issue.ID == "" || pr.ID == "" || evidenceID == "" {
			return
		}
		seed := issue.ID + ":mentions:" + pr.ID + ":" + evidenceID
		id := "link-" + storage.Hash([]byte(seed))[:24]
		if linked[id] {
			return
		}
		linked[id] = true
		d.Links = append(d.Links, contract.LinkRecord{ID: id, SourceID: issue.ID, Relation: "mentions", TargetID: pr.ID, Status: "direct", EvidenceID: evidenceID})
	}
	addDeclaredFix := func(issue query.Issue, pr query.PullRequest) {
		if issue.ID == "" || pr.ID == "" || pr.EvidenceID == "" {
			return
		}
		seed := pr.ID + ":declares_fix_of:" + issue.ID + ":" + pr.EvidenceID
		id := "link-" + storage.Hash([]byte(seed))[:24]
		if linked[id] {
			return
		}
		linked[id] = true
		d.Links = append(d.Links, contract.LinkRecord{ID: id, SourceID: pr.ID, Relation: "declares_fix_of", TargetID: issue.ID, Status: "direct", EvidenceID: pr.EvidenceID})
	}
	for _, issue := range d.Issues {
		body, err := c.sourceBody(issue.EvidenceID)
		if err != nil {
			return err
		}
		for _, match := range pullURL.FindAllStringSubmatch(body, -1) {
			key := strings.ToLower(match[1] + "/" + match[2] + "#" + match[3])
			if pr, ok := prs[key]; ok {
				addMention(issue, pr, issue.EvidenceID)
			}
		}
		if issue.Source == "github" {
			repo := strings.SplitN(issue.Identifier, "#", 2)[0]
			for _, match := range pullReference.FindAllStringSubmatch(body, -1) {
				if pr, ok := prs[strings.ToLower(repo+"#"+match[1])]; ok {
					addMention(issue, pr, issue.EvidenceID)
				}
			}
		}
	}
	for _, pr := range d.PullRequests {
		body, err := c.sourceBody(pr.EvidenceID)
		if err != nil {
			return err
		}
		for _, match := range linearIdentifier.FindAllString(body, -1) {
			if issue, ok := issues[strings.ToLower(match)]; ok {
				addMention(issue, pr, pr.EvidenceID)
			}
		}
		for _, match := range issueURL.FindAllStringSubmatch(body, -1) {
			key := strings.ToLower(match[1] + "/" + match[2] + "#" + match[3])
			if issue, ok := issues[key]; ok {
				addMention(issue, pr, pr.EvidenceID)
			}
		}
		for _, match := range closingReference.FindAllStringSubmatch(body, -1) {
			if _, err := strconv.Atoi(match[1]); err != nil {
				continue
			}
			if issue, ok := issues[strings.ToLower(pr.Repository+"#"+match[1])]; ok {
				addMention(issue, pr, pr.EvidenceID)
			}
		}
		for _, match := range fixingReference.FindAllStringSubmatch(body, -1) {
			if issue, ok := issues[strings.ToLower(pr.Repository+"#"+match[1])]; ok {
				addDeclaredFix(issue, pr)
			}
		}
	}
	for _, evidence := range d.Evidence {
		var item struct {
			Body           string `json:"body"`
			IssueURL       string `json:"issue_url"`
			PullRequestURL string `json:"pull_request_url"`
		}
		var raw json.RawMessage
		if err := c.store.Get("objects", evidence.ID, &raw); err != nil {
			return fault("ERR_AUDIT_FAILED", "comentário de vínculo indisponível")
		}
		if json.Unmarshal(raw, &item) != nil || item.Body == "" {
			continue
		}
		if match := apiIssueURL.FindStringSubmatch(item.IssueURL); match != nil {
			repo := match[1] + "/" + match[2]
			issue, ok := issues[strings.ToLower(repo+"#"+match[3])]
			if ok {
				for _, ref := range pullURL.FindAllStringSubmatch(item.Body, -1) {
					if pr, ok := prs[strings.ToLower(ref[1]+"/"+ref[2]+"#"+ref[3])]; ok {
						addMention(issue, pr, evidence.ID)
					}
				}
				for _, ref := range pullReference.FindAllStringSubmatch(item.Body, -1) {
					if pr, ok := prs[strings.ToLower(repo+"#"+ref[1])]; ok {
						addMention(issue, pr, evidence.ID)
					}
				}
			}
		}
		if match := apiPullURL.FindStringSubmatch(item.PullRequestURL); match != nil {
			pr, ok := prs[strings.ToLower(match[1]+"/"+match[2]+"#"+match[3])]
			if ok {
				for _, ref := range linearIdentifier.FindAllString(item.Body, -1) {
					if issue, ok := issues[strings.ToLower(ref)]; ok {
						addMention(issue, pr, evidence.ID)
					}
				}
				for _, ref := range issueURL.FindAllStringSubmatch(item.Body, -1) {
					if issue, ok := issues[strings.ToLower(ref[1]+"/"+ref[2]+"#"+ref[3])]; ok {
						addMention(issue, pr, evidence.ID)
					}
				}
			}
		}
	}
	status := "complete"
	reason := ""
	hasPRCollection := false
	hasFormalCollection := false
	for _, cov := range d.Coverage {
		if cov.Collection == "pull_requests" && cov.Source == "github" {
			hasPRCollection = true
			if cov.Status != "complete" {
				status = "partial"
				reason = "PRs ou issues parcialmente coletados"
			}
		}
		if cov.Collection == "formal_links" && cov.Source == "github" {
			hasFormalCollection = true
			if cov.Status != "complete" {
				status = "partial"
				reason = "relações formais issue-PR parcialmente coletadas"
			}
		}
		if cov.Collection == "issues" && cov.Status != "complete" && cov.Status != "not_selected" {
			status = "partial"
			reason = "PRs ou issues parcialmente coletados"
		}
		if (cov.Collection == "comments" || cov.Collection == "review_comments") && cov.Status != "complete" {
			status = "partial"
			reason = "comentários parcialmente coletados"
		}
	}
	if !hasFormalCollection && hasPRCollection {
		status = "partial"
		reason = "relações formais issue-PR não coletadas"
	}
	if !hasPRCollection {
		status = "unsupported"
		reason = "nenhuma coleção de PR foi selecionada"
	}
	d.Coverage = append(d.Coverage, c.coverage(contract.Resource{Source: "analysis", ID: plan.InvestigationID}, "links", status, int64(len(d.Links)), reason))
	return nil
}

func (c *Core) sourceBody(evidenceID string) (string, error) {
	if evidenceID == "" {
		return "", nil
	}
	var raw json.RawMessage
	if err := c.store.Get("objects", evidenceID, &raw); err != nil {
		return "", fault("ERR_AUDIT_FAILED", "evidência de vínculo indisponível")
	}
	var data struct {
		Body        string `json:"body"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return "", fault("ERR_AUDIT_FAILED", "evidência de vínculo inválida")
	}
	if data.Body != "" {
		return data.Body, nil
	}
	return data.Description, nil
}

package collect

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	"pocketcli/internal/investigation/contract"
)

const requestReceiptKind = "requestreceipts"

// RequestReceipt records one HTTP attempt. It deliberately contains no URL
// parameters, request or response body, headers, or credential material.
type RequestReceipt struct {
	ID                  string    `json:"id"`
	AuditID             string    `json:"audit_id"`
	Source              string    `json:"source"`
	Method              string    `json:"method"`
	Endpoint            string    `json:"endpoint"`
	StartedAt           time.Time `json:"started_at"`
	DurationMS          int64     `json:"duration_ms"`
	Status              int       `json:"status"`
	RecordCount         int       `json:"record_count"`
	WireBytes           int64     `json:"wire_bytes"`
	CacheHit            bool      `json:"cache_hit"`
	RequestedAPIVersion string    `json:"requested_api_version,omitempty"`
	SelectedAPIVersion  string    `json:"selected_api_version,omitempty"`
}

func (c *Client) recordRequest(ctx context.Context, source, method, endpoint string, started time.Time, status int, wireBytes int64, body []byte, cacheHit bool, selectedVersion string) error {
	auditID, _ := ctx.Value(auditIDContextKey{}).(string)
	if c.cacheStore == nil || auditID == "" {
		return nil
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "falha ao identificar recibo HTTP"}
	}
	id := hex.EncodeToString(nonce[:])
	duration := time.Since(started).Milliseconds()
	if duration < 1 {
		duration = 1
	}
	recordCount := 0
	if (status >= 200 && status < 300) || cacheHit {
		recordCount = responseRecordCount(body)
	}
	receipt := RequestReceipt{
		ID: id, AuditID: auditID, Source: source, Method: method,
		Endpoint: logicalEndpoint(source, endpoint), StartedAt: started,
		DurationMS: duration, Status: status, RecordCount: recordCount,
		WireBytes: wireBytes, CacheHit: cacheHit,
	}
	if source == "github" {
		receipt.RequestedAPIVersion = githubAPIVersion
		if len(selectedVersion) == 10 {
			if parsed, err := time.Parse("2006-01-02", selectedVersion); err == nil && parsed.Format("2006-01-02") == selectedVersion {
				receipt.SelectedAPIVersion = selectedVersion
			}
		}
	}
	if err := c.cacheStore.Put(requestReceiptKind, id, receipt); err != nil {
		return &contract.ErrorResponse{Code: "ERR_AUDIT_FAILED", Message: "falha ao persistir recibo HTTP"}
	}
	return nil
}

func logicalEndpoint(source, endpoint string) string {
	if source == "linear" {
		return "/graphql"
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "/unknown"
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return "/"
	}
	static := map[string]bool{
		"user": true, "repos": true, "orgs": true, "teams": true,
		"pulls": true, "issues": true, "comments": true, "reviews": true,
		"timeline": true, "files": true, "commits": true, "git": true,
		"trees": true, "blobs": true, "actions": true, "runs": true,
		"check-runs": true, "deployments": true, "statuses": true,
		"branches": true, "events": true, "languages": true, "versions": true,
	}
	for i, part := range parts {
		if !static[part] || (i > 0 && (parts[i-1] == "repos" || parts[i-1] == "orgs" || parts[i-1] == "teams")) {
			parts[i] = "{id}"
		}
	}
	return "/" + strings.Join(parts, "/")
}

func responseRecordCount(body []byte) int {
	var value any
	if len(body) == 0 || json.Unmarshal(body, &value) != nil {
		return 0
	}
	return countRecords(value)
}

func countRecords(value any) int {
	switch v := value.(type) {
	case []any:
		return len(v)
	case map[string]any:
		if _, hasErrors := v["errors"]; hasErrors {
			return 0
		}
		if data, ok := v["data"]; ok {
			return countRecords(data)
		}
		count := 0
		collection := false
		for key, field := range v {
			switch key {
			case "nodes", "workflow_runs", "check_runs", "tree":
				if list, ok := field.([]any); ok {
					collection = true
					count += len(list)
				}
			default:
				if nested, ok := field.(map[string]any); ok {
					collection = true
					count += countRecords(nested)
				}
			}
		}
		if collection {
			return count
		}
		return 1
	default:
		return 0
	}
}

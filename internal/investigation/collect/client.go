package collect

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"pocketcli/internal/investigation/contract"
)

type Client struct {
	HTTP       *http.Client
	Secrets    contract.SecretProvider
	GitHubURL  string
	LinearURL  string
	mu         sync.Mutex
	spacing    map[string]time.Duration
	next       map[string]time.Time
	cache      map[string]etagEntry
	cacheSize  int64
	cacheStore CacheStore
}

type etagEntry struct {
	ETag   string
	Body   []byte
	Header http.Header
}

func NewClient(h *http.Client, secrets contract.SecretProvider) *Client {
	if h == nil {
		h = &http.Client{Timeout: 30 * time.Second}
	}
	copy := *h
	if copy.Timeout == 0 || copy.Timeout > 30*time.Second {
		copy.Timeout = 30 * time.Second
	}
	copy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return &contract.ErrorResponse{Code: "ERR_UNSUPPORTED_SOURCE", Message: "redirecionamento externo bloqueado"}
	}
	h = &copy
	return &Client{HTTP: h, Secrets: secrets, GitHubURL: "https://api.github.com", LinearURL: "https://api.linear.app/graphql", spacing: make(map[string]time.Duration), next: make(map[string]time.Time), cache: make(map[string]etagEntry)}
}

func (c *Client) SetRPH(source string, requests int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if requests > 0 {
		c.spacing[source] = time.Hour / time.Duration(requests)
	}
}

type Page struct {
	Items  []json.RawMessage
	Next   string
	Bytes  int64
	Status string
}

func (c *Client) GetGitHub(ctx context.Context, endpoint string) ([]byte, http.Header, int, error) {
	data, headers, status, _, err := c.GetGitHubWithBytes(ctx, endpoint)
	return data, headers, status, err
}

// GetGitHubWithBytes separates bytes received over HTTP from a representation
// restored after 304. Transfer budgets must use the former.
func (c *Client) GetGitHubWithBytes(ctx context.Context, endpoint string) ([]byte, http.Header, int, int64, error) {
	base, err := url.Parse(c.GitHubURL)
	target, e2 := url.Parse(endpoint)
	if err != nil || e2 != nil || target.Scheme != "https" || target.Host != base.Host {
		return nil, nil, 0, 0, &contract.ErrorResponse{Code: "ERR_UNSUPPORTED_SOURCE", Message: "endpoint GitHub não autorizado"}
	}
	return c.request(ctx, "github", "GET", endpoint, nil)
}

func (c *Client) request(ctx context.Context, source, method, endpoint string, body io.Reader) ([]byte, http.Header, int, int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock() // one in-flight external request per client
	if c.Secrets == nil {
		return nil, nil, 0, 0, &contract.ErrorResponse{Code: "ERR_CREDENTIAL_REQUIRED", Message: "credencial ausente"}
	}
	var payload []byte
	if body != nil {
		var e error
		payload, e = io.ReadAll(io.LimitReader(body, 256*1024+1))
		if e != nil || len(payload) > 256*1024 {
			return nil, nil, 0, 0, &contract.ErrorResponse{Code: "ERR_RESOURCE_EXHAUSTED", Message: "request externa excede limite"}
		}
	}
	var response []byte
	var headers http.Header
	var status int
	var wireBytes int64
	var sourceRateLimited bool
	err := c.Secrets.WithSecret(ctx, source, func(secret []byte) error {
		release, err := c.lockPrincipal(ctx, source, secret)
		if err != nil {
			return err
		}
		defer release()
		cacheKey := ""
		var cached etagEntry
		if method == "GET" {
			hash := sha256.New()
			hash.Write([]byte(source + ":" + endpoint + ":"))
			hash.Write(secret)
			cacheKey = hex.EncodeToString(hash.Sum(nil))
			var cacheErr error
			cached, cacheErr = c.loadCache(cacheKey)
			if cacheErr != nil {
				return cacheErr
			}
		}
		skipConditional := false
		for attempt := 0; attempt < 4; attempt++ {
			if err := c.waitRateHold(ctx, source, secret); err != nil {
				return err
			}
			if err := waitUntil(ctx, c.next[source]); err != nil {
				return err
			}
			req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
			if err != nil {
				return &contract.ErrorResponse{Code: "ERR_VALIDATION", Message: "request inválida"}
			}
			if source == "github" {
				req.Header.Set("Authorization", "Bearer "+string(secret))
				req.Header.Set("Accept", "application/vnd.github+json")
				req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
				if cached.ETag != "" && !skipConditional {
					req.Header.Set("If-None-Match", cached.ETag)
				}
			} else {
				req.Header.Set("Authorization", string(secret))
				req.Header.Set("Content-Type", "application/json")
			}
			if err := claimDiscoveryRequest(ctx, source); err != nil {
				return err
			}
			c.next[source] = time.Now().Add(c.spacing[source])
			if err := c.saveRateHold(source, secret, c.spacing[source]); err != nil {
				return err
			}
			started := time.Now().UTC()
			res, err := c.HTTP.Do(req)
			if err != nil {
				if auditErr := c.recordRequest(ctx, source, method, endpoint, started, 0, 0, nil, false); auditErr != nil {
					return auditErr
				}
				if attempt < 3 {
					if e := waitUntil(ctx, time.Now().Add(retryBackoff(attempt))); e != nil {
						return e
					}
					continue
				}
				return &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "fonte indisponível", Retryable: true}
			}
			status = res.StatusCode
			headers = res.Header.Clone()
			response, err = io.ReadAll(io.LimitReader(res.Body, 10*1024*1024+1))
			res.Body.Close()
			wireBytes += int64(len(response))
			cacheHit := status == http.StatusNotModified && cached.ETag != "" && !skipConditional
			auditBody := response
			if cacheHit {
				auditBody = cached.Body
			}
			if auditErr := c.recordRequest(ctx, source, method, endpoint, started, status, int64(len(response)), auditBody, cacheHit); auditErr != nil {
				return auditErr
			}
			if err != nil {
				return &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "resposta externa inválida"}
			}
			if len(response) > 10*1024*1024 {
				return &contract.ErrorResponse{Code: "ERR_RESOURCE_EXHAUSTED", Message: "resposta externa excede 10 MiB"}
			}
			if status == http.StatusNotModified {
				if cached.ETag != "" && !skipConditional {
					if err := c.recordReuse(ctx); err != nil {
						return err
					}
					response = append([]byte(nil), cached.Body...)
					headers = cached.Header.Clone()
					status = http.StatusOK
					return nil
				}
				skipConditional = true
				if attempt == 3 {
					return &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "304 sem representação local"}
				}
				continue
			}
			if cacheKey != "" && status >= 200 && status < 300 && headers.Get("ETag") != "" && len(response) <= 1024*1024 {
				if c.cacheSize+int64(len(response)) > 32*1024*1024 || len(c.cache) >= 128 {
					c.cache = make(map[string]etagEntry)
					c.cacheSize = 0
				}
				if old, ok := c.cache[cacheKey]; ok {
					c.cacheSize -= int64(len(old.Body))
				}
				copyBody := append([]byte(nil), response...)
				entry := etagEntry{ETag: headers.Get("ETag"), Body: copyBody, Header: headers.Clone()}
				if err := c.saveCache(cacheKey, entry); err != nil {
					return err
				}
				c.cache[cacheKey] = entry
				c.cacheSize += int64(len(copyBody))
			}
			if (source == "github" && status == http.StatusForbidden && githubRateLimited(status, headers, response)) || (source == "linear" && (status == http.StatusBadRequest || status == http.StatusOK) && linearRateLimited(response)) {
				sourceRateLimited = true
				if err := c.saveRateHold(source, secret, rateLimitDelay(source, headers)); err != nil {
					return err
				}
				return nil
			}
			if status == 429 || status == 503 || status >= 500 {
				delay := retryBackoff(attempt)
				if status == http.StatusTooManyRequests {
					delay = rateLimitDelay(source, headers)
				} else if value, ok := retryAfterDelay(headers); ok {
					delay = value
				}
				if status == http.StatusTooManyRequests || headers.Get("Retry-After") != "" {
					if err := c.saveRateHold(source, secret, delay); err != nil {
						return err
					}
				}
				if attempt < 3 {
					if delay > 30*time.Second {
						return &contract.ErrorResponse{Code: "ERR_RATE_LIMITED", Message: "fonte solicitou espera longa", Retryable: true}
					}
					if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < delay {
						return &contract.ErrorResponse{Code: "ERR_RATE_LIMITED", Message: "espera da fonte excede sessão restante", Retryable: true}
					}
					if e := waitUntil(ctx, time.Now().Add(delay)); e != nil {
						return e
					}
					continue
				}
			}
			return nil
		}
		return &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "tentativas esgotadas", Retryable: true}
	})
	if err != nil {
		return nil, nil, 0, wireBytes, err
	}
	if sourceRateLimited && source == "github" {
		return nil, nil, status, wireBytes, &contract.ErrorResponse{Code: "ERR_RATE_LIMITED", Message: "limite GitHub atingido", Retryable: true}
	}
	if status == 401 || status == 403 {
		return nil, nil, status, wireBytes, &contract.ErrorResponse{Code: "ERR_FORBIDDEN", Message: "leitura negada pela fonte"}
	}
	if status == 429 {
		return nil, nil, status, wireBytes, &contract.ErrorResponse{Code: "ERR_RATE_LIMITED", Message: "limite da fonte atingido", Retryable: true}
	}
	if source == "linear" && status == 400 {
		return response, headers, status, wireBytes, nil
	}
	if status < 200 || status >= 300 {
		return nil, nil, status, wireBytes, &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: fmt.Sprintf("fonte retornou HTTP %d", status)}
	}
	return response, headers, status, wireBytes, nil
}

func waitUntil(ctx context.Context, t time.Time) error {
	d := time.Until(t)
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) GitHubList(ctx context.Context, path string, maxPages int) ([]json.RawMessage, int, error) {
	return c.GitHubListUntil(ctx, path, maxPages, "")
}

// GitHubListUntil keeps the page containing the cutoff item for reconciliation,
// then stops requesting older pages. Call only on endpoints sorted by updated.
func (c *Client) GitHubListUntil(ctx context.Context, path string, maxPages int, cutoff string) ([]json.RawMessage, int, error) {
	var all []json.RawMessage
	requests, err := c.GitHubWalk(ctx, path, maxPages, cutoff, func(items []json.RawMessage, _ string, _ int64) error {
		all = append(all, items...)
		return nil
	})
	return all, requests, err
}

// GitHubWalk calls committed before following the next page. A caller can
// persist the page and cursor in committed; any error stops network access.
func (c *Client) GitHubWalk(ctx context.Context, path string, maxPages int, cutoff string, committed func([]json.RawMessage, string, int64) error) (int, error) {
	return c.githubWalk(ctx, path, maxPages, cutoff, "", committed)
}

// GitHubWalkField paginates supported object-wrapped GitHub list endpoints.
func (c *Client) GitHubWalkField(ctx context.Context, path string, maxPages int, field string, committed func([]json.RawMessage, string, int64) error) (int, error) {
	if field != "workflow_runs" && field != "check_runs" {
		return 0, &contract.ErrorResponse{Code: "ERR_VALIDATION", Message: "coleção GitHub inválida"}
	}
	return c.githubWalk(ctx, path, maxPages, "", field, committed)
}

func (c *Client) githubWalk(ctx context.Context, path string, maxPages int, cutoff, field string, committed func([]json.RawMessage, string, int64) error) (int, error) {
	if maxPages < 1 {
		return 0, nil
	}
	base, err := url.Parse(c.GitHubURL)
	if err != nil || base.Scheme != "https" {
		return 0, &contract.ErrorResponse{Code: "ERR_UNSUPPORTED_SOURCE", Message: "GitHub base URL inválida"}
	}
	rel, err := url.Parse(path)
	if err != nil || (!rel.IsAbs() && !strings.HasPrefix(rel.Path, "/")) {
		return 0, &contract.ErrorResponse{Code: "ERR_VALIDATION", Message: "endpoint inválido"}
	}
	u := base.ResolveReference(rel).String()
	requests := 0
	for u != "" && requests < maxPages {
		current, err := url.Parse(u)
		if err != nil || current.Host != base.Host || current.Scheme != "https" {
			return requests, &contract.ErrorResponse{Code: "ERR_UNSUPPORTED_SOURCE", Message: "paginação fora do host autorizado"}
		}
		data, headers, _, wireBytes, err := c.request(ctx, "github", "GET", u, nil)
		requests++
		if err != nil {
			return requests, err
		}
		var items []json.RawMessage
		if field == "" {
			err = json.Unmarshal(data, &items)
		} else {
			var envelope map[string]json.RawMessage
			if err = json.Unmarshal(data, &envelope); err == nil {
				err = json.Unmarshal(envelope[field], &items)
			}
		}
		if err != nil || items == nil {
			return requests, &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "lista GitHub inválida"}
		}
		u = nextLink(headers.Get("Link"))
		if cutoff != "" {
			cutoffAt, cutoffErr := time.Parse(time.RFC3339, cutoff)
			for _, item := range items {
				var v struct {
					UpdatedAt string `json:"updated_at"`
				}
				if json.Unmarshal(item, &v) != nil {
					continue
				}
				updatedAt, updatedErr := time.Parse(time.RFC3339, v.UpdatedAt)
				if cutoffErr == nil && updatedErr == nil && updatedAt.Before(cutoffAt) {
					u = ""
					break
				}
			}
		}
		if err := committed(items, u, wireBytes); err != nil {
			return requests, err
		}
	}
	if u != "" {
		return requests, &contract.ErrorResponse{Code: "ERR_RESOURCE_EXHAUSTED", Message: "descoberta incompleta: limite de páginas"}
	}
	return requests, nil
}

func nextLink(header string) string {
	for _, part := range strings.Split(header, ",") {
		if strings.Contains(part, `rel="next"`) {
			a := strings.Index(part, "<")
			b := strings.Index(part, ">")
			if a >= 0 && b > a {
				return part[a+1 : b]
			}
		}
	}
	return ""
}

func (c *Client) LinearQuery(ctx context.Context, query string, variables map[string]string) (json.RawMessage, error) {
	if !strings.HasPrefix(strings.TrimSpace(query), "query ") {
		return nil, &contract.ErrorResponse{Code: "ERR_VALIDATION", Message: "apenas query GraphQL fixa"}
	}
	base, err := url.Parse(c.LinearURL)
	if err != nil || base.Scheme != "https" {
		return nil, &contract.ErrorResponse{Code: "ERR_UNSUPPORTED_SOURCE", Message: "Linear URL inválida"}
	}
	requestBody, _ := json.Marshal(struct {
		Query     string            `json:"query"`
		Variables map[string]string `json:"variables"`
	}{query, variables})
	data, _, _, _, err := c.request(ctx, "linear", "POST", c.LinearURL, strings.NewReader(string(requestBody)))
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message    string `json:"message"`
			Extensions struct {
				Code string `json:"code"`
			} `json:"extensions"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "GraphQL inválido"}
	}
	if len(envelope.Errors) > 0 {
		for _, e := range envelope.Errors {
			if e.Extensions.Code == "RATELIMITED" {
				return nil, &contract.ErrorResponse{Code: "ERR_RATE_LIMITED", Message: "limite Linear atingido", Retryable: true}
			}
		}
		return nil, &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "GraphQL retornou erro parcial"}
	}
	if len(envelope.Data) == 0 {
		return nil, errors.New("GraphQL sem data")
	}
	return envelope.Data, nil
}

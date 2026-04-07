package memory

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	retrievalScoreMinimum = 2.0
	retrievalTopK         = 5
	tagMatchScore         = 3.0
	titleMatchScore       = 2.0
	summaryMatchScore     = 1.0
)

type RetrievalContext struct {
	WorkingDir string
	Project    string
	Host       string
}

type retrievalScore struct {
	base          float64
	final         float64
	recencyWeight float64
}

type retrievalCandidate struct {
	entry Entry
	score retrievalScore
	path  string
	index int
}

func (s *Store) Retrieve(query string, ctx RetrievalContext) ([]Entry, error) {
	normalizedQuery := normalizeQuery(query)
	if normalizedQuery == "" {
		return nil, errors.New("query obrigatória")
	}

	scopes, err := ResolveCandidateScopes(ctx)
	if err != nil {
		return nil, err
	}

	now := s.now().UTC()
	queryTerms := strings.Fields(normalizedQuery)
	entriesByPath := make(map[string][]Entry, len(scopes))
	candidates := make([]retrievalCandidate, 0)

	for _, scope := range scopes {
		path, err := s.scopeFilePath(scope)
		if err != nil {
			return nil, err
		}

		entries, err := s.loadEntries(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		entriesByPath[path] = entries

		for idx, entry := range entries {
			score, err := scoreEntry(normalizedQuery, queryTerms, entry, now)
			if err != nil {
				return nil, err
			}
			candidates = append(candidates, retrievalCandidate{
				entry: entry,
				score: score,
				path:  path,
				index: idx,
			})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]

		switch {
		case left.score.final != right.score.final:
			return left.score.final > right.score.final
		case left.score.base != right.score.base:
			return left.score.base > right.score.base
		case left.entry.CreatedAt != right.entry.CreatedAt:
			return left.entry.CreatedAt > right.entry.CreatedAt
		default:
			return left.entry.ID < right.entry.ID
		}
	})

	updatedAt := now.Format(time.RFC3339)
	dirtyPaths := make(map[string]bool)
	results := make([]Entry, 0, retrievalTopK)

	for _, candidate := range candidates {
		if candidate.score.final < retrievalScoreMinimum {
			continue
		}
		if len(results) == retrievalTopK {
			break
		}

		entries := entriesByPath[candidate.path]
		entries[candidate.index].AccessCount++
		entries[candidate.index].LastAccessed = updatedAt

		dirtyPaths[candidate.path] = true
		results = append(results, entries[candidate.index])
	}

	for path, dirty := range dirtyPaths {
		if !dirty {
			continue
		}
		if err := s.writeEntries(path, entriesByPath[path]); err != nil {
			return nil, err
		}
	}

	return results, nil
}

func ResolveCandidateScopes(ctx RetrievalContext) ([]string, error) {
	scopes := []string{"global"}

	projectScope, err := normalizeContextScope("project", ctx.Project)
	if err != nil {
		return nil, err
	}
	if projectScope == "" {
		projectScope = detectGitProjectScope(ctx.WorkingDir)
	}
	if projectScope != "" {
		scopes = append(scopes, projectScope)
	}

	hostScope, err := normalizeContextScope("host", ctx.Host)
	if err != nil {
		return nil, err
	}
	if hostScope != "" {
		scopes = append(scopes, hostScope)
	}

	return dedupeScopes(scopes), nil
}

func scoreEntry(query string, queryTerms []string, entry Entry, now time.Time) (retrievalScore, error) {
	base := 0.0
	for _, tag := range entry.Tags {
		normalizedTag := normalizeQuery(tag)
		if normalizedTag != "" && strings.Contains(query, normalizedTag) {
			base += tagMatchScore
		}
	}

	title := normalizeQuery(entry.Title)
	if containsAnyQueryTerm(title, queryTerms) {
		base += titleMatchScore
	}

	summary := normalizeQuery(entry.Summary)
	if containsAnyQueryTerm(summary, queryTerms) {
		base += summaryMatchScore
	}

	weight, err := recencyWeight(entry.CreatedAt, now)
	if err != nil {
		return retrievalScore{}, err
	}

	return retrievalScore{
		base:          base,
		final:         base * entry.Confidence * weight,
		recencyWeight: weight,
	}, nil
}

func recencyWeight(createdAt string, now time.Time) (float64, error) {
	created, err := time.Parse(time.RFC3339, strings.TrimSpace(createdAt))
	if err != nil {
		return 0, err
	}

	daysSince := int(now.Sub(created.UTC()).Hours() / 24)
	if daysSince < 0 {
		daysSince = 0
	}

	return 1 / math.Log(float64(daysSince)+2), nil
}

func normalizeQuery(value string) string {
	return strings.ToLower(compactWhitespace(value))
}

func containsAnyQueryTerm(target string, terms []string) bool {
	if target == "" {
		return false
	}
	for _, term := range terms {
		if term != "" && strings.Contains(target, term) {
			return true
		}
	}
	return false
}

func normalizeContextScope(kind, value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}

	prefix := strings.ToLower(kind) + ":"
	if strings.HasPrefix(strings.ToLower(value), prefix) {
		return normalizeScope(value)
	}
	return normalizeScope(kind + ":" + value)
}

func detectGitProjectScope(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return ""
	}

	dir := filepath.Clean(cwd)
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return DefaultScopeFromCWD(dir)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func dedupeScopes(scopes []string) []string {
	seen := make(map[string]struct{}, len(scopes))
	deduped := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		deduped = append(deduped, scope)
	}
	return deduped
}

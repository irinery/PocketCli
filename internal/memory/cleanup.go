package memory

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	cleanupConfidenceThreshold = 0.3
	cleanupStaleAge            = 90 * 24 * time.Hour
	cleanupScopeLimit          = 500
)

const (
	CleanupReasonLowConfidenceStale = "low_confidence_stale"
	CleanupReasonScopeOverflow      = "scope_overflow"
)

type CleanupCandidate struct {
	Entry   Entry
	Reasons []string
}

func (s *Store) CleanupCandidates(scope string) ([]CleanupCandidate, error) {
	paths, err := s.cleanupPaths(scope)
	if err != nil {
		return nil, err
	}

	candidateIndex := make(map[string]int)
	candidates := make([]CleanupCandidate, 0)
	now := s.now().UTC()

	for _, path := range paths {
		entries, err := s.loadEntries(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}

		for _, entry := range entries {
			lastAccessed, err := parseEntryTime(entry.LastAccessed)
			if err != nil {
				return nil, fmt.Errorf("last_accessed inválido para %s: %w", entry.ID, err)
			}
			if entry.Confidence < cleanupConfidenceThreshold && now.Sub(lastAccessed) > cleanupStaleAge {
				addCleanupCandidate(candidateIndex, &candidates, entry, CleanupReasonLowConfidenceStale)
			}
		}

		if len(entries) <= cleanupScopeLimit {
			continue
		}

		sortedEntries := append([]Entry(nil), entries...)
		sort.Slice(sortedEntries, func(i, j int) bool {
			leftLastAccessed, _ := parseEntryTime(sortedEntries[i].LastAccessed)
			rightLastAccessed, _ := parseEntryTime(sortedEntries[j].LastAccessed)

			switch {
			case !leftLastAccessed.Equal(rightLastAccessed):
				return leftLastAccessed.After(rightLastAccessed)
			case sortedEntries[i].CreatedAt != sortedEntries[j].CreatedAt:
				return sortedEntries[i].CreatedAt > sortedEntries[j].CreatedAt
			default:
				return sortedEntries[i].ID < sortedEntries[j].ID
			}
		})

		for _, entry := range sortedEntries[cleanupScopeLimit:] {
			addCleanupCandidate(candidateIndex, &candidates, entry, CleanupReasonScopeOverflow)
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		leftLastAccessed, _ := parseEntryTime(candidates[i].Entry.LastAccessed)
		rightLastAccessed, _ := parseEntryTime(candidates[j].Entry.LastAccessed)

		switch {
		case !leftLastAccessed.Equal(rightLastAccessed):
			return leftLastAccessed.Before(rightLastAccessed)
		case candidates[i].Entry.Scope != candidates[j].Entry.Scope:
			return candidates[i].Entry.Scope < candidates[j].Entry.Scope
		default:
			return candidates[i].Entry.ID < candidates[j].Entry.ID
		}
	})

	return candidates, nil
}

func (s *Store) DeleteEntry(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("id obrigatório")
	}

	paths, err := s.cleanupPaths("")
	if err != nil {
		return err
	}

	for _, path := range paths {
		entries, err := s.loadEntries(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}

		filtered := make([]Entry, 0, len(entries))
		removed := false
		for _, entry := range entries {
			if entry.ID == id {
				removed = true
				continue
			}
			filtered = append(filtered, entry)
		}

		if !removed {
			continue
		}

		return s.writeEntries(path, filtered)
	}

	return fmt.Errorf("%w: %s", ErrEntryNotFound, id)
}

func (s *Store) cleanupPaths(scope string) ([]string, error) {
	scope = strings.TrimSpace(scope)
	if scope != "" {
		path, err := s.scopeFilePath(scope)
		if err != nil {
			return nil, err
		}
		return []string{path}, nil
	}

	dirEntries, err := os.ReadDir(s.memoryDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	paths := make([]string, 0, len(dirEntries))
	for _, dirEntry := range dirEntries {
		if dirEntry.IsDir() || !strings.HasSuffix(dirEntry.Name(), ".jsonl") {
			continue
		}
		paths = append(paths, filepath.Join(s.memoryDir(), dirEntry.Name()))
	}
	sort.Strings(paths)
	return paths, nil
}

func addCleanupCandidate(index map[string]int, candidates *[]CleanupCandidate, entry Entry, reason string) {
	key := entry.Scope + "\x00" + entry.ID
	if candidateIdx, ok := index[key]; ok {
		reasons := (*candidates)[candidateIdx].Reasons
		for _, existing := range reasons {
			if existing == reason {
				return
			}
		}
		(*candidates)[candidateIdx].Reasons = append(reasons, reason)
		return
	}

	index[key] = len(*candidates)
	*candidates = append(*candidates, CleanupCandidate{
		Entry:   entry,
		Reasons: []string{reason},
	})
}

func parseEntryTime(value string) (time.Time, error) {
	return time.Parse(time.RFC3339, strings.TrimSpace(value))
}

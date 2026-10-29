package contract

import (
	"strings"
	"time"
)

// EmptyRepositoryReason prefixes the coverage reason of a repository that
// exists but has no commits. It is a valid, complete observation.
const EmptyRepositoryReason = "repositório vazio"

// IsEmptyRepository reports whether a Git coverage row describes an empty
// repository, so every surface can list those repositories the same way.
func IsEmptyRepository(c Coverage) bool {
	return c.Source == "git" && strings.HasPrefix(c.Reason, EmptyRepositoryReason)
}

// NormalizeInstant turns a source timestamp (RFC 3339 with or without
// fractional seconds, any offset) into UTC RFC 3339, or "" when unusable.
func NormalizeInstant(value string) string {
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return at.UTC().Format(time.RFC3339)
}

// ResourceLifetime is the span a resource existed: creation to last change,
// or to windowEnd when the source has no last-change signal. ok is false
// when the creation instant is unknown.
func ResourceLifetime(r Resource, windowEnd string) (from, to string, ok bool) {
	if r.CreatedAt == "" {
		return "", "", false
	}
	to = r.LastChangeAt
	if to == "" || (windowEnd != "" && to > windowEnd) {
		to = windowEnd
	}
	return r.CreatedAt, to, true
}

// EffectiveWindow resolves what the analysis really covers. A bounded window
// is returned as is. For the "all" period (empty start) it is the union of
// the resources' lifetimes: the earliest creation to the latest last change,
// reaching windowEnd whenever any resource lacks a last-change instant.
// lifetime is false when nothing is known, and the caller should say
// "todo o histórico" instead of printing a blank start.
func EffectiveWindow(start, end string, resources []Resource) (from, to string, lifetime bool) {
	if start != "" {
		return start, end, false
	}
	for _, r := range resources {
		f, t, ok := ResourceLifetime(r, end)
		if !ok {
			continue
		}
		if from == "" || f < from {
			from = f
		}
		if t > to {
			to = t
		}
	}
	if from == "" {
		return "", end, false
	}
	return from, to, true
}

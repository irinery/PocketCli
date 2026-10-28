package insightcontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// AnalysisInputHash identifies the exact immutable source bundle and facts
// consumed by semantic analysis. It is distinct from SourceBundle.InputHash,
// which identifies the source projection before deterministic facts exist.
func AnalysisInputHash(bundle SourceBundle, facts FactsResult) (string, error) {
	body, err := json.Marshal(struct {
		Bundle SourceBundle
		Facts  FactsResult
	}{bundle, facts})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

// ConfigHash returns the canonical analysis identity shared by source facts
// and the asynchronous analysis service. Collection schema and query catalog
// versions are part of the identity because they change the available facts.
func ConfigHash(cfg AnalysisConfig, schemaVersion, queryCatalogVersion string) (string, error) {
	canonical := cfg
	canonical.RequestedScope.Resources = append([]ResourceRef{}, cfg.RequestedScope.Resources...)
	sort.Slice(canonical.RequestedScope.Resources, func(i, j int) bool {
		a, b := canonical.RequestedScope.Resources[i], canonical.RequestedScope.Resources[j]
		return a.Source+"\x00"+a.ID < b.Source+"\x00"+b.ID
	})
	canonical.Objectives = append([]Objective{}, cfg.Objectives...)
	for i := range canonical.Objectives {
		canonical.Objectives[i].EntityIDs = sortedUnique(canonical.Objectives[i].EntityIDs)
		canonical.Objectives[i].EvidenceIDs = sortedUnique(canonical.Objectives[i].EvidenceIDs)
	}
	sort.Slice(canonical.Objectives, func(i, j int) bool { return canonical.Objectives[i].ID < canonical.Objectives[j].ID })
	body, err := json.Marshal(struct {
		SchemaVersion       string         `json:"schema_version"`
		QueryCatalogVersion string         `json:"query_catalog_version"`
		Config              AnalysisConfig `json:"config"`
	}{schemaVersion, queryCatalogVersion, canonical})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

func sortedUnique(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	copyValues := append([]string{}, values...)
	sort.Strings(copyValues)
	out := copyValues[:0]
	for _, value := range copyValues {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}

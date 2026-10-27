package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

const runtimeSchema = "pocketcli.runtime.v1"

var runtimeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
var runtimeDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

type runtimeComponent struct {
	Name        string `json:"name"`
	Repository  string `json:"repository,omitempty"`
	Path        string `json:"path,omitempty"`
	Enabled     *bool  `json:"enabled"`
	RevisionSHA string `json:"revision_sha,omitempty"`
	Digest      string `json:"digest,omitempty"`
}

type runtimeSnapshot struct {
	SchemaVersion string             `json:"schema_version"`
	ObservedAt    string             `json:"observed_at"`
	Environment   string             `json:"environment"`
	Components    []runtimeComponent `json:"components"`
}

type runtimeImportRef struct {
	Hash string `json:"hash"`
}

// ImportRuntime accepts only the typed, sanitized local format. The imported
// version is immutable; PreparePlan pins its hash before confirmation.
func (c *Core) ImportRuntime(ctx context.Context, investigationID string, raw []byte) (string, error) {
	if !runtimeName.MatchString(investigationID) {
		return "", fault("ERR_VALIDATION", "investigation_id inválido")
	}
	if len(raw) > 2*1024*1024 || !utf8.Valid(raw) {
		return "", fault("ERR_RESOURCE_EXHAUSTED", "snapshot operacional excede 2 MiB ou não é UTF-8")
	}
	if err := validateJSONKeys(raw); err != nil {
		return "", fault("ERR_VALIDATION", "JSON operacional inválido ou com chave duplicada")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var snap runtimeSnapshot
	if dec.Decode(&snap) != nil || dec.Decode(new(any)) != io.EOF {
		return "", fault("ERR_VALIDATION", "schema operacional inválido")
	}
	if snap.SchemaVersion != runtimeSchema || !runtimeName.MatchString(snap.Environment) || len(snap.Components) == 0 || len(snap.Components) > 1000 {
		return "", fault("ERR_VALIDATION", "schema, ambiente ou componentes inválidos")
	}
	if _, err := time.Parse(time.RFC3339, snap.ObservedAt); err != nil {
		return "", fault("ERR_VALIDATION", "observed_at inválido")
	}
	seenComponents := make(map[string]bool, len(snap.Components))
	for _, item := range snap.Components {
		if !runtimeName.MatchString(item.Name) || item.Enabled == nil || len(item.Path) > 1024 || strings.ContainsAny(item.Path, "\x00\r\n") {
			return "", fault("ERR_VALIDATION", "componente operacional inválido")
		}
		if seenComponents[item.Name] {
			return "", fault("ERR_VALIDATION", "componente operacional duplicado no ambiente")
		}
		seenComponents[item.Name] = true
		if (item.Repository == "") != (item.Path == "") {
			return "", fault("ERR_VALIDATION", "repository e path devem aparecer juntos")
		}
		if item.Repository != "" && (!validRepo(item.Repository) || path.IsAbs(item.Path) || path.Clean(item.Path) != item.Path || item.Path == ".." || strings.HasPrefix(item.Path, "../")) {
			return "", fault("ERR_VALIDATION", "referência de arquivo operacional inválida")
		}
		if item.RevisionSHA != "" && !validGitOID(item.RevisionSHA) {
			return "", fault("ERR_VALIDATION", "revision_sha inválido")
		}
		if item.Digest != "" && !runtimeDigest.MatchString(item.Digest) {
			return "", fault("ERR_VALIDATION", "digest inválido")
		}
	}
	known := false
	planIDs, err := c.store.List("plans")
	if err != nil {
		return "", fault("ERR_AUDIT_FAILED", "planos indisponíveis")
	}
	for _, id := range planIDs {
		var plan contract.Plan
		if err := c.store.Get("plans", id, &plan); err != nil {
			return "", fault("ERR_AUDIT_FAILED", "plano corrompido")
		}
		known = known || plan.InvestigationID == investigationID
	}
	if !known {
		return "", fault("ERR_NOT_FOUND", "investigação não encontrada; prepare o plano primeiro")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	release, err := c.store.Lock(investigationID)
	if err != nil {
		return "", fault("ERR_REVISION_CONFLICT", "investigação em execução")
	}
	defer release()
	canonical, err := json.Marshal(snap)
	if err != nil {
		return "", fault("ERR_VALIDATION", "snapshot operacional inválido")
	}
	hash := storage.Hash(canonical)
	if err := c.store.Put("runtimeimports", hash, snap); err != nil {
		return "", fault("ERR_AUDIT_FAILED", "falha ao persistir importação operacional")
	}
	if err := c.store.Put("runtimecurrent", investigationID, runtimeImportRef{Hash: hash}); err != nil {
		return "", fault("ERR_AUDIT_FAILED", "falha ao fixar importação operacional")
	}
	return hash, nil
}

func validateJSONKeys(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 32 {
			return errors.New("JSON profundo")
		}
		token, err := dec.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := make(map[string]bool)
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return err
				}
				key, ok := keyToken.(string)
				if !ok || seen[key] {
					return errors.New("chave duplicada")
				}
				seen[key] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			_, err := dec.Token()
			return err
		case '[':
			for dec.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
			_, err := dec.Token()
			return err
		default:
			return errors.New("delimitador inválido")
		}
	}
	if err := walk(0); err != nil {
		return err
	}
	_, err := dec.Token()
	if err != io.EOF {
		return errors.New("JSON extra")
	}
	return nil
}

func (c *Core) runtimeResource(investigationID string) (contract.Resource, bool, error) {
	var ref runtimeImportRef
	if err := c.store.Get("runtimecurrent", investigationID, &ref); err != nil {
		if os.IsNotExist(err) {
			return contract.Resource{ID: "runtime-pending", Source: "runtime", Name: "Importação local pendente", Access: "read"}, false, nil
		}
		return contract.Resource{}, false, fault("ERR_AUDIT_FAILED", "importação operacional indisponível")
	}
	if !validGitOID(ref.Hash) {
		return contract.Resource{}, false, fault("ERR_AUDIT_FAILED", "referência operacional inválida")
	}
	var snap runtimeSnapshot
	if err := c.store.Get("runtimeimports", ref.Hash, &snap); err != nil {
		return contract.Resource{}, false, fault("ERR_AUDIT_FAILED", "importação operacional ausente")
	}
	return contract.Resource{ID: ref.Hash, Source: "runtime", Name: snap.Environment, Access: "read"}, true, nil
}

func (c *Core) collectRuntime(ctx context.Context, r contract.Resource, d *query.Dataset, run *contract.Run) error {
	if r.ID == "runtime-pending" {
		d.Coverage = append(d.Coverage, c.coverage(r, "import", "pending", 0, "importe um snapshot operacional sanitizado e prepare novo plano"))
		return nil
	}
	var snap runtimeSnapshot
	if err := c.store.Get("runtimeimports", r.ID, &snap); err != nil {
		return fault("ERR_AUDIT_FAILED", "snapshot operacional fixado indisponível")
	}
	for _, item := range snap.Components {
		if err := ctx.Err(); err != nil {
			return err
		}
		itemRaw, _ := json.Marshal(item)
		locator := fmt.Sprintf("%s:%s@%s", snap.Environment, item.Name, snap.ObservedAt)
		e, err := c.evidence("runtime", locator, snap.ObservedAt, itemRaw)
		if err != nil {
			return err
		}
		upsertEvidence(d, e)
		componentID := "runtime-" + storage.Hash([]byte(snap.Environment + ":" + item.Name))[:24]
		hasArtifactIdentity := item.RevisionSHA != "" || item.Digest != ""
		matched := false
		if item.Repository != "" {
			for i := range d.Components {
				if d.Components[i].Kind != "file_change" && d.Components[i].Repository == item.Repository && d.Components[i].Path == item.Path {
					componentID = d.Components[i].ID
					if *item.Enabled && hasArtifactIdentity {
						d.Components[i].DeploymentStatus = "observed"
					}
					matched = true
					break
				}
			}
		}
		if !matched {
			deploymentStatus := "unknown"
			if *item.Enabled && hasArtifactIdentity {
				deploymentStatus = "observed"
			}
			d.Components = append(d.Components, contract.ComponentRecord{ID: componentID, Name: item.Name, Repository: item.Repository, Path: item.Path, Kind: "runtime_service", ImplementationStatus: "unknown", DeploymentStatus: deploymentStatus, Coverage: "partial"})
		}
		state := "enabled"
		if !*item.Enabled {
			state = "disabled"
		}
		upsertEvent(d, query.Event{ID: "evt-" + storage.Hash([]byte(locator + ":" + e.ID))[:24], EntityID: componentID, Type: state, EventAt: snap.ObservedAt, ObservedAt: e.ObservedAt, NewValue: snap.Environment, EvidenceID: e.ID})
		if *item.Enabled && hasArtifactIdentity {
			d.Deliveries = append(d.Deliveries, contract.DeliveryRecord{ID: "art-" + storage.Hash([]byte(locator + ":" + e.ID))[:24], ComponentID: componentID, Digest: item.Digest, RevisionSHA: item.RevisionSHA, Environment: snap.Environment, ObservedAt: snap.ObservedAt, ProvenanceStatus: "declared", EvidenceID: e.ID})
		}
		run.Items++
	}
	if !c.saveRun(run) {
		return fault("ERR_AUDIT_FAILED", "falha ao auditar importação operacional")
	}
	d.Coverage = append(d.Coverage, c.coverage(r, "import", "complete", int64(len(snap.Components)), "snapshot local tipado observado; não reconstrói estado anterior"))
	return nil
}

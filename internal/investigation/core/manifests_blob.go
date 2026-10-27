package core

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/gitmirror"
	"pocketcli/internal/investigation/query"
	"pocketcli/internal/investigation/storage"
)

type cachedGitBlob struct {
	SHA     string `json:"sha"`
	Content []byte `json:"content"`
}

func (c *Core) collectManifestBlob(ctx context.Context, plan contract.Plan, r contract.Resource, entry gitTreeEntry, kind, authID, observedAt string, d *query.Dataset, run *contract.Run) (manifestFacts, contract.Evidence, error) {
	var facts manifestFacts
	var evidence contract.Evidence
	cacheID := storage.Hash([]byte(authID + ":" + r.ID + ":" + entry.SHA))
	var blob cachedGitBlob
	err := c.store.Get("gitblobs", cacheID, &blob)
	if err != nil && !os.IsNotExist(err) {
		return facts, evidence, fault("ERR_AUDIT_FAILED", "cache de blob Git corrompido")
	}
	if os.IsNotExist(err) {
		endpoint := c.collector.GitHubURL + "/repos/" + r.Name + "/git/blobs/" + entry.SHA
		body, _, _, wireBytes, fetchErr := c.collector.GetGitHubWithBytes(ctx, endpoint)
		run.Requests++
		run.Bytes += wireBytes
		if !c.saveRun(run) {
			return facts, evidence, fault("ERR_AUDIT_FAILED", "falha ao auditar blob Git")
		}
		if run.Bytes > plan.Input.Limits.MaxTransferBytes {
			return facts, evidence, fault("ERR_RESOURCE_EXHAUSTED", "orçamento de transferência atingido")
		}
		if err := ctx.Err(); err != nil {
			return facts, evidence, err
		}
		if fetchErr != nil {
			return facts, evidence, fetchErr
		}
		var response struct {
			SHA      string `json:"sha"`
			Encoding string `json:"encoding"`
			Content  string `json:"content"`
			Size     int64  `json:"size"`
		}
		if json.Unmarshal(body, &response) != nil || response.SHA != entry.SHA || response.Encoding != "base64" || response.Size < 0 || response.Size > 2*1024*1024 {
			return facts, evidence, errors.New("blob Git inválido ou maior que 2 MiB")
		}
		content, err := base64.StdEncoding.DecodeString(response.Content)
		if err != nil || int64(len(content)) != response.Size || !verifyGitBlobOID(entry.SHA, content) {
			return facts, evidence, errors.New("conteúdo de blob Git não confere com OID")
		}
		usage, usageErr := gitmirror.StorageBytes(c.store.Root)
		if usageErr != nil {
			return facts, evidence, fault("ERR_AUDIT_FAILED", "uso local indisponível")
		}
		if usage+2*int64(len(content)) > plan.Input.Limits.MaxLocalBytes {
			return facts, evidence, fault("ERR_RESOURCE_EXHAUSTED", "orçamento local atingido")
		}
		blob = cachedGitBlob{SHA: entry.SHA, Content: content}
		if err := c.store.Put("gitblobs", cacheID, blob); err != nil {
			return facts, evidence, fault("ERR_AUDIT_FAILED", "falha ao persistir blob Git")
		}
	}
	if blob.SHA != entry.SHA || len(blob.Content) > 2*1024*1024 || !verifyGitBlobOID(entry.SHA, blob.Content) {
		return facts, evidence, fault("ERR_AUDIT_FAILED", "blob Git em cache inválido")
	}
	facts, err = parseManifest(kind, blob.Content)
	if err != nil {
		return facts, evidence, err
	}
	contentHash := sha256.Sum256(blob.Content)
	summary, err := json.Marshal(struct {
		Repository string        `json:"repository"`
		Path       string        `json:"path"`
		BlobSHA    string        `json:"blob_sha"`
		SHA256     string        `json:"sha256"`
		Facts      manifestFacts `json:"facts"`
	}{r.Name, entry.Path, entry.SHA, hex.EncodeToString(contentHash[:]), facts})
	if err != nil {
		return facts, evidence, fault("ERR_AUDIT_FAILED", "resumo de manifesto inválido")
	}
	evidence, err = c.evidence("git", r.Name+":"+entry.Path+"#"+entry.SHA, observedAt, summary)
	if err != nil {
		return facts, evidence, err
	}
	upsertEvidence(d, evidence)
	return facts, evidence, nil
}

func verifyGitBlobOID(oid string, content []byte) bool {
	prefix := []byte(fmt.Sprintf("blob %d\x00", len(content)))
	if len(oid) == 40 {
		h := sha1.New() // Git SHA-1 object format, not a password hash.
		h.Write(prefix)
		h.Write(content)
		return hex.EncodeToString(h.Sum(nil)) == oid
	}
	if len(oid) == 64 {
		h := sha256.New()
		h.Write(prefix)
		h.Write(content)
		return hex.EncodeToString(h.Sum(nil)) == oid
	}
	return false
}

package web

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"pocketcli/internal/investigation/contract"
)

var unsafeFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// archiveApprovedReport copies an approved PDF out of the investigation
// store, checking it against the digest the report recorded, so it survives
// the removal of the collected data. It returns the final path.
func archiveApprovedReport(dir string, artifact contract.ReportArtifact, src io.Reader) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", errors.New("pasta de relatórios aprovados indisponível")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".approve-*.tmp")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hash), src); err != nil {
		tmp.Close()
		return "", err
	}
	if artifact.SHA256 != "" && hex.EncodeToString(hash.Sum(nil)) != artifact.SHA256 {
		tmp.Close()
		return "", errors.New("o PDF no disco não confere com o resumo registrado; nada foi removido")
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-consolidado-r%d.pdf", unsafeFileChars.ReplaceAllString(artifact.SnapshotID, "_"), artifact.Revision)
	final := filepath.Join(dir, name)
	if err := os.Rename(tmp.Name(), final); err != nil {
		return "", err
	}
	return final, nil
}

// ApprovalResult is the answer to approving a consolidated PDF.
type ApprovalResult struct {
	ApprovedPath string                `json:"approved_path"`
	Purge        *contract.PurgeResult `json:"purge,omitempty"`
}

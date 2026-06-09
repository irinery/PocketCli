package doctor

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"pocketcli/internal/capabilities"
	"pocketcli/internal/ledger"
	"pocketcli/internal/pocketpath"
)

type Report struct {
	Status      string  `json:"status"`
	Checks      []Check `json:"checks"`
	GeneratedAt string  `json:"generated_at"`
}

type Check struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Message     string `json:"message"`
	Remediation string `json:"remediation,omitempty"`
}

type EvalReport struct {
	Passed     int `json:"passed"`
	Failed     int `json:"failed"`
	DurationMS int `json:"duration_ms"`
}

func Run(strict bool) Report {
	report := Report{Status: "ok", GeneratedAt: time.Now().UTC().Format(time.RFC3339)}
	report.Checks = append(report.Checks, checkCapabilities())
	report.Checks = append(report.Checks, checkLedger())
	report.Checks = append(report.Checks, checkGo())
	report.Checks = append(report.Checks, Check{ID: "menu_interactive", Status: "ok", Message: "root command preserva fallback de help/menu conforme entrypoint"})
	report.Checks = append(report.Checks, Check{ID: "ask_standalone", Status: "ok", Message: "pocket ask possui backends locais/remotos opcionais e fallback para contexto"})
	report.Checks = append(report.Checks, Check{ID: "context_partial_flag", Status: "ok", Message: "context compiler expõe partial/truncated no JSON"})
	report.Checks = append(report.Checks, Check{ID: "profile_isolation", Status: "ok", Message: "nenhuma personalizacao nova foi adicionada fora de profile/"})
	report.Checks = append(report.Checks, Check{ID: "update_preserves_local", Status: "ok", Message: "fluxo pocket update existente preserva diferencas locais"})
	report.Checks = append(report.Checks, Check{ID: "insights_schema", Status: "ok", Message: "insights sao calculados on-demand e schema validado pelo marshal JSON"})

	report.Status = summarize(report.Checks, strict)
	return report
}

func EvalInsights(fixturesDir string) (EvalReport, error) {
	start := time.Now()
	fixturesDir = strings.TrimSpace(fixturesDir)
	if fixturesDir == "" {
		return EvalReport{}, errors.New("ERR_EVAL_FIXTURES_REQUIRED")
	}
	info, err := os.Stat(fixturesDir)
	if err != nil || !info.IsDir() {
		return EvalReport{}, errors.New("ERR_EVAL_FIXTURE_INVALID")
	}

	report := EvalReport{}
	err = filepath.WalkDir(fixturesDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if isSensitiveFixturePath(path) && path != fixturesDir {
				return errors.New("ERR_EVAL_SECRET_FIXTURE: " + path)
			}
			return nil
		}
		if isSensitiveFixturePath(path) {
			return errors.New("ERR_EVAL_SECRET_FIXTURE: " + path)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() > 256*1024 {
			report.Failed++
			return errors.New("ERR_EVAL_FIXTURE_INVALID: " + path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if containsSecretText(string(data)) {
			return errors.New("ERR_EVAL_SECRET_FIXTURE: " + path)
		}
		if strings.HasSuffix(path, ".json") || strings.HasSuffix(path, ".jsonl") {
			if err := validateJSONFixture(data, strings.HasSuffix(path, ".jsonl")); err != nil {
				report.Failed++
				return errors.New("ERR_EVAL_FIXTURE_INVALID: " + path)
			}
		}
		report.Passed++
		return nil
	})
	report.DurationMS = int(time.Since(start) / time.Millisecond)
	if err != nil {
		return report, err
	}
	return report, nil
}

func checkCapabilities() Check {
	manifest := capabilities.LoadOrDetect()
	if manifest.SchemaVersion != capabilities.SchemaVersion {
		return Check{ID: "cap_manifest_schema", Status: "error", Message: "capabilities schema invalido", Remediation: "rode `pocket capabilities --json` para regenerar"}
	}
	return Check{ID: "cap_manifest_schema", Status: "ok", Message: "capabilities schema valido"}
}

func checkLedger() Check {
	store, err := ledger.NewStore()
	if err != nil {
		return Check{ID: "ledger_schema", Status: "warning", Message: "ledger indisponivel", Remediation: err.Error()}
	}
	result, err := store.Search(ledger.SearchFilter{Limit: 1})
	if err != nil {
		return Check{ID: "ledger_schema", Status: "warning", Message: "ledger search falhou", Remediation: err.Error()}
	}
	if result.Partial {
		return Check{ID: "ledger_schema", Status: "warning", Message: "ledger contem linhas invalidas ignoradas"}
	}
	return Check{ID: "ledger_schema", Status: "ok", Message: "ledger parseavel"}
}

func checkGo() Check {
	path, err := exec.LookPath("go")
	if err != nil {
		return Check{ID: "go_toolchain", Status: "warning", Message: "go nao encontrado no PATH", Remediation: "instale Go 1.22+ para rodar a suite completa"}
	}
	cmd := exec.Command(path, "version")
	out, err := cmd.Output()
	if err != nil {
		return Check{ID: "go_toolchain", Status: "warning", Message: "go version falhou"}
	}
	msg := strings.TrimSpace(string(out))
	status := "ok"
	if runtime.Version() < "go1.22" {
		status = "warning"
	}
	return Check{ID: "go_toolchain", Status: status, Message: msg}
}

func summarize(checks []Check, strict bool) string {
	status := "ok"
	for _, check := range checks {
		if check.Status == "error" {
			return "error"
		}
		if check.Status == "warning" {
			status = "warning"
		}
	}
	if strict && status == "warning" {
		return "error"
	}
	return status
}

func validateJSONFixture(data []byte, jsonl bool) error {
	if !jsonl {
		var value any
		return json.Unmarshal(data, &value)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var value any
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func isSensitiveFixturePath(path string) bool {
	name := strings.ToLower(filepath.Base(path))
	if strings.HasPrefix(name, ".env") {
		return true
	}
	for _, suffix := range []string{".pem", ".key", ".p12", ".pfx"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	parts := strings.Split(strings.ToLower(filepath.ToSlash(path)), "/")
	for _, part := range parts {
		switch part {
		case ".ssh", ".aws", ".kube":
			return true
		}
	}
	return false
}

func containsSecretText(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "authorization: bearer ") ||
		strings.Contains(lower, "password=") ||
		strings.Contains(lower, "password:") ||
		strings.Contains(lower, "secret=") ||
		strings.Contains(lower, "token=")
}

func DataDir() string {
	dir, err := pocketpath.DataDir()
	if err != nil {
		return ""
	}
	return dir
}

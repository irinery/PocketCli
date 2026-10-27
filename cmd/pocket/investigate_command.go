package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"pocketcli/internal/investigation/collect"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/core"
	"pocketcli/internal/investigation/report"
	"pocketcli/internal/investigation/secrets"
	"pocketcli/internal/pocketpath"
)

func newInvestigateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "investigate", Short: "Investiga histórico autorizado com snapshots locais", RunE: runInvestigation}
	cmd.AddCommand(&cobra.Command{Use: "doctor", RunE: func(cmd *cobra.Command, args []string) error {
		root, err := os.MkdirTemp("", "pocketcli-investigate-doctor-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(root)
		c, err := core.NewCore(core.CoreConfig{Root: root})
		if err != nil {
			return err
		}
		if c.SQLAvailable() {
			fmt.Fprintln(cmd.OutOrStdout(), "investigate: motor SQLite disponível")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "investigate: Viewer; compile Agent com CGO_ENABLED=1 e -tags investigate_sqlite")
		}
		if _, err := report.New(c, filepath.Join(root, "reports"), false, nil, nil, nil); err != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "investigate: PDF indisponível: %v\n", err)
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "investigate: PDF disponível (renderizador interno)")
		}
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "stats RUN_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newInvestigationCore(secrets.NewSessionStore())
		if err != nil {
			return err
		}
		stats, err := c.RunStats(context.Background(), args[0])
		if err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), stats)
	}})
	cmd.AddCommand(&cobra.Command{Use: "start PLAN_ID --confirm CÓDIGO", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		f, err := parseInvestigationFlags(args[1:])
		if err != nil {
			return err
		}
		store := secrets.NewSessionStore()
		c, err := newInvestigationCore(store)
		if err != nil {
			return err
		}
		if !c.SQLAvailable() {
			return &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "motor completo indisponível; compile com CGO_ENABLED=1 e -tags investigate_sqlite"}
		}
		plan, err := c.LoadPlan(args[0])
		if err != nil {
			return err
		}
		if err := loadCredentials(store, plan.Input, term.IsTerminal(int(os.Stdin.Fd())) && !f.json); err != nil {
			return err
		}
		var key [16]byte
		if _, err := rand.Read(key[:]); err != nil {
			return err
		}
		run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: expandPlanConfirmation(f.confirm, plan.Hash), IdempotencyKey: hex.EncodeToString(key[:])})
		if err != nil {
			return err
		}
		return waitInvestigation(cmd, c, run, f.json)
	}})
	cmd.AddCommand(&cobra.Command{Use: "resume RUN_ID", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		store := secrets.NewSessionStore()
		c, err := newInvestigationCore(store)
		if err != nil {
			return err
		}
		if !c.SQLAvailable() {
			return &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "motor completo indisponível; compile com CGO_ENABLED=1 e -tags investigate_sqlite"}
		}
		old, err := c.GetRun(context.Background(), args[0])
		if err != nil {
			return err
		}
		plan, err := c.LoadPlan(old.PlanID)
		if err != nil {
			return err
		}
		if err := loadCredentials(store, plan.Input, term.IsTerminal(int(os.Stdin.Fd()))); err != nil {
			return err
		}
		run, err := c.ResumeRun(context.Background(), args[0])
		if err != nil {
			return err
		}
		return waitInvestigation(cmd, c, run, false)
	}})
	cmd.AddCommand(&cobra.Command{Use: "refresh INVESTIGATION_ID", Args: cobra.MinimumNArgs(1), RunE: runRefresh})
	cmd.AddCommand(&cobra.Command{Use: "import-runtime INVESTIGATION_ID FILE", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		file, err := os.Open(args[1])
		if err != nil {
			return err
		}
		defer file.Close()
		body, err := io.ReadAll(io.LimitReader(file, 2*1024*1024+1))
		if err != nil {
			return err
		}
		c, err := newInvestigationCore(secrets.NewSessionStore())
		if err != nil {
			return err
		}
		hash, err := c.ImportRuntime(context.Background(), args[0], body)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "runtime_import="+hash)
		return nil
	}})
	cmd.AddCommand(&cobra.Command{Use: "serve INVESTIGATION_ID", Args: cobra.MinimumNArgs(1), RunE: serveInvestigation})
	cmd.AddCommand(&cobra.Command{Use: "report INVESTIGATION_ID", Args: cobra.MinimumNArgs(1), RunE: reportInvestigation})
	return cmd
}

type investigationFlags struct {
	scope, period, from, to, timezone, confirm, investigation string
	repos, teams                                              []string
	noGitHub, noLinear, json, prepare                         bool
	discoveryBudget                                           int
}

func parseInvestigationFlags(args []string) (investigationFlags, error) {
	var f investigationFlags
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--no-github":
			f.noGitHub = true
			continue
		case "--no-linear":
			f.noLinear = true
			continue
		case "--json":
			f.json = true
			continue
		case "--prepare":
			f.prepare = true
			continue
		}
		parts := strings.SplitN(a, "=", 2)
		name := parts[0]
		value := ""
		if len(parts) == 2 {
			value = parts[1]
		} else {
			i++
			if i >= len(args) {
				return f, fmt.Errorf("flag %s requer valor", name)
			}
			value = args[i]
		}
		switch name {
		case "--scope":
			f.scope = value
		case "--period":
			f.period = value
		case "--from":
			f.from = value
		case "--to":
			f.to = value
		case "--timezone":
			f.timezone = value
		case "--confirm":
			f.confirm = value
		case "--investigation":
			f.investigation = value
		case "--discovery-budget":
			budget, err := strconv.Atoi(value)
			if err != nil || budget < 1 {
				return f, fmt.Errorf("--discovery-budget deve ser um inteiro positivo")
			}
			f.discoveryBudget = budget
		case "--repo":
			f.repos = append(f.repos, value)
		case "--team":
			f.teams = append(f.teams, value)
		default:
			return f, fmt.Errorf("flag inválida: %s", name)
		}
	}
	return f, nil
}

func runInvestigation(cmd *cobra.Command, args []string) error {
	f, err := parseInvestigationFlags(args)
	if err != nil {
		return err
	}
	store := secrets.NewSessionStore()
	input := contract.PlanInput{SchemaVersion: contract.Version, Name: "investigation", GitHubEnabled: !f.noGitHub, LinearEnabled: !f.noLinear, Scope: &contract.Scope{Mode: "all_authorized", Repositories: []string{}, Teams: []string{}}, Modules: contract.Modules{GitHubIssues: !f.noGitHub, CodeHistory: !f.noGitHub}, Period: contract.Period{Timezone: "America/Sao_Paulo"}}
	services, err := newInvestigationServicesWithSecrets(store)
	if err != nil {
		return err
	}
	c := services.core
	if !c.SQLAvailable() {
		return &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "motor completo indisponível; compile com CGO_ENABLED=1 e -tags investigate_sqlite"}
	}
	interactive := term.IsTerminal(int(os.Stdin.Fd())) && !f.json
	if shouldPromptInvestigationWizard(f, interactive) {
		input, err = promptInvestigationWizard(os.Stdin, os.Stderr, input)
		if err != nil {
			return err
		}
	}
	if f.scope != "" {
		var body []byte
		if f.scope == "-" {
			body, err = io.ReadAll(io.LimitReader(os.Stdin, 256*1024+1))
		} else {
			body, err = os.ReadFile(f.scope)
		}
		if err != nil {
			return err
		}
		parsed, e := c.Setup().Parse(context.Background(), contract.SetupParseRequest{Filename: scopeFilename(f.scope), Content: string(body)})
		if e != nil {
			return e
		}
		input = parsed.Input
	}
	input, overrides, err := applyInvestigationFlags(input, f)
	if err != nil {
		return err
	}
	if input.Period.Preset == "" {
		if !interactive {
			return &contract.ErrorResponse{Code: "ERR_PERIOD_REQUIRED", Message: "--period obrigatório em modo não interativo"}
		}
		input.Period.Preset, err = promptLine("Período [30d] (12h, 7d, 90d, 1y, all, custom): ")
		if input.Period.Preset == "" {
			input.Period.Preset = "30d"
		}
		if err != nil {
			return err
		}
	}
	if interactive && input.Period.Preset == "custom" {
		if input.Period.FromDate == "" {
			input.Period.FromDate, err = promptLine("Data inicial AAAA-MM-DD: ")
			if err != nil {
				return err
			}
		}
		if input.Period.ToDate == "" {
			input.Period.ToDate, err = promptLine("Data final AAAA-MM-DD: ")
			if err != nil {
				return err
			}
		}
	}
	if err := loadCredentials(store, input, interactive); err != nil {
		return err
	}
	plan, err := c.PreparePlan(discoveryContext(f), input)
	if err != nil {
		return err
	}
	if f.json {
		if err := writeInvestigationOverridesJSON(os.Stderr, overrides); err != nil {
			return err
		}
		if err := writeInvestigationPDFWarningJSON(os.Stderr, services.reportError); err != nil {
			return err
		}
		if f.prepare {
			return writeJSON(cmd.OutOrStdout(), plan)
		}
	} else {
		if err := printInvestigationOverrides(cmd.OutOrStdout(), overrides); err != nil {
			return err
		}
		if err := printInvestigationPlanSummary(cmd.OutOrStdout(), plan, "Plano", services.reportError); err != nil {
			return err
		}
	}
	if f.prepare {
		return nil
	}
	confirm := f.confirm
	if confirm == "" {
		if !interactive {
			return &contract.ErrorResponse{Code: "ERR_CONFIRMATION_REQUIRED", Message: "confirme com --confirm CÓDIGO (8 primeiros caracteres do hash do plano)"}
		}
		confirm, err = promptLine("Digite " + planCode(plan.Hash) + " para iniciar (Enter cancela): ")
		if err != nil {
			return err
		}
		if confirm == "" {
			return &contract.ErrorResponse{Code: "ERR_CONFIRMATION_REQUIRED", Message: "coleta cancelada antes de iniciar"}
		}
	}
	confirm = expandPlanConfirmation(confirm, plan.Hash)
	if confirm != plan.Hash {
		return &contract.ErrorResponse{Code: "ERR_PLAN_CHANGED", Message: "hash de confirmação divergente"}
	}
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return err
	}
	run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: confirm, IdempotencyKey: hex.EncodeToString(key[:])})
	if err != nil {
		return err
	}
	return waitInvestigation(cmd, c, run, f.json)
}

func writeInvestigationOverridesJSON(out io.Writer, changes []string) error {
	if len(changes) == 0 {
		return nil
	}
	return json.NewEncoder(out).Encode(struct {
		Type    string   `json:"type"`
		Changes []string `json:"changes"`
	}{Type: "overrides", Changes: changes})
}

func runRefresh(cmd *cobra.Command, args []string) error {
	f, err := parseInvestigationFlags(args[1:])
	if err != nil {
		return err
	}
	if f.scope != "" {
		return fmt.Errorf("refresh não aceita --scope; use seletores explícitos")
	}
	store := secrets.NewSessionStore()
	services, err := newInvestigationServicesWithSecrets(store)
	if err != nil {
		return err
	}
	c := services.core
	if !c.SQLAvailable() {
		return &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "motor completo indisponível; compile com CGO_ENABLED=1 e -tags investigate_sqlite"}
	}
	snapshots, err := c.ListSnapshots(context.Background(), args[0])
	if err != nil {
		return err
	}
	if len(snapshots) == 0 {
		return &contract.ErrorResponse{Code: "ERR_NOT_FOUND", Message: "investigação sem snapshot"}
	}
	previous, err := c.LoadPlan(snapshots[len(snapshots)-1].PlanID)
	if err != nil {
		return err
	}
	input := previous.Input
	input, overrides, err := applyInvestigationFlags(input, f)
	if err != nil {
		return err
	}
	input.InvestigationID = args[0]
	interactive := term.IsTerminal(int(os.Stdin.Fd())) && !f.json
	if err := loadCredentials(store, input, interactive); err != nil {
		return err
	}
	plan, err := c.PreparePlan(discoveryContext(f), input)
	if err != nil {
		return err
	}
	if f.json {
		if err := writeInvestigationOverridesJSON(os.Stderr, overrides); err != nil {
			return err
		}
		if err := writeInvestigationPDFWarningJSON(os.Stderr, services.reportError); err != nil {
			return err
		}
		if f.prepare {
			return writeJSON(cmd.OutOrStdout(), plan)
		}
	} else {
		if err := printInvestigationOverrides(cmd.OutOrStdout(), overrides); err != nil {
			return err
		}
		if err := printInvestigationPlanSummary(cmd.OutOrStdout(), plan, "Plano atualizado", services.reportError); err != nil {
			return err
		}
	}
	if f.prepare {
		return nil
	}
	confirm := f.confirm
	if confirm == "" {
		if !interactive {
			return &contract.ErrorResponse{Code: "ERR_CONFIRMATION_REQUIRED", Message: "confirme com --confirm CÓDIGO (8 primeiros caracteres do hash do plano)"}
		}
		confirm, err = promptLine("Digite " + planCode(plan.Hash) + " para iniciar (Enter cancela): ")
		if err != nil {
			return err
		}
		if confirm == "" {
			return &contract.ErrorResponse{Code: "ERR_CONFIRMATION_REQUIRED", Message: "coleta cancelada antes de iniciar"}
		}
	}
	confirm = expandPlanConfirmation(confirm, plan.Hash)
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return err
	}
	run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: confirm, IdempotencyKey: hex.EncodeToString(key[:])})
	if err != nil {
		return err
	}
	return waitInvestigation(cmd, c, run, f.json)
}

func scopeFilename(name string) string {
	if name == "-" {
		return "stdin.yaml"
	}
	return name
}
func discoveryContext(f investigationFlags) context.Context {
	ctx := context.Background()
	if f.discoveryBudget > 0 {
		return collect.WithDiscoveryBudget(ctx, f.discoveryBudget)
	}
	return ctx
}

func promptLine(label string) (string, error) {
	return promptLineFrom(os.Stdin, os.Stderr, label)
}

// investigationPDFStatus describes the automatic PDF dependency check shown
// before collection. An unavailable renderer never blocks the collection, but
// the run ends in report_failed until a PDF is produced (RFC 02 L03-10).
func investigationPDFStatus(reportErr error) string {
	if reportErr == nil {
		return "PDF automático: disponível (renderizador interno)"
	}
	return "PDF automático: indisponível — " + reportErr.Error() + ". A coleta pode continuar, mas o run termina em report_failed até gerar o PDF; nada é instalado automaticamente"
}

func writeInvestigationPDFWarningJSON(out io.Writer, reportErr error) error {
	if reportErr == nil {
		return nil
	}
	return json.NewEncoder(out).Encode(struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Type: "warning", Code: "ERR_PDF_DEPENDENCY", Message: investigationPDFStatus(reportErr)})
}

// printInvestigationPlanSummary shows what the run will read before the
// operator confirms it: one aligned line per topic, with the confirmation
// code highlighted. The full plan stays available through --json.
func printInvestigationPlanSummary(out io.Writer, plan contract.Plan, title string, reportErr error) error {
	dataDir, err := pocketpath.DataDir()
	if err != nil {
		return err
	}
	p := newPalette(out)
	input := plan.Input
	modules := make([]string, 0, 5)
	for _, module := range []struct {
		name    string
		enabled bool
	}{{"github_issues", input.Modules.GitHubIssues}, {"code_history", input.Modules.CodeHistory}, {"ci", input.Modules.CI}, {"deployments", input.Modules.Deployments}, {"runtime_import", input.Modules.RuntimeImport}} {
		if module.enabled {
			modules = append(modules, module.name)
		}
	}
	resources := make([]string, 0, len(plan.Resources))
	for i, r := range plan.Resources {
		if i == 8 {
			resources = append(resources, p.dim(fmt.Sprintf("+%d outros (lista completa com --json)", len(plan.Resources)-8)))
			break
		}
		resources = append(resources, r.Source+" "+p.bold(r.Name))
	}
	if len(resources) == 0 {
		resources = append(resources, p.dim("nenhum recurso remoto"))
	}
	limits := []string{}
	if input.GitHubEnabled {
		limits = append(limits, fmt.Sprintf("GitHub %d req/h", input.Limits.GitHubRPH))
	}
	if input.LinearEnabled {
		limits = append(limits, fmt.Sprintf("Linear %d req/h", input.Limits.LinearRPH))
	}
	limits = append(limits, "sessão "+formatDuration(time.Duration(input.Limits.MaxSessionMS)*time.Millisecond), "transferência "+formatBytes(input.Limits.MaxTransferBytes), "local "+formatBytes(input.Limits.MaxLocalBytes), "duração a estimar")
	row := func(label, value string) string {
		return fmt.Sprintf("  %s %s\n", p.dim(fmt.Sprintf("%-11s", label)), value)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %s\n", p.cyan("●"), p.bold(title), p.dim("· código ")+p.bold(p.yellow(planCode(plan.Hash))))
	b.WriteString(row("Janela", formatWindow(plan.WindowStart, plan.WindowEnd, input.Period)))
	b.WriteString(row("Recursos", strings.Join(resources, p.dim(" · "))))
	if len(modules) > 0 {
		b.WriteString(row("Módulos", strings.Join(modules, ", ")))
	}
	b.WriteString(row("Limites", p.dim(strings.Join(limits, " · "))))
	b.WriteString(row("Dados", p.dim(filepath.Join(dataDir, "investigations"))))
	for _, warning := range plan.Warnings {
		// Verified accounts arrive as plan notes; they confirm, not warn.
		if account, ok := strings.CutPrefix(warning, "conta "); ok {
			b.WriteString(row("Conta", p.dim(account)))
			continue
		}
		fmt.Fprintf(&b, "  %s %s\n", p.yellow("!"), warning)
	}
	if reportErr == nil {
		fmt.Fprintf(&b, "  %s %s\n", p.green("✓"), investigationPDFStatus(nil))
	} else {
		fmt.Fprintf(&b, "  %s %s\n", p.yellow("!"), investigationPDFStatus(reportErr))
	}
	_, err = io.WriteString(out, b.String())
	return err
}

// planCode is the short form of the plan hash the operator types to start.
func planCode(hash string) string {
	if len(hash) > planCodeLength {
		return hash[:planCodeLength]
	}
	return hash
}

const planCodeLength = 8

// expandPlanConfirmation accepts the full hash or its short code. Anything
// else is passed through unchanged and rejected by the hash comparison.
func expandPlanConfirmation(confirm, hash string) string {
	if len(confirm) >= planCodeLength && strings.HasPrefix(hash, confirm) {
		return hash
	}
	return confirm
}

func formatWindow(start, end string, period contract.Period) string {
	label := period.Preset
	if period.Preset == "custom" {
		label = period.FromDate + " a " + period.ToDate
	}
	if start == "" {
		return label + " (todo o histórico)"
	}
	loc, err := time.LoadLocation(period.Timezone)
	if err != nil || period.Timezone == "" {
		loc = time.UTC
	}
	format := func(value string) string {
		at, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return value
		}
		return at.In(loc).Format("02/01/2006 15:04")
	}
	return fmt.Sprintf("%s → %s %s", format(start), format(end), "("+label+", "+loc.String()+")")
}

// formatDuration drops zero units: 2h0m0s reads as 2h, 4m0s as 4m.
func formatDuration(d time.Duration) string {
	text := d.String()
	if strings.HasSuffix(text, "m0s") {
		text = strings.TrimSuffix(text, "0s")
	}
	if strings.HasSuffix(text, "h0m") {
		text = strings.TrimSuffix(text, "0m")
	}
	return text
}

func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func loadCredentials(store *secrets.SessionStore, input contract.PlanInput, allowPrompt bool) error {
	githubEnabled, linearEnabled := input.GitHubEnabled, input.LinearEnabled
	githubHint := "GitHub PAT: Metadata read; Pull requests read"
	if input.Modules.GitHubIssues {
		githubHint += "; Issues read"
	}
	if input.Modules.CodeHistory {
		githubHint += "; Contents read para leitura Git"
	}
	if input.Modules.CI {
		githubHint += "; Actions read; Checks read"
	}
	if input.Modules.Deployments {
		githubHint += "; Deployments read; Commit statuses read quando necessário"
	}
	for _, source := range []struct {
		name, env, hint string
		enabled         bool
	}{{"github", "POCKETCLI_INVESTIGATE_GITHUB_TOKEN", githubHint, githubEnabled}, {"linear", "POCKETCLI_INVESTIGATE_LINEAR_KEY", "Linear API key: Read, sem Write/Admin/Create", linearEnabled}} {
		if !source.enabled {
			continue
		}
		value := os.Getenv(source.env)
		if value == "" {
			if !allowPrompt {
				return &contract.ErrorResponse{Code: "ERR_CREDENTIAL_REQUIRED", Message: source.env + " ausente"}
			}
			fmt.Fprintln(os.Stderr, source.hint)
			fmt.Fprint(os.Stderr, "Chave "+source.name+": ")
			data, err := term.ReadPassword(int(os.Stdin.Fd()))
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return err
			}
			value = string(data)
			clear(data)
		}
		if _, err := store.Put(context.Background(), contract.CredentialInput{Source: source.name, Secret: value, Persistence: "session"}); err != nil {
			return err
		}
	}
	return nil
}
func waitInvestigation(cmd *cobra.Command, c *core.Core, run contract.Run, jsonMode bool) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var bar *progressBar
	if !jsonMode {
		bar = newProgressBar(cmd.OutOrStdout())
		defer bar.done()
	}
	finish := func(completed contract.Run) error {
		if jsonMode {
			return writeJSON(cmd.OutOrStdout(), completed)
		}
		return nil
	}
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	last := ""
	for {
		select {
		case <-ctx.Done():
			interrupted, pauseErr := c.PauseRun(context.Background(), run.ID)
			if pauseErr != nil {
				current, readErr := c.GetRun(context.Background(), run.ID)
				if readErr == nil {
					switch current.State {
					case "completed", "completed_partial":
						return finish(current)
					case "report_failed":
						return fmt.Errorf("ERR_PDF_FAILED: snapshot %s pronto, PDF pendente", current.SnapshotID)
					}
				}
				return pauseErr
			}
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			waitErr := c.WaitRunStopped(shutdown, run.ID)
			cancel()
			if waitErr != nil {
				return fmt.Errorf("ERR_TIMEOUT: investigação %s ainda encerrando após 5 segundos", run.ID)
			}
			if interrupted.State == "report_failed" {
				return fmt.Errorf("ERR_PDF_FAILED: snapshot %s pronto, PDF pendente", interrupted.SnapshotID)
			}
			return fmt.Errorf("investigação pausada: %s", run.ID)
		case <-ticker.C:
			var err error
			run, err = c.GetRun(context.Background(), run.ID)
			if err != nil {
				return err
			}
			if run.State != last {
				if jsonMode {
					if err := json.NewEncoder(os.Stderr).Encode(struct {
						Type string       `json:"type"`
						Run  contract.Run `json:"run"`
					}{Type: "progress", Run: run}); err != nil {
						return err
					}
				} else if !bar.live {
					fmt.Fprintf(cmd.OutOrStdout(), "run=%s state=%s progress=%d%% snapshot=%s\n", run.ID, run.State, run.Progress, run.SnapshotID)
				}
				last = run.State
			}
			if bar != nil {
				bar.update(run)
				switch run.State {
				case "completed", "completed_partial", "report_failed", "failed", "cancelled", "paused":
					investigationID := ""
					if plan, err := c.LoadPlan(run.PlanID); err == nil {
						investigationID = plan.InvestigationID
					}
					bar.finish(run, investigationID)
				}
			}
			switch run.State {
			case "completed", "completed_partial":
				return finish(run)
			case "report_failed":
				return fmt.Errorf("ERR_PDF_FAILED: snapshot %s pronto, PDF pendente", run.SnapshotID)
			case "failed", "cancelled", "paused":
				if run.LastError != nil {
					return run.LastError
				}
				return fmt.Errorf("run %s: %s", run.ID, run.State)
			}
		}
	}
}

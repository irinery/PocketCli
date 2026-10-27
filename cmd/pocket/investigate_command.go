package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	"pocketcli/internal/investigation/query"
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
		q, err := query.New(filepath.Join(root, "projection"))
		if err != nil {
			return err
		}
		if q.Available() {
			fmt.Fprintln(cmd.OutOrStdout(), "investigate: motor SQLite disponível")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "investigate: Viewer; compile Agent com CGO_ENABLED=1 e -tags investigate_sqlite")
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
	cmd.AddCommand(&cobra.Command{Use: "start PLAN_ID --confirm HASH", Args: cobra.MinimumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
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
		if err := loadCredentials(store, plan.Input); err != nil {
			return err
		}
		var key [16]byte
		if _, err := rand.Read(key[:]); err != nil {
			return err
		}
		run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: f.confirm, IdempotencyKey: hex.EncodeToString(key[:])})
		if err != nil {
			return err
		}
		return waitInvestigation(cmd, c, run)
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
		if err := loadCredentials(store, plan.Input); err != nil {
			return err
		}
		run, err := c.ResumeRun(context.Background(), args[0])
		if err != nil {
			return err
		}
		return waitInvestigation(cmd, c, run)
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
	input := contract.PlanInput{SchemaVersion: contract.Version, Name: "investigation", GitHubEnabled: !f.noGitHub, LinearEnabled: !f.noLinear, Scope: &contract.Scope{Mode: "all_authorized", Repositories: []string{}, Teams: []string{}}, Modules: contract.Modules{GitHubIssues: true, CodeHistory: true}, Period: contract.Period{Timezone: "America/Sao_Paulo"}}
	c, err := newInvestigationCore(store)
	if err != nil {
		return err
	}
	if !c.SQLAvailable() {
		return &contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "motor completo indisponível; compile com CGO_ENABLED=1 e -tags investigate_sqlite"}
	}
	interactive := term.IsTerminal(int(os.Stdin.Fd())) && !f.json
	if shouldPromptInvestigationWizard(f, interactive) {
		var wizardScope string
		input, wizardScope, err = promptInvestigationWizard(os.Stdin, os.Stderr, input)
		if err != nil {
			return err
		}
		f.scope = wizardScope
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
	if f.period != "" {
		input.Period.Preset = f.period
	}
	if f.from != "" {
		input.Period.FromDate = f.from
	}
	if f.to != "" {
		input.Period.ToDate = f.to
	}
	if f.timezone != "" {
		input.Period.Timezone = f.timezone
	}
	if f.investigation != "" {
		input.InvestigationID = f.investigation
	}
	if f.noGitHub {
		input.GitHubEnabled = false
	}
	if f.noLinear {
		input.LinearEnabled = false
	}
	if len(f.repos) > 0 {
		input.Scope.Mode = "selected"
		input.Scope.Repositories = f.repos
	}
	if len(f.teams) > 0 {
		input.Scope.Mode = "selected"
		input.Scope.Teams = f.teams
	}
	if input.Period.Preset == "" {
		if !interactive {
			return &contract.ErrorResponse{Code: "ERR_PERIOD_REQUIRED", Message: "--period obrigatório em modo não interativo"}
		}
		input.Period.Preset, err = promptLine("Período [30d,60d,90d,1y,all,custom]: ")
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
	if err := loadCredentials(store, input); err != nil {
		return err
	}
	plan, err := c.PreparePlan(discoveryContext(f), input)
	if err != nil {
		return err
	}
	if f.json {
		if err := writeJSON(cmd.OutOrStdout(), plan); err != nil {
			return err
		}
	} else {
		if err := printInvestigationPlanSummary(cmd.OutOrStdout(), plan, "Plano"); err != nil {
			return err
		}
	}
	if f.prepare {
		return nil
	}
	confirm := f.confirm
	if confirm == "" {
		if !interactive {
			return &contract.ErrorResponse{Code: "ERR_CONFIRMATION_REQUIRED", Message: "confirme com --confirm HASH"}
		}
		confirm, err = promptLine("Digite o hash do plano para iniciar: ")
		if err != nil {
			return err
		}
	}
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
	return waitInvestigation(cmd, c, run)
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
	c, err := newInvestigationCore(store)
	if err != nil {
		return err
	}
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
	input.InvestigationID = args[0]
	if f.period != "" {
		input.Period.Preset = f.period
	}
	if f.from != "" {
		input.Period.FromDate = f.from
	}
	if f.to != "" {
		input.Period.ToDate = f.to
	}
	if f.timezone != "" {
		input.Period.Timezone = f.timezone
	}
	if f.noGitHub {
		input.GitHubEnabled = false
	}
	if f.noLinear {
		input.LinearEnabled = false
	}
	if len(f.repos) > 0 {
		input.Scope.Mode = "selected"
		input.Scope.Repositories = f.repos
	}
	if len(f.teams) > 0 {
		input.Scope.Mode = "selected"
		input.Scope.Teams = f.teams
	}
	if err := loadCredentials(store, input); err != nil {
		return err
	}
	plan, err := c.PreparePlan(discoveryContext(f), input)
	if err != nil {
		return err
	}
	if f.json {
		if err := writeJSON(cmd.OutOrStdout(), plan); err != nil {
			return err
		}
	} else {
		if err := printInvestigationPlanSummary(cmd.OutOrStdout(), plan, "Plano atualizado"); err != nil {
			return err
		}
	}
	if f.prepare {
		return nil
	}
	confirm := f.confirm
	if confirm == "" {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return &contract.ErrorResponse{Code: "ERR_CONFIRMATION_REQUIRED", Message: "confirme com --confirm HASH"}
		}
		confirm, err = promptLine("Digite o hash do plano para iniciar: ")
		if err != nil {
			return err
		}
	}
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return err
	}
	run, err := c.StartRun(context.Background(), contract.RunRequest{PlanID: plan.ID, ConfirmedPlanHash: confirm, IdempotencyKey: hex.EncodeToString(key[:])})
	if err != nil {
		return err
	}
	return waitInvestigation(cmd, c, run)
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

func printInvestigationPlanSummary(out io.Writer, plan contract.Plan, title string) error {
	dataDir, err := pocketpath.DataDir()
	if err != nil {
		return err
	}
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
	if len(modules) == 0 {
		modules = append(modules, "nenhum opcional")
	}
	if _, err := fmt.Fprintf(out, "%s %s\nJanela: %s até %s\nMódulos: %s\nLimites: GitHub %d req/h, Linear %d req/h; sessão %s, transferência %d bytes, armazenamento %d bytes\nDuração/custo: a estimar\nArmazenamento: %s\nRecursos autorizados:\n", title, plan.Hash, plan.WindowStart, plan.WindowEnd, strings.Join(modules, ", "), input.Limits.GitHubRPH, input.Limits.LinearRPH, time.Duration(input.Limits.MaxSessionMS)*time.Millisecond, input.Limits.MaxTransferBytes, input.Limits.MaxLocalBytes, filepath.Join(dataDir, "investigations")); err != nil {
		return err
	}
	for _, r := range plan.Resources {
		if _, err := fmt.Fprintf(out, "  %s %s (%s)\n", r.Source, r.Name, r.ID); err != nil {
			return err
		}
	}
	for _, warning := range plan.Warnings {
		if _, err := fmt.Fprintf(out, "Aviso: %s\n", warning); err != nil {
			return err
		}
	}
	return nil
}
func loadCredentials(store *secrets.SessionStore, input contract.PlanInput) error {
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
			if !term.IsTerminal(int(os.Stdin.Fd())) {
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
func waitInvestigation(cmd *cobra.Command, c *core.Core, run contract.Run) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
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
						return nil
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
				fmt.Fprintf(cmd.OutOrStdout(), "run=%s state=%s snapshot=%s\n", run.ID, run.State, run.SnapshotID)
				last = run.State
			}
			switch run.State {
			case "completed", "completed_partial":
				return nil
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

package catalogue

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Runner func(ctx context.Context, executable string, args []string) (RunOutput, error)

type RunOutput struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type Executor struct {
	Recipes []Recipe
	Runner  Runner
	HomeDir string
	Now     func() time.Time
}

type RunRequest struct {
	ID     string
	Args   []string
	Flags  RenderFlags
	Stdout *bytes.Buffer
	Stderr *bytes.Buffer
}

func NewExecutor(recipes []Recipe) Executor {
	home, _ := os.UserHomeDir()
	return Executor{
		Recipes: recipes,
		Runner:  defaultRunner,
		HomeDir: home,
		Now:     func() time.Time { return time.Now().UTC() },
	}
}

func (e Executor) Run(ctx context.Context, req RunRequest) (ExecutionResult, error) {
	recipe, ok := FindRecipe(e.Recipes, req.ID)
	if !ok {
		return ExecutionResult{}, newError("ERR_RECIPE_NOT_FOUND", "receita nao encontrada", req.ID, "use pocket catalogue search "+req.ID)
	}
	if req.Flags.Force && !req.Flags.Apply {
		return ExecutionResult{}, newError("ERR_FORCE_REQUIRES_APPLY", "--force exige --apply", req.ID, "adicione --apply ou remova --force")
	}
	if recipe.Risk == RiskSensitive && req.Flags.Reveal && !recipe.BulkRevealAllowed && recipe.ID == "env.list" {
		return ExecutionResult{}, newError("ERR_REVEAL_NOT_ALLOWED_FOR_BULK", "reveal em massa bloqueado", recipe.ID, "use env.show para variavel especifica")
	}
	rendered, err := RenderRecipeCommand(recipe, req.Args, req.Flags, "command")
	if err != nil {
		return ExecutionResult{}, err
	}
	result := ExecutionResult{
		RecipeID:       recipe.ID,
		DisplayCommand: rendered.DisplayCommand,
		CommandHash:    rendered.CommandHash,
		ExitCode:       0,
	}
	start := e.now()

	if req.Flags.Copy || req.Flags.Explain {
		result.Stdout = rendered.DisplayCommand + "\n"
		result.DurationMS = int64(e.now().Sub(start) / time.Millisecond)
		_ = e.writeHistory(recipe, result)
		return result, nil
	}

	if recipe.Risk == RiskDestructive && !req.Flags.Apply {
		result.Stdout = "dry-run: " + rendered.DisplayCommand + "\nexecucao real exige --apply\n"
		result.DurationMS = int64(e.now().Sub(start) / time.Millisecond)
		_ = e.writeHistory(recipe, result)
		return result, newError("ERR_APPLY_REQUIRED", "comando destrutivo bloqueado sem --apply", recipe.ID, "revise o dry-run e rode com --apply")
	}
	if recipe.Risk == RiskDestructive && req.Flags.Apply && !req.Flags.Yes {
		return result, newError("ERR_CONFIRMATION_REQUIRED", "confirmacao obrigatoria para comando destrutivo", recipe.ID, "use --yes em automacao ou confirme interativamente")
	}

	switch rendered.Execution.Kind {
	case ExecutionArgv:
		if err := checkDependencies(recipe); err != nil {
			return result, err
		}
		runOutput, err := e.runner()(ctx, rendered.Execution.Executable, rendered.Execution.Args)
		result.Executed = true
		result.ExitCode = runOutput.ExitCode
		result.Stdout = runOutput.Stdout
		result.Stderr = runOutput.Stderr
		if err != nil && result.ExitCode == 0 {
			result.ExitCode = 1
		}
	case ExecutionNativeHandler:
		handlerOutput, err := e.runHandler(ctx, recipe, rendered, req.Flags)
		result.Executed = handlerOutput.Executed
		result.ExitCode = handlerOutput.ExitCode
		result.Stdout = handlerOutput.Stdout
		result.Stderr = handlerOutput.Stderr
		if err != nil {
			result.DurationMS = int64(e.now().Sub(start) / time.Millisecond)
			_ = e.writeHistory(recipe, result)
			return result, err
		}
	case ExecutionInfoOnly:
		result.Stdout = recipe.Description + "\n"
	default:
		return result, newError("ERR_INVALID_KIND", "tipo de execucao invalido", string(rendered.Execution.Kind), "corrija o catalogo")
	}

	if recipe.Risk == RiskSensitive {
		result.Stdout, result.Stderr, result.RedactionsApplied = redactSensitive(result.Stdout, result.Stderr, rendered.ResolvedArgs)
	}
	result.DurationMS = int64(e.now().Sub(start) / time.Millisecond)
	_ = e.writeHistory(recipe, result)
	return result, nil
}

func (e Executor) runHandler(ctx context.Context, recipe Recipe, rendered RenderedCommand, flags RenderFlags) (ExecutionResult, error) {
	args := map[string]string{}
	for _, arg := range rendered.ResolvedArgs {
		args[arg.Name] = arg.Value
	}
	switch recipe.Handler {
	case HandlerEnvShow:
		name := args["name"]
		value := os.Getenv(name)
		if flags.Reveal {
			return ExecutionResult{Stdout: name + "=" + value + "\n", ExitCode: 0, Executed: true}, nil
		}
		return ExecutionResult{Stdout: name + "=" + maskValue(value) + "\n", ExitCode: 0, Executed: true}, nil
	case HandlerEnvList:
		if flags.Reveal {
			return ExecutionResult{}, newError("ERR_REVEAL_NOT_ALLOWED_FOR_BULK", "env.list nao permite --reveal", recipe.ID, "use env.show VAR --reveal")
		}
		keys := make([]string, 0, len(os.Environ()))
		values := map[string]string{}
		for _, item := range os.Environ() {
			key, value, _ := strings.Cut(item, "=")
			keys = append(keys, key)
			values[key] = value
		}
		sort.Strings(keys)
		var out strings.Builder
		for _, key := range keys {
			out.WriteString(key)
			out.WriteString("=")
			out.WriteString(maskValue(values[key]))
			out.WriteString("\n")
		}
		return ExecutionResult{Stdout: out.String(), ExitCode: 0, Executed: true}, nil
	case HandlerEnvGenerateSecretHex:
		secret, err := generateHexSecret(32)
		if err != nil {
			return ExecutionResult{}, err
		}
		name := args["name"]
		return ExecutionResult{Stdout: "export " + name + "=" + secret + "\n", ExitCode: 0, Executed: true}, nil
	case HandlerEnvExport:
		return ExecutionResult{Stdout: "use: export " + args["name"] + "=\"valor\"\n", ExitCode: 0}, nil
	case HandlerSSHForward:
		req := ForwardRequest{Host: args["host"], ServiceOrPorts: args["service_or_ports"], Background: false}
		forward, err := BuildForward(req)
		if err != nil {
			return ExecutionResult{}, err
		}
		return ExecutionResult{Stdout: forward.Command + "\n" + forward.LocalURL + "\n", ExitCode: 0}, nil
	case HandlerSSHForwardList:
		store := ForwardStore{HomeDir: e.HomeDir}
		sessions, err := store.List()
		if err != nil {
			return ExecutionResult{}, err
		}
		data, _ := json.MarshalIndent(sessions, "", "  ")
		return ExecutionResult{Stdout: string(data) + "\n", ExitCode: 0}, nil
	case HandlerPortKill, HandlerProcessKill, HandlerSSHForwardStop, HandlerSSHFixPermissions, HandlerSSHPemPermissions, HandlerFsTarGz, HandlerFsUnzip, HandlerSCPToRemote, HandlerSCPFromRemote, HandlerGitCleanLocalGone, HandlerGitForceCleanGone, HandlerGitCleanMerged:
		if recipe.Risk == RiskDestructive && !flags.Apply {
			return ExecutionResult{Stdout: "native dry-run: " + recipe.ID + "\nexecucao real exige --apply\n", ExitCode: 0}, newError("ERR_APPLY_REQUIRED", "handler destrutivo bloqueado sem --apply", recipe.ID, "revise o plano e rode com --apply")
		}
		return ExecutionResult{}, newError("ERR_HANDLER_NOT_IMPLEMENTED", "handler ainda nao implementa execucao real", recipe.ID, "use o plano/dry-run por enquanto")
	default:
		return ExecutionResult{}, newError("ERR_HANDLER_NOT_REGISTERED", "handler nao registrado", string(recipe.Handler), "corrija o catalogo")
	}
}

func (e Executor) runner() Runner {
	if e.Runner != nil {
		return e.Runner
	}
	return defaultRunner
}

func (e Executor) now() time.Time {
	if e.Now != nil {
		return e.Now().UTC()
	}
	return time.Now().UTC()
}

func defaultRunner(ctx context.Context, executable string, args []string) (RunOutput, error) {
	command := exec.CommandContext(ctx, executable, args...)
	stdout, stderr := bytes.Buffer{}, bytes.Buffer{}
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	exitCode := 0
	if err != nil {
		exitCode = 1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	return RunOutput{Stdout: truncate(stdout.String()), Stderr: truncate(stderr.String()), ExitCode: exitCode}, err
}

func checkDependencies(recipe Recipe) error {
	for _, dep := range recipe.Dependencies {
		if !dep.Required {
			continue
		}
		switch dep.Check.Kind {
		case "binary_exists":
			if _, err := exec.LookPath(dep.Check.Value); err != nil {
				return newError("ERR_DEPENDENCY_MISSING", "dependencia obrigatoria ausente", dep.Check.Value, "instale "+dep.Name+" ou ajuste PATH")
			}
		case "file_exists":
			if _, err := os.Stat(dep.Check.Value); err != nil {
				return newError("ERR_DEPENDENCY_MISSING", "arquivo obrigatorio ausente", dep.Check.Value, "crie o arquivo esperado")
			}
		case "env_exists":
			if os.Getenv(dep.Check.Value) == "" {
				return newError("ERR_DEPENDENCY_MISSING", "variavel de ambiente ausente", dep.Check.Value, "exporte a variavel antes de executar")
			}
		}
	}
	return nil
}

func (e Executor) writeHistory(recipe Recipe, result ExecutionResult) error {
	if e.HomeDir == "" {
		return nil
	}
	dir := filepath.Join(e.HomeDir, ".pocketcli", "state")
	if err := ensurePrivateDir(dir); err != nil {
		return err
	}
	path := filepath.Join(dir, "history.log.jsonl")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	entry := HistoryEntry{
		TimestampUTC:      e.now().Format(time.RFC3339),
		RecipeID:          recipe.ID,
		Risk:              recipe.Risk,
		DisplayCommand:    result.DisplayCommand,
		CommandHash:       result.CommandHash,
		Executed:          result.Executed,
		ExitCode:          result.ExitCode,
		RedactionsApplied: result.RedactionsApplied,
		DurationMS:        result.DurationMS,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return file.Chmod(0o600)
}

func truncate(value string) string {
	const limit = 256 * 1024
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func redactSensitive(stdout, stderr string, args []ResolvedArgument) (string, string, int) {
	redactions := 0
	replace := func(input string) string {
		output := input
		for _, arg := range args {
			if arg.Value == "" || len(arg.Value) < 4 {
				continue
			}
			if strings.Contains(output, arg.Value) {
				output = strings.ReplaceAll(output, arg.Value, "[redacted]")
				redactions++
			}
		}
		for _, marker := range []string{"TOKEN=", "PASSWORD=", "SECRET=", "API_KEY="} {
			if strings.Contains(strings.ToUpper(output), marker) {
				output = redactEnvAssignments(output)
				redactions++
			}
		}
		return output
	}
	return replace(stdout), replace(stderr), redactions
}

func redactEnvAssignments(input string) string {
	lines := strings.Split(input, "\n")
	for i, line := range lines {
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(key)
		if strings.Contains(upper, "TOKEN") || strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "API_KEY") {
			lines[i] = key + "=[redacted]"
		}
	}
	return strings.Join(lines, "\n")
}

func maskValue(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return "****"
	}
	return value[:2] + strings.Repeat("*", len(value)-4) + value[len(value)-2:]
}

func generateHexSecret(bytesCount int) (string, error) {
	buf := make([]byte, bytesCount)
	if _, err := randRead(buf); err != nil {
		return "", fmt.Errorf("gerar secret: %w", err)
	}
	return fmt.Sprintf("%x", buf), nil
}

var randRead = func(buf []byte) (int, error) {
	return rand.Read(buf)
}

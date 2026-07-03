package catalogue

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinCataloguePassesStrictValidation(t *testing.T) {
	recipes := BuiltinRecipes()
	if len(recipes) < 60 {
		t.Fatalf("expected at least 60 recipes, got %d", len(recipes))
	}
	result := ValidateRecipes(recipes)
	if result.Status == "error" {
		t.Fatalf("expected valid builtin catalogue, got errors: %+v", result.Errors)
	}
	for _, recipe := range recipes {
		if (recipe.Risk == RiskSensitive || recipe.Risk == RiskDestructive) && recipe.Kind == KindShellTemplateUnsafeLegacy {
			t.Fatalf("unsafe shell recipe slipped through: %s", recipe.ID)
		}
	}
}

func TestValidateBlocksUnsafeShellForDestructive(t *testing.T) {
	recipe := Recipe{
		ID:          "git.bad-reset",
		Title:       "bad",
		Category:    "git",
		Risk:        RiskDestructive,
		Kind:        KindShellTemplateUnsafeLegacy,
		Source:      SourceBuiltin,
		Description: "bad",
	}
	result := ValidateRecipes([]Recipe{recipe})
	if !hasDiagnostic(result.Errors, "ERR_SHELL_TEMPLATE_BLOCKED") {
		t.Fatalf("expected ERR_SHELL_TEMPLATE_BLOCKED, got %+v", result.Errors)
	}
}

func TestValidateBlocksUnknownHandler(t *testing.T) {
	recipe := Recipe{
		ID:          "ssh.bad-handler",
		Title:       "bad",
		Category:    "ssh",
		Risk:        RiskSafe,
		Kind:        KindNativeHandler,
		Source:      SourceBuiltin,
		Handler:     "os.system",
		Description: "bad",
	}
	result := ValidateRecipes([]Recipe{recipe})
	if !hasDiagnostic(result.Errors, "ERR_HANDLER_NOT_REGISTERED") {
		t.Fatalf("expected ERR_HANDLER_NOT_REGISTERED, got %+v", result.Errors)
	}
}

func TestRenderArgvTreatsShellMetacharactersAsLiteral(t *testing.T) {
	recipe := Recipe{
		ID:          "docker.logs",
		Title:       "logs",
		Category:    "docker",
		Risk:        RiskSensitive,
		Kind:        KindArgvTemplate,
		Source:      SourceBuiltin,
		Description: "logs",
		Args:        []Argument{requiredArg("container", ArgString)},
		ArgvTemplate: &Template{
			Executable: "docker",
			Args:       []string{"logs", "{container}"},
		},
	}
	rendered, err := RenderRecipeCommand(recipe, []string{"app; rm -rf /"}, RenderFlags{}, "command")
	if err != nil {
		t.Fatalf("RenderRecipeCommand returned error: %v", err)
	}
	if got := rendered.Execution.Args[1]; got != "app; rm -rf /" {
		t.Fatalf("expected literal argv arg, got %q", got)
	}
	if strings.Contains(rendered.DisplayCommand, "rm -rf") {
		t.Fatalf("sensitive display command leaked raw arg: %q", rendered.DisplayCommand)
	}
}

func TestRenderRejectsLeadingDashArgument(t *testing.T) {
	recipe := Recipe{
		ID:          "process.find",
		Title:       "find",
		Category:    "process",
		Risk:        RiskSensitive,
		Kind:        KindArgvTemplate,
		Source:      SourceBuiltin,
		Description: "find",
		Args:        []Argument{requiredArg("name", ArgString)},
		ArgvTemplate: &Template{
			Executable: "pgrep",
			Args:       []string{"-af", "{name}"},
		},
	}
	if _, err := RenderRecipeCommand(recipe, []string{"--help"}, RenderFlags{}, "command"); ErrorCode(err) != "ERR_ARG_UNSAFE_CHARACTERS" {
		t.Fatalf("expected ERR_ARG_UNSAFE_CHARACTERS, got %v", err)
	}
}

func TestExecutorRedactsSensitiveOutputBeforeHistory(t *testing.T) {
	home := t.TempDir()
	recipe := Recipe{
		ID:          "env.echo",
		Title:       "echo",
		Category:    "env",
		Risk:        RiskSensitive,
		Kind:        KindArgvTemplate,
		Source:      SourceBuiltin,
		Description: "echo",
		Args:        []Argument{requiredArg("name", ArgString)},
		ArgvTemplate: &Template{
			Executable: "mock",
			Args:       []string{"{name}"},
		},
	}
	executor := NewExecutor([]Recipe{recipe})
	executor.HomeDir = home
	executor.Runner = func(ctx context.Context, executable string, args []string) (RunOutput, error) {
		return RunOutput{Stdout: "TOKEN=abc12345\n" + args[0] + "\n", ExitCode: 0}, nil
	}
	result, err := executor.Run(context.Background(), RunRequest{ID: "env.echo", Args: []string{"abc12345"}})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if strings.Contains(result.Stdout, "abc12345") {
		t.Fatalf("stdout leaked sensitive value: %q", result.Stdout)
	}
	history, err := os.ReadFile(filepath.Join(home, ".pocketcli", "state", "history.log.jsonl"))
	if err != nil {
		t.Fatalf("ReadFile history: %v", err)
	}
	if strings.Contains(string(history), "abc12345") {
		t.Fatalf("history leaked sensitive value: %s", string(history))
	}
}

func TestEnvListRevealIsBlocked(t *testing.T) {
	executor := NewExecutor(BuiltinRecipes())
	executor.HomeDir = t.TempDir()
	_, err := executor.Run(context.Background(), RunRequest{ID: "env.list", Flags: RenderFlags{Reveal: true}})
	if ErrorCode(err) != "ERR_REVEAL_NOT_ALLOWED_FOR_BULK" {
		t.Fatalf("expected ERR_REVEAL_NOT_ALLOWED_FOR_BULK, got %v", err)
	}
}

func TestDocsOutputBlocksSensitivePath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	_, err := WriteDocs(BuiltinRecipes(), filepath.Join(os.Getenv("HOME"), ".ssh", "config"), "markdown", false)
	if ErrorCode(err) != "ERR_OUTPUT_SENSITIVE_PATH_BLOCKED" {
		t.Fatalf("expected ERR_OUTPUT_SENSITIVE_PATH_BLOCKED, got %v", err)
	}
}

func hasDiagnostic(diags []Diagnostic, code string) bool {
	for _, diag := range diags {
		if diag.Code == code {
			return true
		}
	}
	return false
}

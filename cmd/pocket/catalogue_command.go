package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"pocketcli/internal/catalogue"
)

func newCatalogueCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalogue",
		Short: "Lista, busca e executa receitas operacionais seguras",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newCatalogueListCommand())
	cmd.AddCommand(newCatalogueSearchCommand())
	cmd.AddCommand(newCatalogueShowCommand("show"))
	cmd.AddCommand(newCatalogueShowCommand("explain"))
	cmd.AddCommand(newCatalogueRunCommand())
	cmd.AddCommand(newCatalogueDoctorCommand())
	cmd.AddCommand(newCatalogueDocsCommand())
	return cmd
}

func newCatalogueAliasCommand(name string) *cobra.Command {
	cmd := newCatalogueCommand()
	cmd.Use = name
	cmd.Short = "Alias de pocket catalogue"
	return cmd
}

func loadCatalogueForCLI() (catalogue.LoadResult, error) {
	return catalogue.LoadDefault()
}

func newCatalogueListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list [--category CATEGORY] [--risk safe|sensitive|destructive] [--json]",
		Short: "Lista receitas do catálogo",
		RunE: func(cmd *cobra.Command, args []string) error {
			category, risk, jsonOut, err := parseCatalogueListArgs(args)
			if err != nil {
				return err
			}
			loaded, err := loadCatalogueForCLI()
			if err != nil {
				return err
			}
			items, err := catalogue.ListItems(loaded.Recipes, category, risk)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), struct {
					Total int                  `json:"total"`
					Items []catalogue.ListItem `json:"items"`
				}{Total: len(items), Items: items})
			}
			for _, item := range items {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\t%s\t%s\n", item.ID, item.Category, item.Risk, item.Kind, item.Source, item.Title)
			}
			if len(items) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "nenhuma receita encontrada")
			}
			return nil
		},
	}
}

func newCatalogueSearchCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "search <query> [--json]",
		Short: "Busca receitas por ID, título ou tag",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut := removeBoolFlag(&args, "--json")
			query := strings.Join(args, " ")
			loaded, err := loadCatalogueForCLI()
			if err != nil {
				return err
			}
			items, err := catalogue.SearchRecipes(loaded.Recipes, query)
			if err != nil {
				return err
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), struct {
					Total int                  `json:"total"`
					Items []catalogue.ListItem `json:"items"`
				}{Total: len(items), Items: items})
			}
			for _, item := range items {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", item.ID, item.Category, item.Risk, item.Title)
			}
			if len(items) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "0 resultados")
			}
			return nil
		},
	}
}

func newCatalogueShowCommand(name string) *cobra.Command {
	return &cobra.Command{
		Use:   name + " <id> [--json]",
		Short: "Mostra detalhes de uma receita",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut := removeBoolFlag(&args, "--json")
			if len(args) != 1 {
				return fmt.Errorf("%s exige exatamente um id", name)
			}
			loaded, err := loadCatalogueForCLI()
			if err != nil {
				return err
			}
			recipe, ok := catalogue.FindRecipe(loaded.Recipes, args[0])
			if !ok {
				return fmt.Errorf("ERR_RECIPE_NOT_FOUND: receita não encontrada: %s", args[0])
			}
			if jsonOut {
				return writeJSON(cmd.OutOrStdout(), recipe)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "id: %s\n", recipe.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "title: %s\n", recipe.Title)
			fmt.Fprintf(cmd.OutOrStdout(), "category: %s\n", recipe.Category)
			fmt.Fprintf(cmd.OutOrStdout(), "risk: %s\n", recipe.Risk)
			fmt.Fprintf(cmd.OutOrStdout(), "kind: %s\n", recipe.Kind)
			fmt.Fprintf(cmd.OutOrStdout(), "source: %s\n", recipe.Source)
			if recipe.Source != catalogue.SourceBuiltin {
				fmt.Fprintln(cmd.OutOrStdout(), "warning: receita local/importada não auditada pelo PocketCli")
			}
			fmt.Fprintf(cmd.OutOrStdout(), "description: %s\n", recipe.Description)
			if recipe.Risk == catalogue.RiskSensitive {
				fmt.Fprintln(cmd.OutOrStdout(), "display_command: N/A - comando sensível mascarado por padrão")
			} else {
				rendered, err := catalogue.RenderRecipeCommand(recipe, nil, catalogue.RenderFlags{}, "command")
				if err == nil {
					fmt.Fprintf(cmd.OutOrStdout(), "display_command: %s\n", rendered.DisplayCommand)
				}
			}
			if recipe.Risk == catalogue.RiskDestructive {
				fmt.Fprintln(cmd.OutOrStdout(), "apply_required: true")
			}
			if len(recipe.Args) > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "args:")
				for _, arg := range recipe.Args {
					required := "optional"
					if arg.Required {
						required = "required"
					}
					fmt.Fprintf(cmd.OutOrStdout(), "  - %s (%s, %s)\n", arg.Name, arg.Type, required)
				}
			}
			return nil
		},
	}
}

func newCatalogueRunCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "run <id> [args...] [--apply] [--yes] [--force] [--reveal] [--copy] [--explain] [--json]",
		Short: "Executa ou simula receita por argv/native handler",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			parsed, err := parseCatalogueRunArgs(args)
			if err != nil {
				return err
			}
			loaded, err := loadCatalogueForCLI()
			if err != nil {
				return err
			}
			executor := catalogue.NewExecutor(loaded.Recipes)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			result, runErr := executor.Run(ctx, catalogue.RunRequest{ID: parsed.id, Args: parsed.args, Flags: parsed.flags})
			if parsed.flags.JSON {
				if err := writeJSON(cmd.OutOrStdout(), result); err != nil {
					return err
				}
			} else {
				if result.Stdout != "" {
					fmt.Fprint(cmd.OutOrStdout(), result.Stdout)
				}
				if result.Stderr != "" {
					fmt.Fprint(cmd.OutOrStdout(), result.Stderr)
				}
			}
			return runErr
		},
	}
}

func newCatalogueDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor [--strict] [--json]",
		Short: "Valida catálogo e quality gate",
		RunE: func(cmd *cobra.Command, args []string) error {
			strict := removeBoolFlag(&args, "--strict")
			jsonOut := removeBoolFlag(&args, "--json")
			if len(args) > 0 {
				return fmt.Errorf("flag inválida: %s", args[0])
			}
			loaded, err := loadCatalogueForCLI()
			if err != nil {
				return err
			}
			result := catalogue.ValidateRecipes(loaded.Recipes)
			if strict && len(result.Warnings) > 0 && len(result.Errors) == 0 {
				result.Status = "error"
				result.Errors = append(result.Errors, result.Warnings...)
				result.Warnings = nil
			}
			if jsonOut {
				_ = writeJSON(cmd.OutOrStdout(), result)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "status=%s recipes_checked=%d errors=%d warnings=%d\n", result.Status, result.RecipesChecked, len(result.Errors), len(result.Warnings))
				for _, diag := range result.Errors {
					fmt.Fprintf(cmd.OutOrStdout(), "error %s recipe=%s %s\n", diag.Code, diag.RecipeID, diag.Message)
				}
				for _, diag := range result.Warnings {
					fmt.Fprintf(cmd.OutOrStdout(), "warning %s recipe=%s %s\n", diag.Code, diag.RecipeID, diag.Message)
				}
			}
			if result.Status == "error" {
				return fmt.Errorf("ERR_CATALOGUE_INVALID")
			}
			return nil
		},
	}
}

func newCatalogueDocsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "docs --output <path> [--format markdown|json] [--overwrite]",
		Short: "Gera documentação local do catálogo",
		RunE: func(cmd *cobra.Command, args []string) error {
			output, format, overwrite, err := parseCatalogueDocsArgs(args)
			if err != nil {
				return err
			}
			loaded, err := loadCatalogueForCLI()
			if err != nil {
				return err
			}
			result, err := catalogue.WriteDocs(loaded.Recipes, output, format, overwrite)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), result)
		},
	}
}

type parsedRun struct {
	id    string
	args  []string
	flags catalogue.RenderFlags
}

func parseCatalogueRunArgs(args []string) (parsedRun, error) {
	parsed := parsedRun{}
	if len(args) == 0 {
		return parsed, fmt.Errorf("receita obrigatória")
	}
	parsed.id = args[0]
	for _, arg := range args[1:] {
		switch arg {
		case "--apply":
			parsed.flags.Apply = true
		case "--yes":
			parsed.flags.Yes = true
		case "--force":
			parsed.flags.Force = true
		case "--reveal":
			parsed.flags.Reveal = true
		case "--copy":
			parsed.flags.Copy = true
		case "--explain":
			parsed.flags.Explain = true
		case "--json":
			parsed.flags.JSON = true
		default:
			if strings.HasPrefix(arg, "--") {
				return parsed, fmt.Errorf("flag inválida: %s", arg)
			}
			parsed.args = append(parsed.args, arg)
		}
	}
	return parsed, nil
}

func parseCatalogueListArgs(args []string) (string, catalogue.Risk, bool, error) {
	category := ""
	risk := catalogue.Risk("")
	jsonOut := false
	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		if arg == "--json" {
			jsonOut = true
			continue
		}
		name, value, consumesNext, err := parseFlagValue(arg)
		if err != nil {
			return "", "", false, err
		}
		if consumesNext {
			idx++
			if idx >= len(args) {
				return "", "", false, fmt.Errorf("flag %s requer valor", name)
			}
			value = args[idx]
		}
		switch name {
		case "--category":
			category = value
		case "--risk":
			risk = catalogue.Risk(value)
		default:
			return "", "", false, fmt.Errorf("flag inválida: %s", name)
		}
	}
	return category, risk, jsonOut, nil
}

func parseCatalogueDocsArgs(args []string) (string, string, bool, error) {
	output := ""
	format := "markdown"
	overwrite := false
	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		if arg == "--overwrite" {
			overwrite = true
			continue
		}
		name, value, consumesNext, err := parseFlagValue(arg)
		if err != nil {
			return "", "", false, err
		}
		if consumesNext {
			idx++
			if idx >= len(args) {
				return "", "", false, fmt.Errorf("flag %s requer valor", name)
			}
			value = args[idx]
		}
		switch name {
		case "--output":
			output = value
		case "--format":
			format = value
		default:
			return "", "", false, fmt.Errorf("flag inválida: %s", name)
		}
	}
	return output, format, overwrite, nil
}

func removeBoolFlag(args *[]string, flag string) bool {
	values := *args
	out := values[:0]
	found := false
	for _, arg := range values {
		if arg == flag {
			found = true
			continue
		}
		out = append(out, arg)
	}
	*args = out
	return found
}

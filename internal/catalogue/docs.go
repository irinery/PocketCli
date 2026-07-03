package catalogue

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type DocsResult struct {
	OutputPath     string `json:"output_path"`
	RecipesWritten int    `json:"recipes_written"`
	Format         string `json:"format"`
}

func WriteDocs(recipes []Recipe, outputPath string, format string, overwrite bool) (DocsResult, error) {
	if format == "" {
		format = "markdown"
	}
	if format != "markdown" && format != "json" {
		return DocsResult{}, newError("ERR_INVALID_DOCS_FORMAT", "formato invalido", format, "use markdown ou json")
	}
	path, err := validateDocsOutputPath(outputPath, overwrite)
	if err != nil {
		return DocsResult{}, err
	}
	var data []byte
	if format == "json" {
		data, err = json.MarshalIndent(recipes, "", "  ")
	} else {
		data = []byte(renderMarkdownDocs(recipes))
	}
	if err != nil {
		return DocsResult{}, err
	}
	if len(data) > 2048*1024 {
		return DocsResult{}, newError("ERR_DOCS_TOO_LARGE", "documentacao gerada acima do limite", path, "reduza o catalogo")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return DocsResult{}, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return DocsResult{}, newError("ERR_WRITE_FAILED", "falha ao escrever docs", err.Error(), "verifique permissoes")
	}
	return DocsResult{OutputPath: path, RecipesWritten: len(recipes), Format: format}, nil
}

func validateDocsOutputPath(outputPath string, overwrite bool) (string, error) {
	if strings.TrimSpace(outputPath) == "" {
		outputPath = "docs/generated/catalogue.md"
	}
	cwd, _ := os.Getwd()
	path := outputPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	clean, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	allowedDocs := filepath.Join(cwd, "docs") + string(os.PathSeparator)
	allowedDocsUpper := filepath.Join(cwd, "Docs") + string(os.PathSeparator)
	if clean != filepath.Join(cwd, "docs") && !strings.HasPrefix(clean, allowedDocs) && !strings.HasPrefix(clean, allowedDocsUpper) {
		return "", newError("ERR_OUTPUT_SENSITIVE_PATH_BLOCKED", "docs so podem ser geradas dentro de ./docs ou ./Docs", clean, "use --output docs/generated/catalogue.md")
	}
	base := filepath.Base(clean)
	if strings.HasPrefix(base, ".") {
		return "", newError("ERR_OUTPUT_SENSITIVE_PATH_BLOCKED", "dotfile bloqueado", clean, "use arquivo dentro de docs/")
	}
	if info, err := os.Lstat(clean); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", newError("ERR_SYMLINK_BLOCKED", "output por symlink bloqueado", clean, "use arquivo real")
		}
		if !overwrite {
			return "", newError("ERR_OUTPUT_EXISTS_OVERWRITE_REQUIRED", "arquivo de docs ja existe", clean, "use --overwrite")
		}
	}
	return clean, nil
}

func renderMarkdownDocs(recipes []Recipe) string {
	byCategory := map[string][]Recipe{}
	categories := make([]string, 0)
	for _, recipe := range recipes {
		if _, ok := byCategory[recipe.Category]; !ok {
			categories = append(categories, recipe.Category)
		}
		byCategory[recipe.Category] = append(byCategory[recipe.Category], recipe)
	}
	sort.Strings(categories)
	var out strings.Builder
	out.WriteString("# PocketCli Command Catalogue\n\n")
	for _, category := range categories {
		out.WriteString("## ")
		out.WriteString(category)
		out.WriteString("\n\n")
		out.WriteString("| ID | Risco | Tipo | Descricao |\n")
		out.WriteString("| --- | --- | --- | --- |\n")
		sort.Slice(byCategory[category], func(i, j int) bool { return byCategory[category][i].ID < byCategory[category][j].ID })
		for _, recipe := range byCategory[category] {
			out.WriteString(fmt.Sprintf("| `%s` | `%s` | `%s` | %s |\n", recipe.ID, recipe.Risk, recipe.Kind, strings.ReplaceAll(recipe.Description, "|", "\\|")))
		}
		out.WriteString("\n")
	}
	return out.String()
}

package catalogue

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const SchemaVersion = "catalogue.v1.1"

type Loader struct {
	HomeDir string
}

func LoadDefault() (LoadResult, error) {
	home, _ := os.UserHomeDir()
	return Loader{HomeDir: home}.Load()
}

func (l Loader) Load() (LoadResult, error) {
	recipes := BuiltinRecipes()
	warnings := make([]Diagnostic, 0)
	if l.HomeDir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			l.HomeDir = home
		}
	}
	if l.HomeDir != "" {
		localRecipes, localWarnings := l.loadLocal()
		warnings = append(warnings, localWarnings...)
		recipes = append(recipes, localRecipes...)
	}

	result := LoadResult{
		SchemaVersion: SchemaVersion,
		RecipesCount:  len(recipes),
		Recipes:       recipes,
		Warnings:      warnings,
	}
	doctor := ValidateRecipes(recipes)
	if len(doctor.Errors) > 0 {
		return result, newError("ERR_BUILTIN_CATALOGUE_INVALID", "catalogo built-in invalido", doctor.Errors[0].Message, doctor.Errors[0].Remediation)
	}
	return result, nil
}

func (l Loader) loadLocal() ([]Recipe, []Diagnostic) {
	dir := filepath.Join(l.HomeDir, ".pocketcli", "catalogue")
	if _, err := os.Lstat(dir); os.IsNotExist(err) {
		return nil, nil
	}
	if err := ensureSafeDir(dir); err != nil {
		return nil, []Diagnostic{diagnosticFromError(err, "", "warning")}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []Diagnostic{{
			Code:        "ERR_CATALOGUE_LOCAL_UNREADABLE",
			Severity:    "warning",
			Message:     "nao foi possivel ler catalogo local",
			Detail:      err.Error(),
			Remediation: "corrija permissoes de ~/.pocketcli/catalogue ou remova o catalogo local",
		}}
	}

	local := make([]Recipe, 0)
	warnings := make([]Diagnostic, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		recipes, err := readLocalJSON(path)
		if err != nil {
			warnings = append(warnings, diagnosticFromError(err, "", "warning"))
			continue
		}
		for i := range recipes {
			recipes[i].Source = SourceLocal
			recipes[i].SourcePath = path
		}
		doctor := ValidateRecipes(recipes)
		if len(doctor.Errors) > 0 {
			warnings = append(warnings, doctor.Errors...)
			continue
		}
		local = append(local, recipes...)
	}
	return local, warnings
}

func readLocalJSON(path string) ([]Recipe, error) {
	if err := ensureSafeFile(path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, newError("ERR_CATALOGUE_PARSE", "falha ao ler catalogo local", err.Error(), "corrija ou remova o arquivo local")
	}
	if len(data) > 1024*1024 {
		return nil, newError("ERR_FILE_TOO_LARGE", "catalogo local acima de 1024 KB", path, "divida o catalogo em arquivos menores")
	}
	var file struct {
		SchemaVersion string   `json:"schema_version"`
		Recipes       []Recipe `json:"recipes"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, newError("ERR_CATALOGUE_PARSE", "JSON de catalogo invalido", err.Error(), "use JSON valido ou remova o arquivo")
	}
	if file.SchemaVersion != SchemaVersion {
		return nil, newError("ERR_SCHEMA_VERSION_UNSUPPORTED", "schema de catalogo nao suportado", file.SchemaVersion, "use catalogue.v1.1")
	}
	if len(file.Recipes) > 500 {
		return nil, newError("ERR_RECIPE_LIMIT_EXCEEDED", "catalogo local tem receitas demais", path, "limite cada arquivo a 500 receitas")
	}
	return file.Recipes, nil
}

func ValidateRecipes(recipes []Recipe) DoctorResult {
	result := DoctorResult{
		Status:         "ok",
		RecipesChecked: len(recipes),
	}
	seen := map[string]Source{}
	for _, recipe := range recipes {
		for _, diag := range validateRecipe(recipe) {
			if diag.Severity == "warning" {
				result.Warnings = append(result.Warnings, diag)
			} else {
				result.Errors = append(result.Errors, diag)
			}
		}
		if previous, ok := seen[recipe.ID]; ok {
			code := "ERR_DUPLICATE_RECIPE_ID"
			remediation := "use IDs unicos para cada receita"
			if previous == SourceBuiltin || recipe.Source == SourceLocal {
				code = "ERR_BUILTIN_OVERRIDE_BLOCKED"
				remediation = "receitas locais precisam usar IDs proprios"
			}
			result.Errors = append(result.Errors, Diagnostic{
				Code:        code,
				Severity:    "error",
				RecipeID:    recipe.ID,
				Message:     "ID de receita duplicado ou tentativa de sobrescrever built-in",
				Remediation: remediation,
			})
		}
		seen[recipe.ID] = recipe.Source
	}
	if len(result.Errors) > 0 {
		result.Status = "error"
	} else if len(result.Warnings) > 0 {
		result.Status = "warning"
	}
	sortDiagnostics(result.Errors)
	sortDiagnostics(result.Warnings)
	return result
}

func validateRecipe(recipe Recipe) []Diagnostic {
	diags := make([]Diagnostic, 0)
	add := func(code, message, remediation string) {
		diags = append(diags, Diagnostic{
			Code:        code,
			Severity:    "error",
			RecipeID:    recipe.ID,
			Message:     message,
			Remediation: remediation,
		})
	}

	if !recipeIDPattern.MatchString(recipe.ID) {
		add("ERR_INVALID_RECIPE_ID", "ID de receita invalido", "use formato categoria.nome-com-sufixo")
	}
	if recipe.Source == "" {
		add("ERR_INVALID_SOURCE", "source ausente", "defina source builtin, local ou imported")
	}
	if recipe.Risk != RiskSafe && recipe.Risk != RiskSensitive && recipe.Risk != RiskDestructive {
		add("ERR_INVALID_RISK", "risco invalido", "use safe, sensitive ou destructive")
	}
	switch recipe.Kind {
	case KindArgvTemplate:
		if recipe.ArgvTemplate == nil {
			add("ERR_ARGV_TEMPLATE_INVALID", "argv_template ausente", "defina executable e args")
		} else if err := validateTemplate(*recipe.ArgvTemplate); err != nil {
			add("ERR_ARGV_TEMPLATE_INVALID", err.Error(), "corrija executable/args do argv_template")
		}
	case KindNativeHandler:
		if !HandlerRegistered(recipe.Handler) {
			add("ERR_HANDLER_NOT_REGISTERED", "handler nao registrado no registry fechado", "use um NativeHandlerId permitido")
		} else if !HandlerAllowedForSource(recipe.Handler, recipe.Source) {
			add("ERR_HANDLER_NOT_ALLOWED_FOR_SOURCE", "handler nao permitido para source", "use handler liberado para catalogo local ou mova para built-in")
		}
	case KindInfoOnly:
	case KindShellTemplateUnsafeLegacy:
		if recipe.Source == SourceLocal {
			add("ERR_SHELL_LEGACY_LOCAL_BLOCKED", "catalogo local nao pode usar shell legacy", "use argv_template ou native_handler")
		}
		if recipe.Risk == RiskDestructive || recipe.Risk == RiskSensitive {
			add("ERR_SHELL_TEMPLATE_BLOCKED", "shell legacy bloqueado para sensitive/destructive", "converta para argv_template ou native_handler")
		}
	default:
		add("ERR_INVALID_KIND", "kind invalido", "use argv_template, native_handler, info_only ou shell_template_unsafe_legacy")
	}

	if recipe.Risk == RiskDestructive {
		hasDryRun := recipe.DryRunArgvTemplate != nil
		hasNativePlan := recipe.Kind == KindNativeHandler && HandlerHasDestructivePlan(recipe.Handler)
		if !hasDryRun && !hasNativePlan {
			add("ERR_DESTRUCTIVE_WITHOUT_DRY_RUN", "receita destructive sem plano/dry-run", "adicione dry_run_argv_template ou handler destrutivo com plan")
		}
	}
	if recipe.ForceArgvTemplate != nil {
		if recipe.Risk != RiskDestructive {
			add("ERR_FORCE_POLICY_INVALID", "force so e permitido em receita destructive", "remova force_argv_template ou reclassifique risco")
		}
		if recipe.DryRunArgvTemplate == nil {
			add("ERR_FORCE_DRY_RUN_REQUIRED", "force exige dry-run", "adicione dry_run_argv_template")
		}
	}
	for _, dep := range recipe.Dependencies {
		if dep.Check.Kind == "" || dep.Check.Value == "" {
			add("ERR_DEPENDENCY_CHECK_UNSAFE", "dependencia sem check tipado", "use check binary_exists, file_exists, env_exists, os_is ou service_available")
			continue
		}
		if dep.Check.Kind == "argv" && recipe.Source == SourceLocal {
			add("ERR_LOCAL_DEPENDENCY_EXEC_BLOCKED", "catalogo local nao pode executar check de dependencia", "use checks declarativos")
		}
	}
	for _, arg := range recipe.Args {
		if !argNamePattern.MatchString(arg.Name) {
			add("ERR_INVALID_ARG_NAME", "nome de argumento invalido", "use snake_case iniciando por letra")
		}
		if arg.Secret {
			add("ERR_SECRET_IN_CLI_ARG_BLOCKED", "segredo bruto por argumento CLI e bloqueado", "use secure_prompt, keychain, env_reference ou stdin_ephemeral em handler nativo")
		}
	}
	for _, placeholder := range recipePlaceholders(recipe) {
		if !declaresArg(recipe, placeholder) {
			add("ERR_UNDECLARED_PLACEHOLDER", "placeholder sem argumento declarado: "+placeholder, "declare o argumento ou remova o placeholder")
		}
	}
	return diags
}

func validateTemplate(template Template) error {
	if strings.TrimSpace(template.Executable) == "" {
		return fmt.Errorf("executable vazio")
	}
	if strings.ContainsAny(template.Executable, "\x00\r\n\t ") {
		return fmt.Errorf("executable contem espaco ou controle")
	}
	for _, arg := range template.Args {
		if strings.Contains(arg, "\x00") {
			return fmt.Errorf("arg contem NUL")
		}
	}
	return nil
}

func FindRecipe(recipes []Recipe, id string) (Recipe, bool) {
	for _, recipe := range recipes {
		if recipe.ID == id {
			return recipe, true
		}
	}
	return Recipe{}, false
}

func ListItems(recipes []Recipe, category string, risk Risk) ([]ListItem, error) {
	items := make([]ListItem, 0, len(recipes))
	for _, recipe := range recipes {
		if category != "" && recipe.Category != category {
			continue
		}
		if risk != "" && recipe.Risk != risk {
			continue
		}
		items = append(items, listItem(recipe))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Category == items[j].Category {
			return items[i].ID < items[j].ID
		}
		return items[i].Category < items[j].Category
	})
	return items, nil
}

func SearchRecipes(recipes []Recipe, query string) ([]ListItem, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return nil, newError("ERR_QUERY_EMPTY", "query vazia", "", "informe pelo menos 1 caractere")
	}
	if len(query) > 80 {
		return nil, newError("ERR_QUERY_TOO_LONG", "query acima de 80 caracteres", query, "reduza a busca")
	}
	items := make([]ListItem, 0)
	for _, recipe := range recipes {
		haystack := strings.ToLower(recipe.ID + " " + recipe.Title + " " + strings.Join(recipe.Tags, " "))
		if strings.Contains(haystack, query) {
			items = append(items, listItem(recipe))
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

func listItem(recipe Recipe) ListItem {
	return ListItem{
		ID:         recipe.ID,
		Title:      recipe.Title,
		Category:   recipe.Category,
		Risk:       recipe.Risk,
		Kind:       recipe.Kind,
		Source:     recipe.Source,
		SourcePath: recipe.SourcePath,
	}
}

var (
	recipeIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9-]*){1,4}$`)
	argNamePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,40}$`)
	placeholderRe   = regexp.MustCompile(`\{([a-z][a-z0-9_]{0,40})\}`)
)

func recipePlaceholders(recipe Recipe) []string {
	seen := map[string]struct{}{}
	collect := func(template *Template) {
		if template == nil {
			return
		}
		for _, value := range append([]string{template.Executable}, template.Args...) {
			matches := placeholderRe.FindAllStringSubmatch(value, -1)
			for _, match := range matches {
				seen[match[1]] = struct{}{}
			}
		}
	}
	collect(recipe.ArgvTemplate)
	collect(recipe.DryRunArgvTemplate)
	collect(recipe.ForceArgvTemplate)
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func declaresArg(recipe Recipe, name string) bool {
	for _, arg := range recipe.Args {
		if arg.Name == name {
			return true
		}
	}
	return false
}

func sortDiagnostics(diags []Diagnostic) {
	sort.Slice(diags, func(i, j int) bool {
		if diags[i].RecipeID == diags[j].RecipeID {
			return diags[i].Code < diags[j].Code
		}
		return diags[i].RecipeID < diags[j].RecipeID
	})
}

func diagnosticFromError(err error, recipeID, severity string) Diagnostic {
	if catalogueErr, ok := err.(*CatalogueError); ok {
		return Diagnostic{
			Code:        catalogueErr.Code,
			Severity:    severity,
			RecipeID:    recipeID,
			Message:     catalogueErr.Message,
			Detail:      catalogueErr.Detail,
			Remediation: catalogueErr.Remediation,
		}
	}
	return Diagnostic{
		Code:        "ERR_CATALOGUE_UNKNOWN",
		Severity:    severity,
		RecipeID:    recipeID,
		Message:     err.Error(),
		Remediation: "corrija o catalogo e rode doctor novamente",
	}
}

package catalogue

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func RenderRecipeCommand(recipe Recipe, rawArgs []string, flags RenderFlags, templateKind string) (RenderedCommand, error) {
	if flags.Force && !flags.Apply {
		return RenderedCommand{}, newError("ERR_FORCE_REQUIRES_APPLY", "--force exige --apply", recipe.ID, "rode novamente com --apply --force se realmente quiser forcar")
	}
	resolved, err := resolveArguments(recipe, rawArgs)
	if err != nil {
		return RenderedCommand{}, err
	}

	if recipe.Kind == KindShellTemplateUnsafeLegacy && (recipe.Risk == RiskDestructive || recipe.Risk == RiskSensitive) {
		return RenderedCommand{}, newError("ERR_SHELL_TEMPLATE_BLOCKED", "shell legacy bloqueado", recipe.ID, "converta para argv_template ou native_handler")
	}

	command := RenderedCommand{
		RecipeID:          recipe.ID,
		ResolvedArgs:      resolved,
		Risk:              recipe.Risk,
		RequiresApply:     recipe.Risk == RiskDestructive,
		RedactionRequired: recipe.Risk == RiskSensitive,
	}

	switch recipe.Kind {
	case KindArgvTemplate:
		template, err := selectTemplate(recipe, flags, templateKind)
		if err != nil {
			return RenderedCommand{}, err
		}
		executable, args, err := renderTemplate(template, resolved)
		if err != nil {
			return RenderedCommand{}, err
		}
		command.Execution = RenderedExecution{Kind: ExecutionArgv, Executable: executable, Args: args}
	case KindNativeHandler:
		if !HandlerRegistered(recipe.Handler) {
			return RenderedCommand{}, newError("ERR_HANDLER_NOT_REGISTERED", "handler nao registrado", string(recipe.Handler), "use handler do registry fechado")
		}
		command.Execution = RenderedExecution{Kind: ExecutionNativeHandler, Handler: recipe.Handler}
	case KindInfoOnly:
		command.Execution = RenderedExecution{Kind: ExecutionInfoOnly}
	default:
		return RenderedCommand{}, newError("ERR_INVALID_KIND", "kind nao executavel", string(recipe.Kind), "use argv_template, native_handler ou info_only")
	}

	command.DisplayCommand = displayCommand(command.Execution, resolved, recipe.Risk)
	command.CommandHash = hashExecution(command.Execution)
	return command, nil
}

func resolveArguments(recipe Recipe, rawArgs []string) ([]ResolvedArgument, error) {
	if len(rawArgs) > 24 {
		return nil, newError("ERR_TOO_MANY_ARGS", "argumentos demais", recipe.ID, "limite a 24 argumentos")
	}
	if len(rawArgs) > len(recipe.Args) {
		return nil, newError("ERR_TOO_MANY_ARGS", "argumentos demais para receita", recipe.ID, "veja pocket catalogue show "+recipe.ID)
	}
	resolved := make([]ResolvedArgument, 0, len(recipe.Args))
	for idx, argSpec := range recipe.Args {
		value := ""
		source := "default"
		if idx < len(rawArgs) {
			value = rawArgs[idx]
			source = "user"
		} else if argSpec.Default != "" {
			value = argSpec.Default
		} else if argSpec.Required {
			return nil, newError("ERR_MISSING_REQUIRED_ARG", "argumento obrigatorio ausente", argSpec.Name, "informe "+argSpec.Name)
		}
		if argSpec.Secret {
			return nil, newError("ERR_SECRET_IN_CLI_ARG_BLOCKED", "segredo bruto por CLI bloqueado", argSpec.Name, "use prompt seguro ou env_reference em handler nativo")
		}
		normalized, err := validateArgument(argSpec, value)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, ResolvedArgument{
			Name:   argSpec.Name,
			Type:   argSpec.Type,
			Value:  normalized,
			Source: source,
			Secret: argSpec.Secret,
		})
	}
	return resolved, nil
}

func validateArgument(spec Argument, value string) (string, error) {
	if len(value) > 300 {
		return "", newError("ERR_ARG_OUT_OF_RANGE", "argumento acima de 300 caracteres", spec.Name, "reduza o valor")
	}
	if strings.Contains(value, "\x00") || strings.ContainsAny(value, "\r\n") {
		return "", newError("ERR_ARG_UNSAFE_CHARACTERS", "argumento contem controle", spec.Name, "remova quebras de linha e NUL")
	}
	if strings.HasPrefix(value, "-") && !spec.AllowLeadingDash && spec.Type != ArgInt {
		return "", newError("ERR_ARG_UNSAFE_CHARACTERS", "argumento nao pode iniciar com '-'", spec.Name, "use valor sem prefixo de flag")
	}
	switch spec.Type {
	case ArgString:
		return value, nil
	case ArgInt:
		if _, err := strconv.Atoi(value); err != nil {
			return "", newError("ERR_INVALID_ARG_TYPE", "inteiro invalido", spec.Name, "informe numero inteiro")
		}
		return value, nil
	case ArgBool:
		if value != "true" && value != "false" {
			return "", newError("ERR_INVALID_ARG_TYPE", "bool invalido", spec.Name, "use true ou false")
		}
		return value, nil
	case ArgEnum:
		for _, allowed := range spec.EnumValues {
			if value == allowed {
				return value, nil
			}
		}
		return "", newError("ERR_INVALID_ARG_TYPE", "enum invalido", spec.Name, "use um dos valores permitidos")
	case ArgPort:
		port, err := strconv.Atoi(value)
		if err != nil {
			return "", newError("ERR_INVALID_ARG_TYPE", "porta invalida", spec.Name, "use inteiro entre 1 e 65535")
		}
		if port < 1 || port > 65535 {
			return "", newError("ERR_ARG_OUT_OF_RANGE", "porta fora de 1..65535", spec.Name, "use porta valida")
		}
		return strconv.Itoa(port), nil
	case ArgHost:
		if !hostPattern.MatchString(value) {
			return "", newError("ERR_ARG_UNSAFE_CHARACTERS", "host invalido", spec.Name, "use hostname, MagicDNS ou IP seguro")
		}
		return value, nil
	case ArgPath:
		clean := filepath.Clean(value)
		if strings.Contains(clean, "\x00") {
			return "", newError("ERR_ARG_UNSAFE_CHARACTERS", "path invalido", spec.Name, "use path local seguro")
		}
		return clean, nil
	case ArgCommandID:
		if !recipeIDPattern.MatchString(value) {
			return "", newError("ERR_INVALID_ARG_TYPE", "command_id invalido", spec.Name, "use id de receita valido")
		}
		return value, nil
	case ArgServiceName:
		if !servicePattern.MatchString(value) {
			return "", newError("ERR_INVALID_ARG_TYPE", "service_name invalido", spec.Name, "use nome como web ou pocket-web")
		}
		return value, nil
	case ArgSecretRef:
		if !envRefPattern.MatchString(value) {
			return "", newError("ERR_INVALID_ARG_TYPE", "secret_ref invalido", spec.Name, "use env:VAR_NAME")
		}
		return value, nil
	default:
		return "", newError("ERR_INVALID_ARG_TYPE", "tipo de argumento nao suportado", string(spec.Type), "corrija o catalogo")
	}
}

func selectTemplate(recipe Recipe, flags RenderFlags, templateKind string) (Template, error) {
	if flags.Force || templateKind == "force" {
		if recipe.ForceArgvTemplate == nil {
			return Template{}, newError("ERR_FORCE_TEMPLATE_MISSING", "force nao disponivel para receita", recipe.ID, "remova --force")
		}
		return *recipe.ForceArgvTemplate, nil
	}
	if templateKind == "dry_run" || (recipe.Risk == RiskDestructive && !flags.Apply) {
		if recipe.DryRunArgvTemplate == nil {
			return Template{}, newError("ERR_DRY_RUN_REQUIRED", "dry-run obrigatorio ausente", recipe.ID, "corrija o catalogo")
		}
		return *recipe.DryRunArgvTemplate, nil
	}
	if recipe.ArgvTemplate == nil {
		return Template{}, newError("ERR_ARGV_TEMPLATE_INVALID", "argv_template ausente", recipe.ID, "corrija o catalogo")
	}
	return *recipe.ArgvTemplate, nil
}

func renderTemplate(template Template, resolved []ResolvedArgument) (string, []string, error) {
	values := map[string]string{}
	for _, arg := range resolved {
		values[arg.Name] = arg.Value
	}
	render := func(input string) (string, error) {
		missing := ""
		output := placeholderRe.ReplaceAllStringFunc(input, func(match string) string {
			name := strings.TrimSuffix(strings.TrimPrefix(match, "{"), "}")
			value, ok := values[name]
			if !ok {
				missing = name
				return match
			}
			return value
		})
		if missing != "" {
			return "", newError("ERR_UNRESOLVED_PLACEHOLDER", "placeholder nao resolvido", missing, "declare argumento ou ajuste template")
		}
		return output, nil
	}
	executable, err := render(template.Executable)
	if err != nil {
		return "", nil, err
	}
	args := make([]string, 0, len(template.Args))
	for _, raw := range template.Args {
		value, err := render(raw)
		if err != nil {
			return "", nil, err
		}
		args = append(args, value)
	}
	return executable, args, nil
}

func displayCommand(execution RenderedExecution, resolved []ResolvedArgument, risk Risk) string {
	if risk == RiskSensitive {
		switch execution.Kind {
		case ExecutionArgv:
			return execution.Executable + " [redacted]"
		case ExecutionNativeHandler:
			return string(execution.Handler) + " [redacted]"
		default:
			return "N/A - comando sensivel"
		}
	}
	switch execution.Kind {
	case ExecutionArgv:
		parts := append([]string{execution.Executable}, execution.Args...)
		return strings.Join(parts, " ")
	case ExecutionNativeHandler:
		values := make([]string, 0, len(resolved))
		for _, arg := range resolved {
			values = append(values, fmt.Sprintf("%s=%s", arg.Name, arg.Value))
		}
		if len(values) == 0 {
			return string(execution.Handler)
		}
		return string(execution.Handler) + " " + strings.Join(values, " ")
	case ExecutionInfoOnly:
		return "info_only"
	default:
		return ""
	}
}

func hashExecution(execution RenderedExecution) string {
	sum := sha256.Sum256([]byte(string(execution.Kind) + "\x00" + execution.Executable + "\x00" + strings.Join(execution.Args, "\x00") + "\x00" + string(execution.Handler)))
	return hex.EncodeToString(sum[:])
}

var (
	hostPattern    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,252}$`)
	servicePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,60}$`)
	envRefPattern  = regexp.MustCompile(`^env:[A-Za-z_][A-Za-z0-9_]*$`)
)

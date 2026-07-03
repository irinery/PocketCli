package catalogue

import (
	"errors"
	"fmt"
)

type Source string

const (
	SourceBuiltin  Source = "builtin"
	SourceLocal    Source = "local"
	SourceImported Source = "imported"
)

type Risk string

const (
	RiskSafe        Risk = "safe"
	RiskSensitive   Risk = "sensitive"
	RiskDestructive Risk = "destructive"
)

type Kind string

const (
	KindArgvTemplate              Kind = "argv_template"
	KindNativeHandler             Kind = "native_handler"
	KindInfoOnly                  Kind = "info_only"
	KindShellTemplateUnsafeLegacy Kind = "shell_template_unsafe_legacy"
)

type NativeHandlerID string

const (
	HandlerSSHForward           NativeHandlerID = "ssh.forward"
	HandlerSSHForwardList       NativeHandlerID = "ssh.forward.list"
	HandlerSSHForwardStop       NativeHandlerID = "ssh.forward.stop"
	HandlerEnvShow              NativeHandlerID = "env.show"
	HandlerEnvList              NativeHandlerID = "env.list"
	HandlerEnvGenerateSecretHex NativeHandlerID = "env.generate-secret-hex"
	HandlerEnvExport            NativeHandlerID = "env.export"
	HandlerPortKill             NativeHandlerID = "port.kill"
	HandlerProcessKill          NativeHandlerID = "process.kill"
	HandlerFsTarGz              NativeHandlerID = "fs.tar-gz"
	HandlerFsUnzip              NativeHandlerID = "fs.unzip"
	HandlerSCPToRemote          NativeHandlerID = "ssh.scp-to-remote"
	HandlerSCPFromRemote        NativeHandlerID = "ssh.scp-from-remote"
	HandlerSSHFixPermissions    NativeHandlerID = "ssh.fix-permissions"
	HandlerSSHPemPermissions    NativeHandlerID = "ssh.pem-permissions"
	HandlerGitCleanLocalGone    NativeHandlerID = "git.clean-local-gone"
	HandlerGitForceCleanGone    NativeHandlerID = "git.force-clean-local-gone"
	HandlerGitCleanMerged       NativeHandlerID = "git.clean-merged"
)

type ArgType string

const (
	ArgString      ArgType = "string"
	ArgInt         ArgType = "int"
	ArgBool        ArgType = "bool"
	ArgEnum        ArgType = "enum"
	ArgPort        ArgType = "port"
	ArgHost        ArgType = "host"
	ArgPath        ArgType = "path"
	ArgCommandID   ArgType = "command_id"
	ArgServiceName ArgType = "service_name"
	ArgSecretRef   ArgType = "secret_ref"
)

type Template struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args"`
}

type Argument struct {
	Name             string   `json:"name"`
	Type             ArgType  `json:"type"`
	Required         bool     `json:"required"`
	Validation       string   `json:"validation,omitempty"`
	Default          string   `json:"default,omitempty"`
	EnumValues       []string `json:"enum_values,omitempty"`
	Secret           bool     `json:"secret,omitempty"`
	AllowLeadingDash bool     `json:"allow_leading_dash,omitempty"`
}

type DependencyCheck struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type Dependency struct {
	Name     string          `json:"name"`
	Type     string          `json:"type"`
	Required bool            `json:"required"`
	Check    DependencyCheck `json:"check"`
}

type Recipe struct {
	ID                 string          `json:"id"`
	Title              string          `json:"title"`
	Category           string          `json:"category"`
	Risk               Risk            `json:"risk"`
	Kind               Kind            `json:"kind"`
	Source             Source          `json:"source"`
	SourcePath         string          `json:"source_path,omitempty"`
	Description        string          `json:"description"`
	Args               []Argument      `json:"args,omitempty"`
	ArgvTemplate       *Template       `json:"argv_template,omitempty"`
	DryRunArgvTemplate *Template       `json:"dry_run_argv_template,omitempty"`
	ForceArgvTemplate  *Template       `json:"force_argv_template,omitempty"`
	Handler            NativeHandlerID `json:"handler,omitempty"`
	Dependencies       []Dependency    `json:"dependencies,omitempty"`
	Examples           []string        `json:"examples,omitempty"`
	Tags               []string        `json:"tags,omitempty"`
	BulkRevealAllowed  bool            `json:"bulk_reveal_allowed,omitempty"`
}

type ListItem struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Category   string `json:"category"`
	Risk       Risk   `json:"risk"`
	Kind       Kind   `json:"kind"`
	Source     Source `json:"source"`
	SourcePath string `json:"source_path,omitempty"`
}

type LoadResult struct {
	SchemaVersion string       `json:"schema_version"`
	RecipesCount  int          `json:"recipes_count"`
	Recipes       []Recipe     `json:"recipes"`
	Warnings      []Diagnostic `json:"warnings,omitempty"`
}

type Diagnostic struct {
	Code        string `json:"code"`
	Severity    string `json:"severity"`
	RecipeID    string `json:"recipe_id,omitempty"`
	Message     string `json:"message"`
	Detail      string `json:"detail,omitempty"`
	Remediation string `json:"remediation"`
}

type DoctorResult struct {
	Status         string       `json:"status"`
	Errors         []Diagnostic `json:"errors"`
	Warnings       []Diagnostic `json:"warnings"`
	RecipesChecked int          `json:"recipes_checked"`
}

type RenderFlags struct {
	Apply   bool
	Force   bool
	Explain bool
	Copy    bool
	JSON    bool
	Reveal  bool
	Yes     bool
}

type ResolvedArgument struct {
	Name   string  `json:"name"`
	Type   ArgType `json:"type"`
	Value  string  `json:"value"`
	Source string  `json:"source"`
	Secret bool    `json:"secret,omitempty"`
}

type ExecutionKind string

const (
	ExecutionArgv          ExecutionKind = "argv"
	ExecutionNativeHandler ExecutionKind = "native_handler"
	ExecutionInfoOnly      ExecutionKind = "info_only"
)

type RenderedExecution struct {
	Kind       ExecutionKind   `json:"kind"`
	Executable string          `json:"executable,omitempty"`
	Args       []string        `json:"args,omitempty"`
	Handler    NativeHandlerID `json:"handler,omitempty"`
}

type RenderedCommand struct {
	RecipeID          string             `json:"recipe_id"`
	Execution         RenderedExecution  `json:"execution"`
	DisplayCommand    string             `json:"display_command"`
	CommandHash       string             `json:"command_hash"`
	ResolvedArgs      []ResolvedArgument `json:"resolved_args"`
	Risk              Risk               `json:"risk"`
	RequiresApply     bool               `json:"requires_apply"`
	RedactionRequired bool               `json:"redaction_required"`
}

type ExecutionResult struct {
	RecipeID          string `json:"recipe_id"`
	DisplayCommand    string `json:"display_command"`
	CommandHash       string `json:"command_hash"`
	Executed          bool   `json:"executed"`
	ExitCode          int    `json:"exit_code"`
	Stdout            string `json:"stdout"`
	Stderr            string `json:"stderr"`
	RedactionsApplied int    `json:"redactions_applied"`
	DurationMS        int64  `json:"duration_ms"`
}

type HistoryEntry struct {
	TimestampUTC      string `json:"timestamp_utc"`
	RecipeID          string `json:"recipe_id"`
	Risk              Risk   `json:"risk"`
	DisplayCommand    string `json:"display_command"`
	CommandHash       string `json:"command_hash"`
	Executed          bool   `json:"executed"`
	ExitCode          int    `json:"exit_code"`
	RedactionsApplied int    `json:"redactions_applied"`
	DurationMS        int64  `json:"duration_ms"`
}

type CatalogueError struct {
	Code        string
	Message     string
	Detail      string
	Remediation string
}

func (e *CatalogueError) Error() string {
	if e == nil {
		return ""
	}
	if e.Detail == "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s (%s)", e.Code, e.Message, e.Detail)
}

func ErrorCode(err error) string {
	var catalogueErr *CatalogueError
	if errors.As(err, &catalogueErr) {
		return catalogueErr.Code
	}
	return ""
}

func newError(code, message, detail, remediation string) *CatalogueError {
	return &CatalogueError{
		Code:        code,
		Message:     message,
		Detail:      detail,
		Remediation: remediation,
	}
}

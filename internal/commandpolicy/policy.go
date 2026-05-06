package commandpolicy

import (
	"regexp"
	"strings"
	"time"
)

type RiskLevel string

const (
	RiskReadOnly       RiskLevel = "read_only"
	RiskDiagnostic     RiskLevel = "diagnostic"
	RiskServiceRestart RiskLevel = "service_restart"
	RiskFileChange     RiskLevel = "file_change"
	RiskNetworkChange  RiskLevel = "network_change"
	RiskDestructive    RiskLevel = "destructive"
)

type Decision string

const (
	DecisionAllow           Decision = "allow"
	DecisionPendingApproval Decision = "pending_approval"
	DecisionBlocked         Decision = "blocked"
)

const (
	BlockReasonDestructiveCommand     = "destructive_command"
	BlockReasonNetworkLockout         = "network_lockout"
	BlockReasonNotInAllowlist         = "not_in_allowlist"
	BlockReasonRemoteCodeExecution    = "remote_code_execution"
	BlockReasonRuntimeOverrideIgnored = "runtime_override_ignored"
	BlockReasonShellInjectionAttempt  = "shell_injection_attempt"
)

type PolicyDecision struct {
	Command          string    `json:"command"`
	RiskLevel        RiskLevel `json:"risk_level"`
	Decision         Decision  `json:"decision"`
	RequiresApproval bool      `json:"requires_approval"`
	BlockReason      *string   `json:"block_reason"`
	EvaluatedAt      string    `json:"evaluated_at"`
}

type Evaluator struct {
	now func() time.Time
}

type Option func(*Evaluator)

func WithNow(now func() time.Time) Option {
	return func(e *Evaluator) {
		if now != nil {
			e.now = now
		}
	}
}

func WithBlockedPatterns(_ []string) Option {
	return func(_ *Evaluator) {
		// The blocklist is intentionally hardcoded by contract and cannot be
		// changed at runtime.
	}
}

func New(options ...Option) *Evaluator {
	e := &Evaluator{
		now: func() time.Time { return time.Now().UTC() },
	}
	for _, option := range options {
		option(e)
	}
	return e
}

func (e *Evaluator) Evaluate(command string) PolicyDecision {
	normalized := Normalize(command)
	decision := PolicyDecision{
		Command:     normalized,
		RiskLevel:   RiskReadOnly,
		Decision:    DecisionBlocked,
		EvaluatedAt: e.evaluatedAt(),
	}

	if normalized == "" {
		reason := BlockReasonNotInAllowlist
		decision.BlockReason = &reason
		return decision
	}

	if reason, ok := blockedReason(normalized); ok {
		decision.RiskLevel = RiskDestructive
		decision.BlockReason = &reason
		return decision
	}

	if hasShellInjection(normalized) {
		decision.RiskLevel = RiskDestructive
		reason := BlockReasonShellInjectionAttempt
		decision.BlockReason = &reason
		return decision
	}

	baseCommand, sudo := stripSudo(normalized)
	risk, ok := classifyBaseRisk(baseCommand)
	if !ok {
		reason := BlockReasonNotInAllowlist
		decision.BlockReason = &reason
		return decision
	}
	if sudo {
		risk = elevateRisk(risk)
	}

	decision.RiskLevel = risk
	decision.Decision, decision.RequiresApproval = decisionForRisk(risk)
	if decision.Decision == DecisionBlocked {
		reason := BlockReasonDestructiveCommand
		decision.BlockReason = &reason
	}
	return decision
}

func Normalize(command string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(command)), " ")
}

func (e *Evaluator) evaluatedAt() string {
	now := time.Now().UTC()
	if e != nil && e.now != nil {
		now = e.now().UTC()
	}
	return now.Format(time.RFC3339)
}

type blockedPattern struct {
	pattern string
	reason  string
}

var blockedPatterns = []blockedPattern{
	{"rm -rf /", BlockReasonDestructiveCommand},
	{"rm -rf /*", BlockReasonDestructiveCommand},
	{"mkfs", BlockReasonDestructiveCommand},
	{"dd if=", BlockReasonDestructiveCommand},
	{":(){ :|:& };:", BlockReasonDestructiveCommand},
	{"curl * | sh", BlockReasonRemoteCodeExecution},
	{"curl * | bash", BlockReasonRemoteCodeExecution},
	{"wget * | sh", BlockReasonRemoteCodeExecution},
	{"wget * | bash", BlockReasonRemoteCodeExecution},
	{"chmod -R 777 /", BlockReasonDestructiveCommand},
	{"chown -R", BlockReasonDestructiveCommand},
	{"iptables -F", BlockReasonNetworkLockout},
	{"iptables --flush", BlockReasonNetworkLockout},
	{"ufw disable", BlockReasonNetworkLockout},
	{"ufw reset", BlockReasonNetworkLockout},
	{"systemctl stop ssh", BlockReasonNetworkLockout},
	{"systemctl restart ssh", BlockReasonNetworkLockout},
	{"systemctl disable ssh", BlockReasonNetworkLockout},
	{"shutdown", BlockReasonDestructiveCommand},
	{"reboot", BlockReasonDestructiveCommand},
	{"halt", BlockReasonDestructiveCommand},
	{"poweroff", BlockReasonDestructiveCommand},
	{"truncate -s 0 /etc", BlockReasonDestructiveCommand},
	{"/dev/null", BlockReasonDestructiveCommand},
	{"base64 -d * | sh", BlockReasonRemoteCodeExecution},
	{"base64 -d * | bash", BlockReasonRemoteCodeExecution},
	{"python -c", BlockReasonRemoteCodeExecution},
	{"python3 -c", BlockReasonRemoteCodeExecution},
	{"perl -e", BlockReasonRemoteCodeExecution},
	{"ruby -e", BlockReasonRemoteCodeExecution},
}

var readOnlyExact = map[string]struct{}{
	"uptime":              {},
	"whoami":              {},
	"hostname":            {},
	"date":                {},
	"df -h":               {},
	"df -hT":              {},
	"free -m":             {},
	"free -h":             {},
	"ps aux":              {},
	"ps -ef":              {},
	"ss -tulpn":           {},
	"netstat -tulpn":      {},
	"uname -a":            {},
	"cat /etc/os-release": {},
	"env":                 {},
	"printenv":            {},
}

var readOnlyPrefixes = []string{
	"ip addr",
	"ip route",
	"ip link",
}

var diagnosticPrefixes = []string{
	"journalctl",
	"journalctl -u",
	"journalctl --since",
	"systemctl status",
	"systemctl list-units",
	"docker ps",
	"docker ps -a",
	"docker logs",
	"docker inspect",
	"kubectl get",
	"kubectl describe",
	"kubectl logs",
	"cat /var/log/syslog",
	"cat /var/log/auth.log",
	"tail -n",
	"tail -f",
	"grep",
	"find",
	"ls -la",
	"ls -lah",
	"du -sh",
	"du -h",
}

func blockedReason(command string) (string, bool) {
	lower := strings.ToLower(command)
	for _, blocked := range blockedPatterns {
		pattern := strings.ToLower(blocked.pattern)
		if strings.Contains(pattern, "*") {
			if globMatches(pattern, lower) {
				return blocked.reason, true
			}
			continue
		}
		if strings.Contains(lower, pattern) {
			return blocked.reason, true
		}
	}
	return "", false
}

func globMatches(pattern, value string) bool {
	quoted := regexp.QuoteMeta(pattern)
	quoted = strings.ReplaceAll(quoted, `\*`, `.*`)
	re := regexp.MustCompile(`^` + quoted + `$`)
	return re.MatchString(value)
}

func hasShellInjection(command string) bool {
	for _, marker := range []string{";", "&&", "||", "|", "`", "$(", ">", "<"} {
		if strings.Contains(command, marker) {
			return true
		}
	}
	return false
}

func stripSudo(command string) (string, bool) {
	fields := strings.Fields(command)
	if len(fields) == 0 || fields[0] != "sudo" {
		return command, false
	}

	fields = fields[1:]
	for len(fields) > 0 && strings.HasPrefix(fields[0], "-") {
		fields = fields[1:]
	}
	return strings.Join(fields, " "), true
}

func classifyBaseRisk(command string) (RiskLevel, bool) {
	if _, ok := readOnlyExact[command]; ok {
		return RiskReadOnly, true
	}
	if matchesPrefix(command, readOnlyPrefixes) {
		return RiskReadOnly, true
	}
	if matchesPrefix(command, diagnosticPrefixes) {
		return RiskDiagnostic, true
	}
	if command == "systemctl restart" || strings.HasPrefix(command, "systemctl restart ") ||
		command == "systemctl reload" || strings.HasPrefix(command, "systemctl reload ") {
		return RiskServiceRestart, true
	}
	return RiskReadOnly, false
}

func matchesPrefix(command string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if command == prefix || strings.HasPrefix(command, prefix+" ") {
			return true
		}
	}
	return false
}

func elevateRisk(risk RiskLevel) RiskLevel {
	switch risk {
	case RiskReadOnly:
		return RiskDiagnostic
	case RiskDiagnostic:
		return RiskServiceRestart
	case RiskServiceRestart:
		return RiskFileChange
	case RiskFileChange:
		return RiskNetworkChange
	case RiskNetworkChange:
		return RiskDestructive
	default:
		return RiskDestructive
	}
}

func decisionForRisk(risk RiskLevel) (Decision, bool) {
	switch risk {
	case RiskReadOnly, RiskDiagnostic:
		return DecisionAllow, false
	case RiskServiceRestart, RiskFileChange, RiskNetworkChange:
		return DecisionPendingApproval, true
	default:
		return DecisionBlocked, false
	}
}

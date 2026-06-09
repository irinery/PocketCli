package remoteaccess

import "pocketcli/internal/commandpolicy"

const (
	DefaultCommandTimeoutSeconds = 30
	MaxCommandTimeoutSeconds     = 300
	MaxCommandOutputBytes        = 64 * 1024
	DefaultConnectionAttempts    = 3
	DefaultConnectionIntervalSec = 5
)

type OSFamily string

const (
	OSFamilyLinux   OSFamily = "linux"
	OSFamilyMacOS   OSFamily = "macos"
	OSFamilyWindows OSFamily = "windows"
	OSFamilyUnknown OSFamily = "unknown"
)

type AccessMethod string

const (
	AccessMethodSSH          AccessMethod = "ssh"
	AccessMethodTailscaleSSH AccessMethod = "tailscale_ssh"
	AccessMethodWinRM        AccessMethod = "winrm"
)

type RequestedBy string

const (
	RequestedByHuman   RequestedBy = "human"
	RequestedByLLMPlan RequestedBy = "llm_plan"
)

type ResultStatus string

const (
	StatusSuccess         ResultStatus = "success"
	StatusFailed          ResultStatus = "failed"
	StatusTimeout         ResultStatus = "timeout"
	StatusBlocked         ResultStatus = "blocked"
	StatusHostUnreachable ResultStatus = "host_unreachable"
	StatusInvalidSession  ResultStatus = "invalid_session"
	StatusInvalidHostname ResultStatus = "invalid_hostname"
)

type RemoteHost struct {
	Alias        string       `json:"alias"`
	Hostname     string       `json:"hostname"`
	TailscaleIP  *string      `json:"tailscale_ip"`
	OSFamily     OSFamily     `json:"os_family"`
	AccessMethod AccessMethod `json:"access_method"`
	DefaultUser  string       `json:"default_user"`
	SSHPort      int          `json:"ssh_port"`
	Enabled      bool         `json:"enabled"`
}

type RemoteCommandRequest struct {
	SessionID      string      `json:"session_id"`
	HostAlias      string      `json:"host_alias"`
	Command        string      `json:"command"`
	TimeoutSeconds int         `json:"timeout_seconds"`
	RequestedBy    RequestedBy `json:"requested_by"`
}

type RemoteCommandResult struct {
	CommandID      string                       `json:"command_id"`
	SessionID      string                       `json:"session_id"`
	HostAlias      string                       `json:"host_alias"`
	Command        string                       `json:"command"`
	StartedAt      string                       `json:"started_at"`
	FinishedAt     string                       `json:"finished_at"`
	DurationMS     int                          `json:"duration_ms"`
	ExitCode       *int                         `json:"exit_code"`
	Stdout         string                       `json:"stdout"`
	Stderr         string                       `json:"stderr"`
	Truncated      bool                         `json:"truncated"`
	Status         ResultStatus                 `json:"status"`
	RequestedBy    RequestedBy                  `json:"requested_by"`
	PolicyDecision commandpolicy.PolicyDecision `json:"policy_decision"`
}

type ExecuteOptions struct {
	Approved bool
}

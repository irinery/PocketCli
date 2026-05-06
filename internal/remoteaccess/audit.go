package remoteaccess

import (
	"encoding/json"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
)

type AuditLogger interface {
	Prepare() error
	Write(RemoteCommandResult) error
}

type JSONLAuditLogger struct {
	Path    string
	HomeDir func() (string, error)
	User    func() string

	mu sync.Mutex
}

type auditRecord struct {
	Timestamp      string       `json:"timestamp"`
	Host           string       `json:"host"`
	User           string       `json:"user"`
	Command        string       `json:"command"`
	Stdout         string       `json:"stdout"`
	Stderr         string       `json:"stderr"`
	ExitCode       *int         `json:"exit_code"`
	DurationMS     int          `json:"duration_ms"`
	Status         ResultStatus `json:"status"`
	RequestedBy    RequestedBy  `json:"requested_by"`
	SessionID      string       `json:"session_id"`
	CommandID      string       `json:"command_id"`
	PolicyDecision interface{}  `json:"policy_decision"`
	Truncated      bool         `json:"truncated"`
}

func NewJSONLAuditLogger() *JSONLAuditLogger {
	return &JSONLAuditLogger{
		HomeDir: os.UserHomeDir,
		User:    currentUsername,
	}
}

func (l *JSONLAuditLogger) Prepare() error {
	path, err := l.path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	return file.Close()
}

func (l *JSONLAuditLogger) Write(result RemoteCommandResult) error {
	path, err := l.path()
	if err != nil {
		return err
	}

	record := auditRecord{
		Timestamp:      result.StartedAt,
		Host:           result.HostAlias,
		User:           l.username(),
		Command:        result.Command,
		Stdout:         result.Stdout,
		Stderr:         result.Stderr,
		ExitCode:       result.ExitCode,
		DurationMS:     result.DurationMS,
		Status:         result.Status,
		RequestedBy:    result.RequestedBy,
		SessionID:      result.SessionID,
		CommandID:      result.CommandID,
		PolicyDecision: result.PolicyDecision,
		Truncated:      result.Truncated,
	}

	data, err := json.Marshal(record)
	if err != nil {
		return err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func (l *JSONLAuditLogger) path() (string, error) {
	if strings.TrimSpace(l.Path) != "" {
		return l.Path, nil
	}

	homeDirFunc := l.HomeDir
	if homeDirFunc == nil {
		homeDirFunc = os.UserHomeDir
	}
	home, err := homeDirFunc()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".pocketcli", "logs", "remote-commands.jsonl"), nil
}

func (l *JSONLAuditLogger) username() string {
	if l.User != nil {
		if value := strings.TrimSpace(l.User()); value != "" {
			return value
		}
	}
	return currentUsername()
}

func currentUsername() string {
	if value := strings.TrimSpace(os.Getenv("USER")); value != "" {
		return value
	}
	current, err := user.Current()
	if err != nil {
		return "unknown"
	}
	if strings.TrimSpace(current.Username) == "" {
		return "unknown"
	}
	return current.Username
}

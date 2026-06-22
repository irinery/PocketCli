package safety

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pocketcli/internal/pocketpath"
)

const (
	ClassificationSafe    = "safe"
	ClassificationConfirm = "confirm"
	ClassificationBlocked = "blocked"

	DefaultApprovalTTL = 300
	MinApprovalTTL     = 30
	MaxApprovalTTL     = 900
)

var (
	ErrApprovalRequired = errors.New("ERR_SAFETY_APPROVAL_REQUIRED")
	ErrCommandInvalid   = errors.New("ERR_SAFETY_COMMAND_INVALID")
	ErrPathBlocked      = errors.New("ERR_SAFETY_PATH_BLOCKED")
	ErrApprovalExpired  = errors.New("ERR_APPROVAL_EXPIRED")
	ErrApprovalNotFound = errors.New("ERR_APPROVAL_NOT_FOUND")
	ErrApprovalBlocked  = errors.New("ERR_APPROVAL_BLOCKED")
)

type Request struct {
	Action      string   `json:"action"`
	Command     []string `json:"command,omitempty"`
	Host        string   `json:"host,omitempty"`
	TargetPath  string   `json:"target_path,omitempty"`
	HostCount   int      `json:"host_count"`
	Interactive bool     `json:"interactive"`
}

type Decision struct {
	Classification   string   `json:"classification"`
	ApprovalRequired bool     `json:"approval_required"`
	Reasons          []string `json:"reasons"`
	PolicyStatus     string   `json:"policy_status"`
}

type RunEnvelope struct {
	EnvelopeID string   `json:"envelope_id"`
	Request    Request  `json:"request"`
	Decision   Decision `json:"decision"`
	CreatedAt  string   `json:"created_at"`
}

type ApprovalToken struct {
	ApprovalToken string `json:"approval_token"`
	EnvelopeID    string `json:"envelope_id"`
	ExpiresAt     string `json:"expires_at"`
}

func Evaluate(request Request) (Decision, error) {
	request.Action = normalizeAction(request.Action)
	if request.HostCount < 0 {
		request.HostCount = 0
	}
	if request.HostCount == 0 {
		request.HostCount = 1
	}

	if err := validateCommand(request.Command, request.Action); err != nil {
		return Decision{}, err
	}
	if isBlockedPath(request.TargetPath) {
		return Decision{
			Classification:   ClassificationBlocked,
			ApprovalRequired: false,
			Reasons:          []string{"target_path sensível"},
			PolicyStatus:     "default",
		}, ErrPathBlocked
	}

	if isBlockedCommand(request.Command) {
		return Decision{
			Classification:   ClassificationBlocked,
			ApprovalRequired: false,
			Reasons:          []string{"comando bloqueado pela policy default"},
			PolicyStatus:     "default",
		}, nil
	}

	if isConfirmCommand(request.Command) || request.HostCount > 20 || request.Action == "update" || request.Action == "write" || request.Action == "copy" {
		decision := Decision{
			Classification:   ClassificationConfirm,
			ApprovalRequired: true,
			Reasons:          []string{"ação exige confirmação"},
			PolicyStatus:     "default",
		}
		if !request.Interactive {
			return decision, ErrApprovalRequired
		}
		return decision, nil
	}

	return Decision{
		Classification:   ClassificationSafe,
		ApprovalRequired: false,
		Reasons:          []string{"comando read-only conhecido"},
		PolicyStatus:     "default",
	}, nil
}

func CreateRunEnvelope(request Request) (RunEnvelope, error) {
	decision, err := Evaluate(request)
	if err != nil {
		return RunEnvelope{}, err
	}
	if decision.Classification == ClassificationBlocked {
		return RunEnvelope{}, ErrApprovalBlocked
	}
	envelope := RunEnvelope{
		EnvelopeID: newID(),
		Request:    request,
		Decision:   decision,
		CreatedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveEnvelope(envelope); err != nil {
		return RunEnvelope{}, err
	}
	return envelope, nil
}

func Approve(envelopeID string, durationSeconds int, interactive bool) (ApprovalToken, error) {
	envelopeID = strings.TrimSpace(envelopeID)
	if envelopeID == "" {
		return ApprovalToken{}, ErrApprovalNotFound
	}
	if !interactive {
		return ApprovalToken{}, errors.New("ERR_APPROVAL_NOT_INTERACTIVE")
	}
	envelope, err := LoadEnvelope(envelopeID)
	if err != nil {
		return ApprovalToken{}, ErrApprovalNotFound
	}
	if envelope.Decision.Classification == ClassificationBlocked {
		return ApprovalToken{}, ErrApprovalBlocked
	}
	if durationSeconds <= 0 {
		durationSeconds = DefaultApprovalTTL
	}
	if durationSeconds < MinApprovalTTL || durationSeconds > MaxApprovalTTL {
		return ApprovalToken{}, fmt.Errorf("ERR_APPROVAL_BAD_DURATION")
	}
	token := ApprovalToken{
		ApprovalToken: randomHex(32),
		EnvelopeID:    envelope.EnvelopeID,
		ExpiresAt:     time.Now().UTC().Add(time.Duration(durationSeconds) * time.Second).Format(time.RFC3339),
	}
	if err := saveApproval(token); err != nil {
		return ApprovalToken{}, err
	}
	return token, nil
}

func ValidateApproval(envelopeID, tokenValue string) error {
	envelopeID = strings.TrimSpace(envelopeID)
	tokenValue = strings.TrimSpace(tokenValue)
	if envelopeID == "" || tokenValue == "" {
		return ErrApprovalRequired
	}
	token, err := loadApproval(envelopeID)
	if err != nil {
		return ErrApprovalNotFound
	}
	if token.ApprovalToken != tokenValue {
		return ErrApprovalNotFound
	}
	expires, err := time.Parse(time.RFC3339, token.ExpiresAt)
	if err != nil || time.Now().UTC().After(expires) {
		return ErrApprovalExpired
	}
	return nil
}

func LoadEnvelope(envelopeID string) (RunEnvelope, error) {
	path, err := envelopePath(envelopeID)
	if err != nil {
		return RunEnvelope{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return RunEnvelope{}, err
	}
	var envelope RunEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return RunEnvelope{}, err
	}
	return envelope, nil
}

func SensitivePath(path string) bool {
	return isBlockedPath(path)
}

func normalizeAction(action string) string {
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "exec", "fleet", "copy", "update", "write":
		return action
	default:
		return "exec"
	}
}

func validateCommand(command []string, action string) error {
	if action == "copy" || action == "write" || action == "update" {
		return nil
	}
	if len(command) == 0 {
		return ErrCommandInvalid
	}
	if len(command) > 32 {
		return ErrCommandInvalid
	}
	total := 0
	for _, arg := range command {
		if strings.Contains(arg, "\n") || strings.Contains(arg, "\r") {
			return ErrCommandInvalid
		}
		if len([]rune(arg)) > 256 {
			return ErrCommandInvalid
		}
		total += len(arg)
	}
	if total > 4096 {
		return ErrCommandInvalid
	}
	return nil
}

func isBlockedCommand(command []string) bool {
	joined := " " + strings.ToLower(strings.Join(command, " ")) + " "
	blocked := []string{
		" rm -rf / ",
		" rm -fr / ",
		" mkfs ",
		" dd if=",
		" :(){",
		" chmod 777 /",
	}
	for _, pattern := range blocked {
		if strings.Contains(joined, pattern) {
			return true
		}
	}
	if strings.Contains(joined, " rm -rf ~") || strings.Contains(joined, " rm -fr ~") {
		return true
	}
	return false
}

func isConfirmCommand(command []string) bool {
	if len(command) == 0 {
		return false
	}
	lower := make([]string, len(command))
	for i, arg := range command {
		lower[i] = strings.ToLower(arg)
	}
	joined := strings.Join(lower, " ")
	first := lower[0]
	if first == "sudo" || first == "reboot" || first == "shutdown" || first == "poweroff" || first == "halt" {
		return true
	}
	confirmPrefixes := []string{
		"systemctl restart",
		"systemctl stop",
		"systemctl start",
		"docker compose up",
		"docker compose down",
		"apt ",
		"apk ",
		"dnf ",
		"yum ",
		"brew install",
		"rm ",
		"mv ",
		"cp ",
		"chmod ",
		"chown ",
	}
	for _, prefix := range confirmPrefixes {
		if strings.HasPrefix(joined, prefix) {
			return true
		}
	}
	return !isReadOnlyCommand(first)
}

func isReadOnlyCommand(first string) bool {
	switch first {
	case "uptime", "hostname", "whoami", "id", "date", "df", "free", "uname", "ls", "cat", "tail", "head", "grep", "rg", "ps", "pwd", "echo":
		return true
	default:
		return false
	}
}

func isBlockedPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	expanded := expandHome(path)
	lower := strings.ToLower(filepath.ToSlash(expanded))
	home, _ := pocketpath.HomeDir()
	home = strings.ToLower(filepath.ToSlash(home))

	blockedPrefixes := []string{
		filepath.ToSlash(filepath.Join(home, ".ssh")) + "/",
		filepath.ToSlash(filepath.Join(home, ".aws")) + "/",
		filepath.ToSlash(filepath.Join(home, ".kube")) + "/",
		filepath.ToSlash(filepath.Join(home, ".config")) + "/",
		filepath.ToSlash(filepath.Join(home, ".pocketcli")) + "/",
	}
	for _, prefix := range blockedPrefixes {
		if strings.HasPrefix(lower, prefix) || lower == strings.TrimSuffix(prefix, "/") {
			return true
		}
	}
	name := strings.ToLower(filepath.Base(lower))
	if strings.HasPrefix(name, ".env") {
		return true
	}
	for _, suffix := range []string{".pem", ".key", ".p12", ".pfx"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := pocketpath.HomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

func saveEnvelope(envelope RunEnvelope) error {
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}
	path, err := envelopePath(envelope.EnvelopeID)
	if err != nil {
		return err
	}
	return pocketpath.AtomicWrite(path, append(data, '\n'), 0o600)
}

func saveApproval(token ApprovalToken) error {
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	path, err := approvalPath(token.EnvelopeID)
	if err != nil {
		return err
	}
	return pocketpath.AtomicWrite(path, append(data, '\n'), 0o600)
}

func loadApproval(envelopeID string) (ApprovalToken, error) {
	path, err := approvalPath(envelopeID)
	if err != nil {
		return ApprovalToken{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ApprovalToken{}, err
	}
	var token ApprovalToken
	if err := json.Unmarshal(data, &token); err != nil {
		return ApprovalToken{}, err
	}
	return token, nil
}

func envelopePath(envelopeID string) (string, error) {
	if !safeID(envelopeID) {
		return "", ErrApprovalNotFound
	}
	dir, err := pocketpath.EnsureDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "envelopes", envelopeID+".json"), nil
}

func approvalPath(envelopeID string) (string, error) {
	if !safeID(envelopeID) {
		return "", ErrApprovalNotFound
	}
	dir, err := pocketpath.EnsureDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "approvals", envelopeID+".json"), nil
}

func safeID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." {
		return false
	}
	return !strings.ContainsAny(value, `/\`)
}

func newID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	dst := make([]byte, 36)
	hex.Encode(dst[0:8], raw[0:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], raw[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], raw[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], raw[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:36], raw[10:16])
	return string(dst)
}

func randomHex(bytesLen int) string {
	raw := make([]byte, bytesLen)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
}

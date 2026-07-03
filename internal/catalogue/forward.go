package catalogue

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type ForwardRequest struct {
	Host            string
	ServiceOrPorts  string
	User            string
	Bind            string
	Background      bool
	Name            string
	AllowPublicBind bool
}

type ForwardResult struct {
	Name       string `json:"name"`
	Host       string `json:"host"`
	SSHTarget  string `json:"ssh_target"`
	LocalURL   string `json:"local_url,omitempty"`
	Command    string `json:"command"`
	Background bool   `json:"background"`
	PID        int    `json:"pid"`
}

type ForwardSession struct {
	Name             string `json:"name"`
	SessionID        string `json:"session_id"`
	PID              int    `json:"pid"`
	PGID             int    `json:"pgid"`
	ProcessStartTime string `json:"process_start_time"`
	ExecutablePath   string `json:"executable_path"`
	CommandHash      string `json:"command_hash"`
	PocketSessionEnv string `json:"pocket_session_env"`
	Host             string `json:"host"`
	SSHTarget        string `json:"ssh_target"`
	Bind             string `json:"bind"`
	LocalPort        int    `json:"local_port"`
	RemoteHost       string `json:"remote_host"`
	RemotePort       int    `json:"remote_port"`
	StartedAtUTC     string `json:"started_at_utc"`
	Status           string `json:"status"`
}

type ForwardStore struct {
	HomeDir string
}

type portMapping struct {
	LocalPort  int
	RemoteHost string
	RemotePort int
	Scheme     string
}

func BuildForward(req ForwardRequest) (ForwardResult, error) {
	if strings.TrimSpace(req.Bind) == "" {
		req.Bind = "127.0.0.1"
	}
	if req.Bind != "127.0.0.1" {
		return ForwardResult{}, newError("ERR_PUBLIC_BIND_REQUIRES_EXPLICIT_APPLY", "bind publico fora do MVP", req.Bind, "use 127.0.0.1")
	}
	if !hostPattern.MatchString(req.Host) {
		return ForwardResult{}, newError("ERR_ARG_UNSAFE_CHARACTERS", "host invalido", req.Host, "use hostname seguro")
	}
	mapping, err := resolvePortMapping(req.ServiceOrPorts)
	if err != nil {
		return ForwardResult{}, err
	}
	if err := checkLocalPortAvailable(req.Bind, mapping.LocalPort); err != nil {
		return ForwardResult{}, err
	}
	target := req.Host
	if req.User != "" {
		if !userPattern.MatchString(req.User) {
			return ForwardResult{}, newError("ERR_INVALID_USER", "usuario SSH invalido", req.User, "use usuario POSIX simples")
		}
		target = req.User + "@" + req.Host
	}
	name := req.Name
	if name == "" {
		name = req.Host + "-" + strconv.Itoa(mapping.LocalPort)
		name = strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
				return r
			}
			if r >= 'A' && r <= 'Z' {
				return r + 32
			}
			return '-'
		}, name)
	}
	if !servicePattern.MatchString(name) {
		return ForwardResult{}, newError("ERR_INVALID_SESSION_NAME", "nome de sessao invalido", name, "use nome como pocket-web")
	}
	args := forwardSSHArgs(req.Bind, mapping.LocalPort, mapping.RemoteHost, mapping.RemotePort, target)
	command := "ssh " + strings.Join(args, " ")
	localURL := ""
	if mapping.Scheme != "" && mapping.Scheme != "none" {
		localURL = mapping.Scheme + "://localhost:" + strconv.Itoa(mapping.LocalPort)
	}
	return ForwardResult{
		Name:       name,
		Host:       req.Host,
		SSHTarget:  target,
		LocalURL:   localURL,
		Command:    command,
		Background: req.Background,
		PID:        -1,
	}, nil
}

func StartForward(req ForwardRequest, home string) (ForwardResult, error) {
	result, err := BuildForward(req)
	if err != nil {
		return result, err
	}
	mapping, err := resolvePortMapping(req.ServiceOrPorts)
	if err != nil {
		return result, err
	}
	target := result.SSHTarget
	args := forwardSSHArgs("127.0.0.1", mapping.LocalPort, mapping.RemoteHost, mapping.RemotePort, target)
	if !req.Background {
		cmd := exec.Command("ssh", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
		err := cmd.Run()
		result.PID = -1
		return result, err
	}

	sessionID, err := newUUID()
	if err != nil {
		return result, err
	}
	cmd := exec.Command("ssh", args...)
	cmd.Env = append(os.Environ(), "POCKETCLI_SESSION_ID="+sessionID)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return result, err
	}
	result.PID = cmd.Process.Pid
	session := ForwardSession{
		Name:             result.Name,
		SessionID:        sessionID,
		PID:              cmd.Process.Pid,
		PGID:             cmd.Process.Pid,
		ProcessStartTime: time.Now().UTC().Format(time.RFC3339Nano),
		ExecutablePath:   "ssh",
		CommandHash:      hashString(result.Command),
		PocketSessionEnv: "POCKETCLI_SESSION_ID=" + sessionID,
		Host:             result.Host,
		SSHTarget:        result.SSHTarget,
		Bind:             "127.0.0.1",
		LocalPort:        mapping.LocalPort,
		RemoteHost:       mapping.RemoteHost,
		RemotePort:       mapping.RemotePort,
		StartedAtUTC:     time.Now().UTC().Format(time.RFC3339),
		Status:           "running",
	}
	if err := (ForwardStore{HomeDir: home}).Save(session); err != nil {
		_ = cmd.Process.Kill()
		return result, err
	}
	return result, nil
}

func forwardSSHArgs(bind string, localPort int, remoteHost string, remotePort int, target string) []string {
	return []string{
		"-N",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=30",
		"-o", "ServerAliveCountMax=3",
		"-L", fmt.Sprintf("%s:%d:%s:%d", bind, localPort, remoteHost, remotePort),
		target,
	}
}

func resolvePortMapping(input string) (portMapping, error) {
	services := map[string]portMapping{
		"web":      {LocalPort: 3000, RemoteHost: "127.0.0.1", RemotePort: 3000, Scheme: "http"},
		"api":      {LocalPort: 8080, RemoteHost: "127.0.0.1", RemotePort: 8080, Scheme: "http"},
		"postgres": {LocalPort: 5432, RemoteHost: "127.0.0.1", RemotePort: 5432, Scheme: "postgres"},
		"redis":    {LocalPort: 6379, RemoteHost: "127.0.0.1", RemotePort: 6379, Scheme: "redis"},
	}
	if service, ok := services[input]; ok {
		return service, nil
	}
	if strings.Contains(input, ":") {
		localRaw, remoteRaw, ok := strings.Cut(input, ":")
		if !ok {
			return portMapping{}, newError("ERR_INVALID_PORT", "mapeamento de porta invalido", input, "use local:remote")
		}
		localPort, err := parsePort(localRaw)
		if err != nil {
			return portMapping{}, err
		}
		remotePort, err := parsePort(remoteRaw)
		if err != nil {
			return portMapping{}, err
		}
		return portMapping{LocalPort: localPort, RemoteHost: "127.0.0.1", RemotePort: remotePort, Scheme: "http"}, nil
	}
	port, err := parsePort(input)
	if err != nil {
		return portMapping{}, err
	}
	return portMapping{LocalPort: port, RemoteHost: "127.0.0.1", RemotePort: port, Scheme: "http"}, nil
}

func parsePort(value string) (int, error) {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return 0, newError("ERR_INVALID_PORT", "porta invalida", value, "use inteiro entre 1 e 65535")
	}
	return port, nil
}

func checkLocalPortAvailable(bind string, port int) error {
	ln, err := net.Listen("tcp", net.JoinHostPort(bind, strconv.Itoa(port)))
	if err != nil {
		return newError("ERR_PORT_ALREADY_IN_USE", "porta local ocupada", strconv.Itoa(port), "rode pocket catalogue run port.check "+strconv.Itoa(port))
	}
	return ln.Close()
}

func (s ForwardStore) stateDir() string {
	home := s.HomeDir
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	return filepath.Join(home, ".pocketcli", "state", "ssh-forwards")
}

func (s ForwardStore) Save(session ForwardSession) error {
	dir := s.stateDir()
	if err := ensurePrivateDir(filepath.Dir(dir)); err != nil {
		return err
	}
	if err := ensurePrivateDir(dir); err != nil {
		return err
	}
	path := filepath.Join(dir, session.Name+".json")
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+session.Name+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (s ForwardStore) List() ([]ForwardSession, error) {
	dir := s.stateDir()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return []ForwardSession{}, nil
	}
	if err := ensureSafeDir(dir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	sessions := make([]ForwardSession, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := ensureSafeFile(path); err != nil {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) > 16*1024 {
			continue
		}
		var session ForwardSession
		if err := json.Unmarshal(data, &session); err != nil {
			continue
		}
		if !processAlive(session.PID) {
			session.Status = "stale"
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

func (s ForwardStore) Stop(name string, force bool) (ForwardSession, error) {
	if !servicePattern.MatchString(name) {
		return ForwardSession{}, newError("ERR_INVALID_SESSION_NAME", "nome invalido", name, "use nome de sessao valido")
	}
	path := filepath.Join(s.stateDir(), name+".json")
	if err := ensureSafeFile(path); err != nil {
		return ForwardSession{}, newError("ERR_SESSION_NOT_FOUND", "sessao nao encontrada", name, "rode pocket ssh.forward list")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ForwardSession{}, err
	}
	var session ForwardSession
	if err := json.Unmarshal(data, &session); err != nil {
		return ForwardSession{}, err
	}
	if session.SessionID == "" || session.PocketSessionEnv != "POCKETCLI_SESSION_ID="+session.SessionID {
		return session, newError("ERR_SESSION_ID_MISMATCH", "session_id invalido no state", name, "nao vou matar processo sem state confiavel")
	}
	if !processAlive(session.PID) {
		session.Status = "stale"
		_ = os.Remove(path)
		return session, nil
	}
	signal := syscall.SIGTERM
	if force {
		signal = syscall.SIGKILL
	}
	if session.PGID > 0 {
		_ = syscall.Kill(-session.PGID, signal)
	} else {
		_ = syscall.Kill(session.PID, signal)
	}
	_ = os.Remove(path)
	session.Status = "stopped"
	return session, nil
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil
}

func newUUID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:]), nil
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

var userPattern = regexpMust(`^[a-z_][a-z0-9_-]{0,31}$`)

func regexpMust(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pattern)
}

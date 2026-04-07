package audit

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"pocketcli/internal/backend"
	"pocketcli/internal/router"
)

const (
	defaultMaxSizeBytes int64         = 10 * 1024 * 1024
	defaultMaxAge       time.Duration = 30 * 24 * time.Hour
	defaultRetention    int           = 3
)

type Record struct {
	Timestamp      time.Time
	Command        string
	SessionID      string
	RouterDecision router.Decision
	Response       backend.LLMResponse
	MemoryHit      bool
	LatencyMS      int
}

type Logger struct {
	baseDir      string
	now          func() time.Time
	newSessionID func() (string, error)
	maxSizeBytes int64
	maxAge       time.Duration
	retention    int
}

func NewLogger() (*Logger, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return NewLoggerAt(filepath.Join(home, ".pocket")), nil
}

func NewLoggerAt(baseDir string) *Logger {
	return &Logger{
		baseDir:      baseDir,
		now:          func() time.Time { return time.Now().UTC() },
		newSessionID: NewSessionID,
		maxSizeBytes: defaultMaxSizeBytes,
		maxAge:       defaultMaxAge,
		retention:    defaultRetention,
	}
}

func NewSessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}

	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		raw[0:4],
		raw[4:6],
		raw[6:8],
		raw[8:10],
		raw[10:16],
	), nil
}

func AuditWrite(command string, decision router.Decision, response backend.LLMResponse, memoryHit bool) error {
	logger, err := NewLogger()
	if err != nil {
		return err
	}
	return logger.Write(Record{
		Command:        command,
		RouterDecision: decision,
		Response:       response,
		MemoryHit:      memoryHit,
	})
}

func (l *Logger) Prepare() error {
	if err := os.MkdirAll(l.baseDir, 0o755); err != nil {
		return err
	}

	info, err := os.Stat(l.activePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	if !l.shouldRotate(info) {
		return nil
	}

	return l.rotate()
}

func (l *Logger) Write(record Record) error {
	if err := l.Prepare(); err != nil {
		return err
	}

	normalized, err := l.normalizeRecord(record)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(l.activePath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	if _, err := file.WriteString(l.formatLine(normalized)); err != nil {
		return err
	}

	return nil
}

func (l *Logger) normalizeRecord(record Record) (Record, error) {
	if record.Timestamp.IsZero() {
		record.Timestamp = l.now().UTC()
	} else {
		record.Timestamp = record.Timestamp.UTC()
	}

	record.Command = strings.TrimSpace(record.Command)
	if record.Command == "" {
		record.Command = "unknown"
	}

	record.SessionID = strings.TrimSpace(record.SessionID)
	if record.SessionID == "" {
		sessionID, err := l.newSessionID()
		if err != nil {
			return Record{}, err
		}
		record.SessionID = sessionID
	}

	if record.LatencyMS < 0 {
		record.LatencyMS = 0
	}
	if record.LatencyMS == 0 && record.Response.LatencyMS > 0 {
		record.LatencyMS = record.Response.LatencyMS
	}
	if record.Response.TokenUsage < 0 {
		record.Response.TokenUsage = 0
	}

	return record, nil
}

func (l *Logger) formatLine(record Record) string {
	return fmt.Sprintf(
		"%s | %s | %s | tokens=%d | latency=%dms | memory_hit=%t | session_id=%s\n",
		record.Timestamp.Format(time.RFC3339),
		record.Command,
		l.resolveBackend(record),
		record.Response.TokenUsage,
		record.LatencyMS,
		record.MemoryHit,
		record.SessionID,
	)
}

func (l *Logger) resolveBackend(record Record) string {
	if backendName := strings.ToLower(strings.TrimSpace(record.Response.Backend)); backendName != "" {
		return backendName
	}
	if backendName := strings.ToLower(strings.TrimSpace(record.RouterDecision.SelectedBackend)); backendName != "" {
		return backendName
	}
	return router.BackendNone
}

func (l *Logger) shouldRotate(info os.FileInfo) bool {
	if info.Size() >= l.maxSizeBytes {
		return true
	}
	return l.now().UTC().Sub(info.ModTime().UTC()) >= l.maxAge
}

func (l *Logger) rotate() error {
	target := l.rotatedPath(l.now().UTC())

	if _, err := os.Stat(target); err == nil {
		if err := os.Remove(target); err != nil {
			return err
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.Rename(l.activePath(), target); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	file, err := os.OpenFile(l.activePath(), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	return l.pruneHistorical()
}

func (l *Logger) pruneHistorical() error {
	entries, err := os.ReadDir(l.baseDir)
	if err != nil {
		return err
	}

	historical := make([]string, 0)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "audit.log.") {
			continue
		}
		historical = append(historical, name)
	}

	sort.Slice(historical, func(i, j int) bool {
		return historical[i] > historical[j]
	})

	for idx := l.retention; idx < len(historical); idx++ {
		if err := os.Remove(filepath.Join(l.baseDir, historical[idx])); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	return nil
}

func (l *Logger) activePath() string {
	return filepath.Join(l.baseDir, "audit.log")
}

func (l *Logger) rotatedPath(now time.Time) string {
	return filepath.Join(l.baseDir, "audit.log."+now.UTC().Format("2006-01-02"))
}

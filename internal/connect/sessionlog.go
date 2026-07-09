package connect

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type SessionLogger struct {
	HomeDir         func() (string, error)
	Now             func() time.Time
	Err             io.Writer
	MaxSizeBytes    int64
	SessionsLogPath string

	mu     sync.Mutex
	warned bool
}

func (l *SessionLogger) LogConnect(host, ip string, startedAt time.Time) {
	l.write(SessionRecord{
		Event:     "connect",
		Host:      host,
		IP:        ip,
		StartedAt: startedAt.UTC().Format(time.RFC3339),
	})
}

func (l *SessionLogger) LogDisconnect(host, ip string, endedAt time.Time, duration time.Duration, cause string) {
	seconds := int(duration.Seconds())
	if seconds < 0 {
		seconds = 0
	}

	l.write(SessionRecord{
		Event:     "disconnect",
		Host:      host,
		IP:        ip,
		EndedAt:   endedAt.UTC().Format(time.RFC3339),
		DurationS: seconds,
		ExitCause: cause,
	})
}

func (l *SessionLogger) write(record SessionRecord) {
	path, err := l.path()
	if err != nil {
		l.warnOnce(fmt.Sprintf("pocket: aviso: não foi possível preparar log de sessão: %v", err))
		return
	}

	logDir := filepath.Dir(path)
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		l.warnOnce(fmt.Sprintf("pocket: aviso: não foi possível preparar log de sessão: %v", err))
		return
	}
	if err := os.Chmod(filepath.Dir(logDir), 0o700); err != nil {
		l.warnOnce(fmt.Sprintf("pocket: aviso: não foi possível ajustar permissão da instalação: %v", err))
		return
	}
	if err := os.Chmod(logDir, 0o700); err != nil {
		l.warnOnce(fmt.Sprintf("pocket: aviso: não foi possível ajustar permissão do log de sessão: %v", err))
		return
	}

	if err := l.rotateIfNeeded(path); err != nil {
		l.warnOnce(fmt.Sprintf("pocket: aviso: não foi possível rotacionar log de sessão: %v", err))
	}

	data, err := json.Marshal(record)
	if err != nil {
		l.warnOnce(fmt.Sprintf("pocket: aviso: não foi possível serializar log de sessão: %v", err))
		return
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		l.warnOnce(fmt.Sprintf("pocket: aviso: não foi possível escrever log de sessão: %v", err))
		return
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		l.warnOnce(fmt.Sprintf("pocket: aviso: não foi possível ajustar permissão do log de sessão: %v", err))
		return
	}

	if _, err := file.Write(append(data, '\n')); err != nil {
		l.warnOnce(fmt.Sprintf("pocket: aviso: não foi possível escrever log de sessão: %v", err))
	}
}

func (l *SessionLogger) rotateIfNeeded(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	maxSize := l.MaxSizeBytes
	if maxSize <= 0 {
		maxSize = DefaultLogMaxSizeBytes
	}
	if info.Size() < maxSize {
		return nil
	}

	rotated := path + ".1"
	if err := os.Remove(rotated); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(path, rotated); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (l *SessionLogger) path() (string, error) {
	homeDirFunc := l.HomeDir
	if homeDirFunc == nil {
		homeDirFunc = os.UserHomeDir
	}

	homeDir, err := homeDirFunc()
	if err != nil {
		return "", err
	}

	relativePath := l.SessionsLogPath
	if relativePath == "" {
		relativePath = filepath.Join(".pocketcli", "logs", "sessions.log")
	}

	return filepath.Join(homeDir, relativePath), nil
}

func (l *SessionLogger) warnOnce(message string) {
	if l.Err == nil || strings.TrimSpace(message) == "" {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.warned {
		return
	}
	l.warned = true
	fmt.Fprintln(l.Err, message)
}

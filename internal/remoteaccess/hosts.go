package remoteaccess

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type HostResolver func(ctx context.Context, alias string) (RemoteHost, error)

type JSONHostStore struct {
	Path    string
	HomeDir func() (string, error)
}

type hostsDocument struct {
	Hosts []RemoteHost `json:"hosts"`
}

func DefaultHostStore() *JSONHostStore {
	return &JSONHostStore{HomeDir: os.UserHomeDir}
}

func (s *JSONHostStore) Resolve(_ context.Context, alias string) (RemoteHost, error) {
	if err := ValidateHostAlias(alias); err != nil {
		return RemoteHost{}, err
	}

	hosts, err := s.Load()
	if err != nil {
		return RemoteHost{}, err
	}
	for _, host := range hosts {
		if strings.EqualFold(host.Alias, alias) {
			return normalizeHost(alias, host), nil
		}
	}

	return normalizeHost(alias, RemoteHost{
		Alias:        alias,
		Hostname:     alias,
		OSFamily:     OSFamilyUnknown,
		AccessMethod: AccessMethodSSH,
		SSHPort:      22,
		Enabled:      true,
	}), nil
}

func (s *JSONHostStore) Load() ([]RemoteHost, error) {
	path, err := s.path()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	var hosts []RemoteHost
	if err := json.Unmarshal(data, &hosts); err == nil {
		return hosts, nil
	}

	var doc hostsDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc.Hosts, nil
}

func (s *JSONHostStore) path() (string, error) {
	if strings.TrimSpace(s.Path) != "" {
		return s.Path, nil
	}

	homeDirFunc := s.HomeDir
	if homeDirFunc == nil {
		homeDirFunc = os.UserHomeDir
	}
	home, err := homeDirFunc()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "pocketcli", "remote-hosts.json"), nil
}

func normalizeHost(alias string, host RemoteHost) RemoteHost {
	if strings.TrimSpace(host.Alias) == "" {
		host.Alias = alias
	}
	if strings.TrimSpace(host.Hostname) == "" {
		host.Hostname = host.Alias
	}
	if host.OSFamily == "" {
		host.OSFamily = OSFamilyUnknown
	}
	if host.AccessMethod == "" {
		host.AccessMethod = AccessMethodSSH
	}
	if host.SSHPort == 0 {
		host.SSHPort = 22
	}
	return host
}

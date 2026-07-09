package remoteaccess

import (
	"errors"
	"net/netip"
	"regexp"
	"strings"
)

var (
	ErrInvalidHostname = errors.New("invalid hostname")
	ErrUnsafeHostStore = errors.New("unsafe host store")

	aliasPattern       = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	defaultUserPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]{0,31}$`)
	sessionPattern     = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

func ValidateHostAlias(alias string) error {
	if !aliasPattern.MatchString(strings.TrimSpace(alias)) {
		return ErrInvalidHostname
	}
	return nil
}

func ValidateHostname(hostname string) error {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" || len(hostname) > 253 || strings.HasPrefix(hostname, "-") || strings.HasSuffix(hostname, ".") {
		return ErrInvalidHostname
	}
	if _, err := netip.ParseAddr(hostname); err == nil {
		return nil
	}
	for _, label := range strings.Split(hostname, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return ErrInvalidHostname
		}
		for _, char := range label {
			if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
				continue
			}
			return ErrInvalidHostname
		}
	}
	return nil
}

func ValidateTailscaleIP(value string) error {
	if _, err := netip.ParseAddr(strings.TrimSpace(value)); err != nil {
		return ErrInvalidHostname
	}
	return nil
}

func ValidateDefaultUser(value string) error {
	if value == "" {
		return nil
	}
	if !defaultUserPattern.MatchString(strings.TrimSpace(value)) {
		return ErrInvalidHostname
	}
	return nil
}

func ValidSessionID(sessionID string) bool {
	return sessionPattern.MatchString(strings.TrimSpace(sessionID))
}

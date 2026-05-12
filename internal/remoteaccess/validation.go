package remoteaccess

import (
	"errors"
	"regexp"
	"strings"
)

var (
	ErrInvalidHostname = errors.New("invalid hostname")

	aliasPattern   = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	sessionPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

func ValidateHostAlias(alias string) error {
	if !aliasPattern.MatchString(strings.TrimSpace(alias)) {
		return ErrInvalidHostname
	}
	return nil
}

func ValidateHostname(hostname string) error {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return ErrInvalidHostname
	}

	for _, blocked := range []string{";", "|", "&", "$", "(", ")", "`", `\`, "'", `"`, "<", ">", "{", "}", "[", "]", "!", "#", "~", "*", "?"} {
		if strings.Contains(hostname, blocked) {
			return ErrInvalidHostname
		}
	}
	return nil
}

func ValidSessionID(sessionID string) bool {
	return sessionPattern.MatchString(strings.TrimSpace(sessionID))
}

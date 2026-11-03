package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"

	"pocketcli/internal/pocketpath"
)

// investigationReportProfilePath holds the user's report preferences. It
// lives under profile/ so `pocket update` never overwrites it.
const investigationReportProfilePath = "profile/investigation-report.json"

type investigationReportProfile struct {
	Version string `json:"version"`
	// ActorNames is "pseudonym" (default) or "real".
	ActorNames string `json:"actor_names"`
}

// investigationRealNames reports whether the user's profile asks the PDF to
// show actors by the names the sources reported. A missing, unreadable or
// invalid profile keeps the pseudonyms: names are never shown by accident.
func investigationRealNames() bool {
	configDir, err := pocketpath.ConfigDir()
	if err != nil {
		return false
	}
	raw, err := os.ReadFile(filepath.Join(configDir, investigationReportProfilePath))
	if err != nil || len(raw) > 64*1024 {
		return false
	}
	var profile investigationReportProfile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&profile) != nil || profile.Version != "1" {
		return false
	}
	return profile.ActorNames == "real"
}

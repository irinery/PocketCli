package main

import (
	"encoding/json"
	"io"
)

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name {
			return true
		}
	}
	return false
}

func flagValue(args []string, name string) (string, bool, error) {
	for idx, arg := range args {
		if arg == name {
			if idx+1 >= len(args) {
				return "", false, errFlagRequiresValue(name)
			}
			return args[idx+1], true, nil
		}
		if len(arg) > len(name)+1 && arg[:len(name)+1] == name+"=" {
			return arg[len(name)+1:], true, nil
		}
	}
	return "", false, nil
}

func errFlagRequiresValue(name string) error {
	return flagError{name + " requer valor"}
}

type flagError struct {
	message string
}

func (e flagError) Error() string { return e.message }

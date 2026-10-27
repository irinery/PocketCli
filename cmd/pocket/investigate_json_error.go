package main

import (
	"errors"
	"io"
	"strings"

	"pocketcli/internal/investigation/contract"
)

func investigationJSONRequested(args []string) bool {
	if len(args) == 0 || args[0] != "investigate" {
		return false
	}
	for _, arg := range args[1:] {
		if arg == "--json" {
			return true
		}
	}
	return false
}

func writeInvestigationJSONError(out io.Writer, err error) error {
	response := contract.ErrorResponse{Code: "ERR_UNAVAILABLE", Message: "falha ao executar investigate", Fields: []contract.FieldError{}}
	var typed *contract.ErrorResponse
	if errors.As(err, &typed) {
		response = *typed
		response.OK = false
		if response.Fields == nil {
			response.Fields = []contract.FieldError{}
		}
	} else if message := err.Error(); strings.HasPrefix(message, "ERR_") {
		if separator := strings.Index(message, ":"); separator > 4 && validInvestigationErrorCode(message[:separator]) {
			response.Code = message[:separator]
			response.Message = "falha ao executar investigate"
		}
	} else if strings.Contains(err.Error(), "flag") || strings.Contains(err.Error(), "--") {
		response.Code = "ERR_VALIDATION"
		response.Message = "parâmetros inválidos"
	}
	return writeJSON(out, response)
}

func validInvestigationErrorCode(code string) bool {
	if !strings.HasPrefix(code, "ERR_") || len(code) > 64 {
		return false
	}
	for _, ch := range code[4:] {
		if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
			continue
		}
		return false
	}
	return true
}

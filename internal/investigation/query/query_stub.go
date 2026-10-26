//go:build !investigate_sqlite || !cgo

package query

import (
	"context"
	"pocketcli/internal/investigation/contract"
)

type unavailable struct{}

func New(root string) (Service, error) { return unavailable{}, nil }
func (unavailable) Available() bool    { return false }
func (unavailable) Execute(context.Context, string, contract.QueryRequest) (contract.QueryResult, error) {
	return contract.QueryResult{}, &contract.ErrorResponse{Code: "ERR_SQL_DENIED", Message: "motor SQL indisponível; compile Agent com -tags investigate_sqlite e CGO_ENABLED=1"}
}
func (unavailable) Publish(string, Dataset) error {
	return &contract.ErrorResponse{Code: "ERR_SQL_DENIED", Message: "motor SQL indisponível; compile Agent com -tags investigate_sqlite e CGO_ENABLED=1"}
}
func (unavailable) VerifyPublished(string, string) error {
	return &contract.ErrorResponse{Code: "ERR_SQL_DENIED", Message: "motor SQL indisponível; compile Agent com -tags investigate_sqlite e CGO_ENABLED=1"}
}

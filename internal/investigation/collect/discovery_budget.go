package collect

import (
	"context"
	"sync"

	"pocketcli/internal/investigation/contract"
)

const DefaultDiscoveryBudget = 20

type discoveryBudgetKey struct{}

type discoveryBudget struct {
	mu    sync.Mutex
	limit int
	used  map[string]int
}

// WithDiscoveryBudget authorizes at most limit external HTTP attempts per
// source during one PreparePlan call. Retries and 304 responses count too.
func WithDiscoveryBudget(ctx context.Context, limit int) context.Context {
	return context.WithValue(ctx, discoveryBudgetKey{}, &discoveryBudget{limit: limit, used: make(map[string]int)})
}

func DiscoveryBudget(ctx context.Context) int {
	if budget, ok := ctx.Value(discoveryBudgetKey{}).(*discoveryBudget); ok {
		return budget.limit
	}
	return 0
}

func claimDiscoveryRequest(ctx context.Context, source string) error {
	budget, ok := ctx.Value(discoveryBudgetKey{}).(*discoveryBudget)
	if !ok {
		return nil
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	if budget.used[source] >= budget.limit {
		return &contract.ErrorResponse{Code: "ERR_RESOURCE_EXHAUSTED", Message: "orçamento de descoberta esgotado; aprove mais chamadas antes de continuar"}
	}
	budget.used[source]++
	return nil
}

package core

import (
	"context"
	"os"

	"pocketcli/internal/investigation/collect"
	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/storage"
)

type planAuth struct {
	GitHub           string `json:"github,omitempty"`
	Linear           string `json:"linear,omitempty"`
	GitHubAPIVersion string `json:"github_api_version,omitempty"`
}

func (c *Core) currentAuth(ctx context.Context, input contract.PlanInput) (planAuth, error) {
	var out planAuth
	if (input.GitHubEnabled || input.LinearEnabled) && c.collector.Secrets == nil {
		return out, fault("ERR_CREDENTIAL_REQUIRED", "credencial ausente")
	}
	for _, source := range []struct {
		name    string
		enabled bool
		target  *string
	}{{"github", input.GitHubEnabled, &out.GitHub}, {"linear", input.LinearEnabled, &out.Linear}} {
		if !source.enabled {
			continue
		}
		if err := c.collector.Secrets.WithSecret(ctx, source.name, func(secret []byte) error {
			seed := append([]byte(source.name+":"), secret...)
			*source.target = storage.Hash(seed)
			clear(seed)
			return nil
		}); err != nil {
			return planAuth{}, err
		}
	}
	if input.GitHubEnabled {
		out.GitHubAPIVersion = collect.GitHubAPIVersion()
	}
	return out, nil
}

func (c *Core) checkPlanAuth(ctx context.Context, plan contract.Plan) error {
	var saved planAuth
	if err := c.store.Get("planauth", plan.ID, &saved); err != nil {
		return fault("ERR_AUDIT_FAILED", "identidade do plano indisponível")
	}
	current, err := c.currentAuth(ctx, plan.Input)
	if err != nil {
		return err
	}
	if current != saved {
		return fault("ERR_PLAN_CHANGED", "credencial ou versão da fonte diferente da aprovada no plano")
	}
	return nil
}

func (c *Core) canReuse(previousPlanID, currentPlanID string) (bool, error) {
	var current, previous planAuth
	if err := c.store.Get("planauth", currentPlanID, &current); err != nil {
		return false, fault("ERR_AUDIT_FAILED", "identidade do plano indisponível")
	}
	if err := c.store.Get("planauth", previousPlanID, &previous); err != nil {
		if os.IsNotExist(err) { // Snapshot criado antes deste controle: recarregar.
			return false, nil
		}
		return false, fault("ERR_AUDIT_FAILED", "identidade anterior corrompida")
	}
	return previous == current, nil
}

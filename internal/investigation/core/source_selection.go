package core

import "pocketcli/internal/investigation/contract"

func validateSourceSelection(input contract.PlanInput) error {
	if input.Scope != nil {
		if !input.GitHubEnabled && len(input.Scope.Repositories) > 0 {
			return fault("ERR_VALIDATION", "repositórios selecionados exigem a fonte GitHub")
		}
		if !input.LinearEnabled && len(input.Scope.Teams) > 0 {
			return fault("ERR_VALIDATION", "teams selecionados exigem a fonte Linear")
		}
	}
	if !input.GitHubEnabled && (input.Modules.GitHubIssues || input.Modules.CodeHistory || input.Modules.CI || input.Modules.Deployments) {
		return fault("ERR_VALIDATION", "módulos GitHub exigem a fonte GitHub")
	}
	return nil
}

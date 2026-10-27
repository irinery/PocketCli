package main

import (
	"testing"

	"pocketcli/internal/investigation/collect"
)

func TestDiscoveryBudgetFlagAuthorizesAdditionalCalls(t *testing.T) {
	flags, err := parseInvestigationFlags([]string{"--discovery-budget", "21", "--prepare"})
	if err != nil || collect.DiscoveryBudget(discoveryContext(flags)) != 21 {
		t.Fatalf("approved budget: flags=%+v err=%v", flags, err)
	}
	for _, value := range []string{"0", "-1", "invalid"} {
		if _, err := parseInvestigationFlags([]string{"--discovery-budget", value}); err == nil {
			t.Errorf("accepted invalid budget %q", value)
		}
	}
}

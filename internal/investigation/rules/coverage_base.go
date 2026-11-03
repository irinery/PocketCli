package rules

import (
	"fmt"
	"strings"

	"pocketcli/internal/investigation/contract"
	"pocketcli/internal/investigation/query"
)

// linearHistoryBase counts the Linear issues of a team (every team when
// team is empty) whose history was read completely enough to project
// workflow stages, out of all the team's issues in the snapshot. A round
// capped at a fixed number of issues leaves the rest out, and the metrics
// built from history speak only for that share.
func linearHistoryBase(d query.Dataset, team string) *contract.MetricBase {
	withHistory := make(map[string]bool)
	for _, stage := range d.Stages {
		if strings.HasPrefix(stage.Stage, linearStagePrefix) {
			withHistory[stage.SubjectID] = true
		}
	}
	var observed, total int64
	for _, issue := range d.Issues {
		if issue.Source != "linear" || (team != "" && issue.TeamID != team) {
			continue
		}
		total++
		if withHistory[issue.ID] {
			observed++
		}
	}
	if total == 0 {
		return nil
	}
	return contract.NewMetricBase(observed, total)
}

// withBase attaches the base to a metric and states it in the limitations,
// so every surface that lists limitations also shows it.
func withBase(m *contract.Metric, base *contract.MetricBase, scope string) {
	if base == nil {
		return
	}
	m.Base = base
	m.Limitations = append(m.Limitations, fmt.Sprintf("Base: %d de %d %s com histórico lido (%d%%).", base.Observed, base.Total, scope, base.Percent()))
	if base.Low {
		m.Limitations = append(m.Limitations, "Baixa cobertura: menos da metade das issues tem histórico; o valor pode não representar o team.")
	}
}

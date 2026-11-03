package contract

import "sort"

// ReferencedRepositoryCollection is the coverage row of a GitHub repository
// that Linear attachments cite in pull request links but the run did not
// collect: either it was left out of the scope or the credential cannot see
// it. The resource ID is the repository's owner/name.
const ReferencedRepositoryCollection = "referenced_repository"

// PullReferencesCollection is the analysis coverage row that counts the pull
// request links found in Linear attachments: Observed is how many point to a
// collected repository, Expected how many there are in total.
const PullReferencesCollection = "pull_references"

// InsufficientReferenceShare is the share of pull request references outside
// the collection above which the Git side cannot speak for the work tracked
// in Linear.
const InsufficientReferenceShare = 0.2

// UncollectedRepository is a repository cited by Linear but not collected.
type UncollectedRepository struct {
	Name       string
	References int64
	Status     string
}

// UncollectedRepositories lists the cited repositories that were not
// collected, most cited first, so every surface names them the same way.
func UncollectedRepositories(items []Coverage) []UncollectedRepository {
	var out []UncollectedRepository
	for _, c := range items {
		if c.Source == "github" && c.Collection == ReferencedRepositoryCollection {
			out = append(out, UncollectedRepository{Name: c.ResourceID, References: c.Observed, Status: c.Status})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].References != out[j].References {
			return out[i].References > out[j].References
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// PullReferenceGap returns how many pull request references from Linear
// point outside the collected repositories, out of how many in total, and
// whether that share makes the Git coverage insufficient.
func PullReferenceGap(items []Coverage) (outside, total int64, insufficient bool) {
	for _, c := range items {
		if c.Source != "analysis" || c.Collection != PullReferencesCollection || c.Expected == nil {
			continue
		}
		total = *c.Expected
		outside = total - c.Observed
	}
	if total == 0 || outside < 0 {
		return 0, total, false
	}
	return outside, total, float64(outside)/float64(total) > InsufficientReferenceShare
}

// ForkReason is the coverage reason of a fork whose Git history was not
// collected: its commits belong to the upstream, not to the organization.
func ForkReason(r Resource) string {
	if r.Parent != "" {
		return "fork de " + r.Parent + "; histórico Git do upstream não é atribuído à organização"
	}
	return "fork; histórico Git do upstream não é atribuído à organização"
}

// Forks names the GitHub resources that are forks, with their upstream when
// known, so the report says why their code was left out.
func Forks(resources []Resource) []string {
	var out []string
	for _, r := range resources {
		if r.Source != "github" || !r.Fork {
			continue
		}
		name := r.Name
		if r.Parent != "" {
			name += " (fork de " + r.Parent + ")"
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

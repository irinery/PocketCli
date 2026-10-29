package contract

import "testing"

func TestEffectiveWindowUsesLifetimesOnlyForAllPeriod(t *testing.T) {
	end := "2026-10-29T12:00:00Z"
	repo := Resource{Source: "github", Name: "a/b", CreatedAt: "2021-03-01T10:00:00Z", LastChangeAt: "2026-09-30T08:30:00Z"}
	late := Resource{Source: "github", Name: "a/c", CreatedAt: "2023-01-01T00:00:00Z", LastChangeAt: "2024-01-01T00:00:00Z"}
	team := Resource{Source: "linear", Name: "ENG", CreatedAt: "2019-05-02T00:00:00Z"}
	for _, test := range []struct {
		name      string
		start     string
		resources []Resource
		from, to  string
		lifetime  bool
	}{
		{"bounded window is untouched", "2026-10-01T00:00:00Z", []Resource{repo}, "2026-10-01T00:00:00Z", end, false},
		{"repository lives until its last change", "", []Resource{repo, late}, "2021-03-01T10:00:00Z", "2026-09-30T08:30:00Z", true},
		{"linear reaches the window end", "", []Resource{repo, team}, "2019-05-02T00:00:00Z", end, true},
		{"unknown creation says nothing", "", []Resource{{Source: "github", Name: "x/y"}}, "", end, false},
	} {
		from, to, lifetime := EffectiveWindow(test.start, end, test.resources)
		if from != test.from || to != test.to || lifetime != test.lifetime {
			t.Errorf("%s: %q → %q (%v), esperado %q → %q (%v)", test.name, from, to, lifetime, test.from, test.to, test.lifetime)
		}
	}
	if NormalizeInstant("2022-07-10T12:00:00.000-03:00") != "2022-07-10T15:00:00Z" || NormalizeInstant("ontem") != "" {
		t.Fatal("NormalizeInstant")
	}
	if !IsEmptyRepository(Coverage{Source: "git", Reason: EmptyRepositoryReason + "; x"}) || IsEmptyRepository(Coverage{Source: "github", Reason: EmptyRepositoryReason}) {
		t.Fatal("IsEmptyRepository")
	}
}

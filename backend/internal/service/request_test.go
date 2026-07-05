package service

import (
	"testing"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/qualityprofile"
	"github.com/kyleaupton/arrflix/internal/scope"
)

func TestNormalizeTier(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		wantVal qualityprofile.Tier
		wantOK  bool
	}{
		{"", qualityprofile.TierHD, true}, // omitted → default HD
		{"HD", qualityprofile.TierHD, true},
		{"4K", qualityprofile.Tier4K, true},
		{"hd", "", false}, // case-sensitive; tiers are uppercase
		{"8K", "", false},
	}
	for _, c := range cases {
		got, ok := normalizeTier(c.in)
		if got != c.wantVal || ok != c.wantOK {
			t.Errorf("normalizeTier(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.wantVal, c.wantOK)
		}
	}
}

func TestNormalizeAutonomy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		wantVal string
		wantOK  bool
	}{
		{"", string(model.AutonomyAuto), true}, // omitted → born default
		{"auto", "auto", true},
		{"propose", "propose", true},
		{"manual", "manual", true},
		{"Auto", "", false}, // case-sensitive; enum is lowercase
		{"grab", "", false},
	}
	for _, c := range cases {
		got, ok := normalizeAutonomy(c.in)
		if got != c.wantVal || ok != c.wantOK {
			t.Errorf("normalizeAutonomy(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.wantVal, c.wantOK)
		}
	}
}

func TestNormalizeSeriesScope(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		wantVal string
		wantOK  bool
	}{
		{"", string(scope.RuleAll), true}, // omitted → default 'all'
		{"all", "all", true},
		{"future_only", "future_only", true},
		{"season", "", false}, // needs a param the API doesn't carry yet
		{"pilot", "", false},
		{"bogus", "", false},
	}
	for _, c := range cases {
		got, ok := normalizeSeriesScope(c.in)
		if got != c.wantVal || ok != c.wantOK {
			t.Errorf("normalizeSeriesScope(%q) = (%q, %v), want (%q, %v)", c.in, got, ok, c.wantVal, c.wantOK)
		}
	}
}

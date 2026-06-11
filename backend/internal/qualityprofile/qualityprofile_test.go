package qualityprofile

import (
	"testing"

	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/rules"
)

// --- helpers -----------------------------------------------------------------

func fld(path string) *rules.Operand {
	return &rules.Operand{Kind: rules.OperandField, Path: path}
}

func litEnum(s string) *rules.Operand {
	return &rules.Operand{Kind: rules.OperandLiteral, Literal: &rules.Literal{Type: rules.LitEnum, Str: s}}
}

func leaf(op rules.Operator, l, r *rules.Operand) *rules.Condition {
	return &rules.Condition{Op: op, Left: l, Right: r}
}

// subj builds a movie/series-agnostic subject from a parsed quality core plus the
// indexer facts the structural gate and tiebreak read.
func subj(source, res, mod string, seeders int, size int64, guid string) model.Subject {
	return model.Subject{
		Release: model.Release{
			Quality: model.QualityFields{
				Source:     parsing.Field[string]{Value: source},
				Resolution: parsing.Field[string]{Value: res},
				Modifier:   parsing.Field[string]{Value: mod},
			},
			Candidate: model.CandidateFields{Seeders: seeders, Size: size, GUID: guid},
		},
	}
}

func withRepack(s model.Subject) model.Subject {
	s.Release.Quality.IsRepack = parsing.Field[bool]{Value: true}
	return s
}

func withCodec(s model.Subject, codec string) model.Subject {
	s.Release.MediaInfo = &model.MediaInfoFields{VideoCodec: codec}
	return s
}

func withRuntime(s model.Subject, minutes int) model.Subject {
	s.Media.Runtime = &minutes
	return s
}

// testProfile is a small movie profile: three ranked bins, cutoff at WEBDL-1080p,
// min 5 seeders, the seeded repack scorer.
func testProfile() Profile {
	return Profile{
		Domain: parsing.DomainMovie,
		Bins: []parsing.BinKey{
			bin(parsing.SourceBluRay, parsing.Res1080p, parsing.ModNone), // 0 best
			bin(parsing.SourceWEBDL, parsing.Res1080p, parsing.ModNone),  // 1
			bin(parsing.SourceBluRay, parsing.Res720p, parsing.ModNone),  // 2
		},
		Cutoff:     bin(parsing.SourceWEBDL, parsing.Res1080p, parsing.ModNone),
		MinSeeders: 5,
		Formats:    []ScoredFormat{repackProper()},
	}
}

// --- binOf -------------------------------------------------------------------

func TestBinOf_ModifierNoneEquivalence(t *testing.T) {
	none := binOf(subj("BluRay", "1080p", "", 0, 0, ""), parsing.DomainMovie)
	nameNone := binOf(subj("BluRay", "1080p", "NONE", 0, 0, ""), parsing.DomainMovie)
	want := bin(parsing.SourceBluRay, parsing.Res1080p, parsing.ModNone)
	if none != want || nameNone != want {
		t.Errorf(`"" => %+v, "NONE" => %+v, want both %+v`, none, nameNone, want)
	}
}

// --- Gate --------------------------------------------------------------------

func TestGate(t *testing.T) {
	reg := rules.NewRegistry()
	p := testProfile()

	t.Run("allowed bin with enough seeders passes", func(t *testing.T) {
		ok, reason := p.Gate(reg, subj("BluRay", "1080p", "", 10, 0, ""), model.PhaseSearch)
		if !ok {
			t.Errorf("passed=false reason=%+v, want pass", reason)
		}
	})

	t.Run("bin not in allowed list rejects", func(t *testing.T) {
		ok, reason := p.Gate(reg, subj("TV", "2160p", "", 10, 0, ""), model.PhaseSearch)
		if ok || reason.Gate != "bin" {
			t.Errorf("ok=%v reason=%+v, want reject on bin", ok, reason)
		}
	})

	t.Run("too few seeders rejects", func(t *testing.T) {
		ok, reason := p.Gate(reg, subj("BluRay", "1080p", "", 2, 0, ""), model.PhaseSearch)
		if ok || reason.Gate != "min_seeders" {
			t.Errorf("ok=%v reason=%+v, want reject on min_seeders", ok, reason)
		}
	})

	t.Run("custom gate matching True rejects", func(t *testing.T) {
		gp := testProfile()
		gp.Gates = []Gate{{Name: "no-bluray", Tree: leaf(rules.OpEq, fld("quality.source"), litEnum("BluRay"))}}
		ok, reason := gp.Gate(reg, subj("BluRay", "1080p", "", 10, 0, ""), model.PhaseSearch)
		if ok || reason.Gate != "no-bluray" {
			t.Errorf("ok=%v reason=%+v, want reject on no-bluray", ok, reason)
		}
	})

	t.Run("import-field gate is inert at search (Unknown never rejects)", func(t *testing.T) {
		gp := testProfile()
		gp.Gates = []Gate{{Name: "h264", Tree: leaf(rules.OpEq, fld("mediainfo.video_codec"), litEnum("H.264"))}}
		ok, _ := gp.Gate(reg, subj("BluRay", "1080p", "", 10, 0, ""), model.PhaseSearch)
		if !ok {
			t.Error("import-field gate rejected at search; Unknown must not reject")
		}
	})
}

// --- Score -------------------------------------------------------------------

func TestScore(t *testing.T) {
	reg := rules.NewRegistry()
	p := testProfile()

	score, traces := p.Score(reg, withRepack(subj("BluRay", "1080p", "", 10, 0, "")), model.PhaseSearch)
	if score != 5 {
		t.Errorf("repack score = %d, want 5", score)
	}
	if len(traces) != len(p.Formats) {
		t.Errorf("traces = %d, want %d", len(traces), len(p.Formats))
	}

	if score, _ := p.Score(reg, subj("BluRay", "1080p", "", 10, 0, ""), model.PhaseSearch); score != 0 {
		t.Errorf("non-repack score = %d, want 0", score)
	}
}

// --- Pick --------------------------------------------------------------------

func TestPick(t *testing.T) {
	reg := rules.NewRegistry()
	p := testProfile()

	t.Run("bin rank beats a higher score in a lower bin", func(t *testing.T) {
		webdlRepack := withRepack(subj("WEB-DL", "1080p", "", 10, 0, "webdl")) // score 5, bin rank 1
		bluray := subj("BluRay", "1080p", "", 10, 0, "bluray")                 // score 0, bin rank 0
		sel := p.Pick(reg, []model.Subject{webdlRepack, bluray})
		if sel.Picked == nil || sel.Picked.Subject.Release.Candidate.GUID != "bluray" {
			t.Errorf("picked = %+v, want the Bluray release", sel.Picked)
		}
	})

	t.Run("score breaks a tie within the same bin", func(t *testing.T) {
		plain := subj("BluRay", "1080p", "", 10, 0, "plain")
		repack := withRepack(subj("BluRay", "1080p", "", 10, 0, "repack"))
		sel := p.Pick(reg, []model.Subject{plain, repack})
		if sel.Picked == nil || sel.Picked.Subject.Release.Candidate.GUID != "repack" {
			t.Errorf("picked = %+v, want the repack", sel.Picked)
		}
	})

	t.Run("size then guid is the deterministic final tiebreak", func(t *testing.T) {
		big := subj("BluRay", "1080p", "", 10, 2000, "big")
		small := subj("BluRay", "1080p", "", 10, 1000, "small")
		sel := p.Pick(reg, []model.Subject{big, small})
		if sel.Picked == nil || sel.Picked.Subject.Release.Candidate.GUID != "small" {
			t.Errorf("picked = %+v, want the smaller release", sel.Picked)
		}
	})

	t.Run("all gated out yields no pick", func(t *testing.T) {
		sel := p.Pick(reg, []model.Subject{subj("TV", "2160p", "", 10, 0, "a")})
		if sel.Picked != nil {
			t.Errorf("picked = %+v, want nil", sel.Picked)
		}
		if sel.All[0].Disposition != DispositionRejected {
			t.Errorf("disposition = %q, want rejected", sel.All[0].Disposition)
		}
	})

	t.Run("dispositions: grabbed, runner_up, rejected", func(t *testing.T) {
		win := subj("BluRay", "1080p", "", 10, 0, "win")
		runner := subj("WEB-DL", "1080p", "", 10, 0, "runner")
		gated := subj("TV", "2160p", "", 10, 0, "gated")
		sel := p.Pick(reg, []model.Subject{win, runner, gated})
		got := map[string]Disposition{}
		for _, e := range sel.All {
			got[e.Subject.Release.Candidate.GUID] = e.Disposition
		}
		if got["win"] != DispositionGrabbed || got["runner"] != DispositionRunnerUp || got["gated"] != DispositionRejected {
			t.Errorf("dispositions = %+v", got)
		}
	})
}

// --- Size enforcement --------------------------------------------------------

// sizeProfile is the testProfile with a size band on the best bin (Bluray-1080p):
// per-minute min 10, preferred 100, max 200. At runtime 10min the scaled band is
// [100, 2000] bytes with a 1000-byte preferred.
func sizeProfile() Profile {
	p := testProfile()
	p.Sizes = map[parsing.BinKey]SizeBand{
		bin(parsing.SourceBluRay, parsing.Res1080p, parsing.ModNone): {
			MinBytesPerMin: 10, PreferredBytesPerMin: 100, MaxBytesPerMin: 200,
		},
	}
	return p
}

func TestGateSize(t *testing.T) {
	reg := rules.NewRegistry()
	p := sizeProfile()

	t.Run("over scaled max rejects", func(t *testing.T) {
		s := withRuntime(subj("BluRay", "1080p", "", 10, 2500, ""), 10) // scaled max 2000
		ok, reason := p.Gate(reg, s, model.PhaseSearch)
		if ok || reason.Gate != "size_max" {
			t.Errorf("ok=%v reason=%+v, want reject on size_max", ok, reason)
		}
	})

	t.Run("under scaled min rejects", func(t *testing.T) {
		s := withRuntime(subj("BluRay", "1080p", "", 10, 50, ""), 10) // scaled min 100
		ok, reason := p.Gate(reg, s, model.PhaseSearch)
		if ok || reason.Gate != "size_min" {
			t.Errorf("ok=%v reason=%+v, want reject on size_min", ok, reason)
		}
	})

	t.Run("within band passes", func(t *testing.T) {
		s := withRuntime(subj("BluRay", "1080p", "", 10, 1000, ""), 10)
		if ok, reason := p.Gate(reg, s, model.PhaseSearch); !ok {
			t.Errorf("passed=false reason=%+v, want pass", reason)
		}
	})

	t.Run("absent runtime skips the size gate", func(t *testing.T) {
		// Way over the scaled max, but no runtime to scale against — no-op.
		s := subj("BluRay", "1080p", "", 10, 9_000_000, "")
		if ok, reason := p.Gate(reg, s, model.PhaseSearch); !ok {
			t.Errorf("size gate fired without a runtime: reason=%+v", reason)
		}
	})
}

func TestPickPreferredSize(t *testing.T) {
	reg := rules.NewRegistry()
	p := sizeProfile() // scaled preferred 1000 at 10min

	// Both pass the band [100, 2000]. closer is nearer the 1000-byte preferred
	// (dist 100) but is the larger file; farther is smaller (dist 200). The
	// preferred-closest tiebreak must beat the plain smaller-size tiebreak.
	closer := withRuntime(subj("BluRay", "1080p", "", 10, 900, "closer"), 10)
	farther := withRuntime(subj("BluRay", "1080p", "", 10, 800, "farther"), 10)
	sel := p.Pick(reg, []model.Subject{farther, closer})
	if sel.Picked == nil || sel.Picked.Subject.Release.Candidate.GUID != "closer" {
		t.Errorf("picked = %+v, want the size closest to preferred", sel.Picked)
	}
}

// --- ReGate ------------------------------------------------------------------

func TestReGate(t *testing.T) {
	reg := rules.NewRegistry()

	t.Run("import gate matching hard-fails", func(t *testing.T) {
		p := testProfile()
		p.Gates = []Gate{{Name: "h264", Tree: leaf(rules.OpEq, fld("mediainfo.video_codec"), litEnum("H.264"))}}
		out := p.ReGate(reg, withCodec(subj("BluRay", "1080p", "", 10, 0, ""), "H.264"))
		if out.Result != ReGateHardFail || out.Reason.Gate != "h264" {
			t.Errorf("out = %+v, want hard_fail on h264", out)
		}
	})

	t.Run("import gate not matching passes", func(t *testing.T) {
		p := testProfile()
		p.Gates = []Gate{{Name: "h264", Tree: leaf(rules.OpEq, fld("mediainfo.video_codec"), litEnum("H.264"))}}
		out := p.ReGate(reg, withCodec(subj("BluRay", "1080p", "", 10, 0, ""), "H.265"))
		if out.Result != ReGatePass {
			t.Errorf("out = %+v, want pass", out)
		}
	})
}

// --- StrictlyBetter ----------------------------------------------------------

func TestStrictlyBetter(t *testing.T) {
	reg := rules.NewRegistry()
	p := testProfile()
	bluray1080 := bin(parsing.SourceBluRay, parsing.Res1080p, parsing.ModNone) // rank 0
	webdl1080 := bin(parsing.SourceWEBDL, parsing.Res1080p, parsing.ModNone)   // rank 1

	t.Run("strictly better bin upgrades", func(t *testing.T) {
		if !p.StrictlyBetter(reg, FileQuality{Bin: webdl1080}, subj("BluRay", "1080p", "", 10, 0, ""), 0) {
			t.Error("better bin should upgrade")
		}
	})

	t.Run("worse bin never upgrades", func(t *testing.T) {
		if p.StrictlyBetter(reg, FileQuality{Bin: bluray1080}, subj("WEB-DL", "1080p", "", 10, 0, ""), 0) {
			t.Error("worse bin should not upgrade")
		}
	})

	t.Run("same bin upgrades only past anti-flap delta", func(t *testing.T) {
		cur := FileQuality{Bin: bluray1080, Score: 0}
		cand := withRepack(subj("BluRay", "1080p", "", 10, 0, "")) // score 5
		if !p.StrictlyBetter(reg, cur, cand, 3) {
			t.Error("score 5 over delta 3 should upgrade")
		}
		if p.StrictlyBetter(reg, cur, cand, 5) {
			t.Error("score 5 not strictly over delta 5 should not upgrade")
		}
	})
}

// --- BelowCutoff -------------------------------------------------------------

func TestBelowCutoff(t *testing.T) {
	p := testProfile() // cutoff = WEBDL-1080p (rank 1)
	cases := []struct {
		name string
		b    parsing.BinKey
		want bool
	}{
		{"above cutoff", bin(parsing.SourceBluRay, parsing.Res1080p, parsing.ModNone), false},
		{"at cutoff", bin(parsing.SourceWEBDL, parsing.Res1080p, parsing.ModNone), false},
		{"below cutoff", bin(parsing.SourceBluRay, parsing.Res720p, parsing.ModNone), true},
		{"unlisted bin", bin(parsing.SourceTV, parsing.Res2160p, parsing.ModNone), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := p.BelowCutoff(tc.b); got != tc.want {
				t.Errorf("BelowCutoff = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- ResolveTier -------------------------------------------------------------

func TestResolveTier(t *testing.T) {
	profiles := DefaultProfiles()
	bindings := []TierBinding{
		{Tier: TierHD, Domain: parsing.DomainMovie, ProfileID: profiles[0].ID},
		{Tier: Tier4K, Domain: parsing.DomainMovie, ProfileID: profiles[1].ID},
	}
	if id, ok := ResolveTier(TierHD, parsing.DomainMovie, bindings); !ok || id != profiles[0].ID {
		t.Errorf("HD/movie => %v,%v, want %v,true", id, ok, profiles[0].ID)
	}
	if _, ok := ResolveTier(Tier4K, parsing.DomainSeries, bindings); ok {
		t.Error("4K/series should miss")
	}
}

// --- DefaultProfiles ---------------------------------------------------------

func TestDefaultProfiles(t *testing.T) {
	reg := rules.NewRegistry()
	profiles := DefaultProfiles()
	if len(profiles) != 4 {
		t.Fatalf("got %d profiles, want 4", len(profiles))
	}
	for _, p := range profiles {
		if len(p.Bins) == 0 {
			t.Errorf("%s/%s has no bins", p.Name, p.Domain)
		}
		if p.rank(p.Cutoff) >= len(p.Bins) {
			t.Errorf("%s/%s cutoff %+v not in bins", p.Name, p.Domain, p.Cutoff)
		}
	}

	// Smoke: the HD movie preset grabs a Bluray-1080p release.
	hdMovie := profiles[0]
	sel := hdMovie.Pick(reg, []model.Subject{subj("BluRay", "1080p", "", 10, 0, "x")})
	if sel.Picked == nil {
		t.Error("HD movie preset failed to pick a Bluray-1080p release")
	}
}

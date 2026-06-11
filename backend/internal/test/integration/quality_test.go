//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/qualityprofile"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/rules"
	"github.com/kyleaupton/arrflix/internal/service"
	"github.com/kyleaupton/arrflix/internal/test/dbtest"
)

// mib is the byte-per-minute unit the size seed stores (MiB), matching
// migration 0015's ×1048576 conversion of upstream MB/min values.
const mib = int64(1048576)

func bk(s parsing.Source, r parsing.Resolution, m parsing.Modifier) parsing.BinKey {
	return parsing.BinKey{Source: s, Resolution: r, Modifier: m}
}

// repackTree is the canonical quality.is_repack == true scorer, the seeded
// search-phase custom format.
func repackTree(t *testing.T) json.RawMessage {
	t.Helper()
	tree := rules.Condition{
		Op:    rules.OpEq,
		Left:  &rules.Operand{Kind: rules.OperandField, Path: "quality.is_repack"},
		Right: &rules.Operand{Kind: rules.OperandLiteral, Literal: &rules.Literal{Type: rules.LitBool, Bool: true}},
	}
	b, err := json.Marshal(&tree)
	if err != nil {
		t.Fatalf("marshal repack tree: %v", err)
	}
	return b
}

// qpSubject builds a movie subject from a release title, carrying the candidate
// facts the gate reads plus the matched runtime size bands scale against.
func qpSubject(title string, size int64, seeders, runtime int) model.Subject {
	cand := model.DownloadCandidate{Title: title, Size: size, Seeders: seeders, GUID: title}
	s := model.NewSubject(cand, parsing.Parse(title, parsing.DomainMovie))
	rt := runtime
	return s.WithMedia(model.MediaTypeMovie, "Movie", 2020, 123, &rt)
}

// TestQualityProfile_ResolveRoundTrip creates a custom format and a quality
// profile with bins/cutoff/an assigned format weight, then resolves it through
// the service and asserts the in-memory engine profile matches — bins ordered,
// cutoff carried, format tree+weight joined, and the global size defaults
// merged from the migration seed. A Pick over two synthetic releases is the
// end-to-end smoke.
func TestQualityProfile_ResolveRoundTrip(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	svc := service.NewQualityProfileService(r)
	ctx := context.Background()

	bluray1080 := bk(parsing.SourceBluRay, parsing.Res1080p, parsing.ModNone)
	webdl1080 := bk(parsing.SourceWEBDL, parsing.Res1080p, parsing.ModNone)

	format, err := r.CreateCustomFormat(ctx, repo.CreateCustomFormatParams{
		Name:       "Repack/Proper",
		Domain:     "movie",
		Conditions: repackTree(t),
	})
	if err != nil {
		t.Fatalf("create custom format: %v", err)
	}

	profile, err := r.CreateQualityProfile(ctx, repo.CreateQualityProfileParams{
		Name:       "HD",
		Domain:     "movie",
		Bins:       []parsing.BinKey{bluray1080, webdl1080},
		Cutoff:     webdl1080,
		MinSeeders: 5,
	})
	if err != nil {
		t.Fatalf("create quality profile: %v", err)
	}

	if err := r.SetProfileFormats(ctx, profile.ID, []model.ProfileFormat{
		{CustomFormatID: format.ID, Weight: 7},
	}); err != nil {
		t.Fatalf("assign format: %v", err)
	}

	resolved, err := svc.Resolve(ctx, profile.ID)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// Bins are carried in rank order; cutoff round-trips.
	if len(resolved.Bins) != 2 || resolved.Bins[0] != bluray1080 || resolved.Bins[1] != webdl1080 {
		t.Errorf("Bins = %+v, want [bluray1080, webdl1080] in order", resolved.Bins)
	}
	if resolved.Cutoff != webdl1080 {
		t.Errorf("Cutoff = %+v, want webdl1080", resolved.Cutoff)
	}
	if resolved.MinSeeders != 5 {
		t.Errorf("MinSeeders = %d, want 5", resolved.MinSeeders)
	}

	// The assigned custom format resolves to a scorer with its tree + weight.
	if len(resolved.Formats) != 1 {
		t.Fatalf("Formats = %d, want 1", len(resolved.Formats))
	}
	f := resolved.Formats[0]
	if f.Name != "Repack/Proper" || f.Weight != 7 {
		t.Errorf("format = %+v, want Repack/Proper weight 7", f)
	}
	if f.Tree == nil || f.Tree.Op != rules.OpEq || f.Tree.Left.Path != "quality.is_repack" {
		t.Errorf("format tree = %+v, want quality.is_repack ==", f.Tree)
	}

	// Global size defaults merged: Bluray-1080p is min 4, preferred 95, max 155
	// MB/min (seed 0015, ported from Sonarr Quality.cs).
	band, ok := resolved.Sizes[bluray1080]
	if !ok {
		t.Fatalf("Sizes missing the bluray1080 band; merged = %+v", resolved.Sizes)
	}
	if band.MinBytesPerMin != 4*mib || band.PreferredBytesPerMin != 95*mib || band.MaxBytesPerMin != 155*mib {
		t.Errorf("bluray1080 band = %+v, want min=4MiB preferred=95MiB max=155MiB", band)
	}

	// End-to-end smoke: a Bluray-1080p release outranks a WEBDL-1080p one. Both
	// sit inside their size bands at a 100-minute runtime (~8GB each).
	reg := rules.NewRegistry()
	size := int64(8_000_000_000)
	sel := resolved.Pick(reg, []model.Subject{
		qpSubject("Movie 2020 1080p WEB-DL x264-GRP", size, 10, 100),
		qpSubject("Movie 2020 1080p BluRay x264-GRP", size, 10, 100),
	})
	if sel.Picked == nil {
		t.Fatal("Pick returned no winner")
	}
	if got := sel.Picked.Bin; got != bluray1080 {
		t.Errorf("picked bin = %+v, want bluray1080 (bin rank wins)", got)
	}
}

// seededMovieHDID returns the preset HD-movie profile's fixed ID from the engine
// presets, so tests reference the seeded profile without re-deriving the UUID.
func seededMovieHDID(t *testing.T) uuid.UUID {
	t.Helper()
	for _, p := range qualityprofile.DefaultProfiles() {
		if p.Domain == parsing.DomainMovie && p.Name == "HD" {
			return p.ID
		}
	}
	t.Fatal("no HD movie preset in DefaultProfiles")
	return uuid.Nil
}

// TestQualityProfile_SeedDefaults asserts seeding installs the four presets,
// their two repack custom formats (one per domain), and the four tier bindings;
// that a re-run is a no-op; and that an edit to a seeded preset survives a
// re-seed (non-clobbering).
func TestQualityProfile_SeedDefaults(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	svc := service.NewQualityProfileService(r)
	ctx := context.Background()

	if err := svc.SeedDefaults(ctx); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}

	profiles, err := svc.ListProfiles(ctx)
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}
	if len(profiles) != 4 {
		t.Errorf("seeded profiles = %d, want 4", len(profiles))
	}

	formats, err := svc.ListCustomFormats(ctx)
	if err != nil {
		t.Fatalf("list custom formats: %v", err)
	}
	if len(formats) != 2 {
		t.Errorf("seeded custom formats = %d, want 2 (repack per domain)", len(formats))
	}

	bindings, err := svc.ListTierBindings(ctx)
	if err != nil {
		t.Fatalf("list tier bindings: %v", err)
	}
	if len(bindings) != 4 {
		t.Errorf("seeded tier bindings = %d, want 4", len(bindings))
	}

	// Each preset carries its repack format assignment.
	detail, err := svc.GetProfile(ctx, seededMovieHDID(t))
	if err != nil {
		t.Fatalf("get seeded HD movie profile: %v", err)
	}
	if len(detail.Formats) != 1 {
		t.Errorf("seeded HD movie formats = %d, want 1", len(detail.Formats))
	}

	// Re-running seeding is a no-op: counts are stable.
	if err := svc.SeedDefaults(ctx); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	profiles2, err := svc.ListProfiles(ctx)
	if err != nil {
		t.Fatalf("list profiles after re-seed: %v", err)
	}
	if len(profiles2) != 4 {
		t.Errorf("profiles after re-seed = %d, want 4 (idempotent)", len(profiles2))
	}

	// Non-clobbering: an edit to a seeded preset survives a re-seed.
	if _, err := svc.UpdateProfile(ctx, seededMovieHDID(t), service.QualityProfileInput{
		Name:       "HD (edited)",
		Domain:     "movie",
		Bins:       detail.Bins,
		Cutoff:     detail.Cutoff,
		MinSeeders: detail.MinSeeders,
	}); err != nil {
		t.Fatalf("edit seeded profile: %v", err)
	}
	if err := svc.SeedDefaults(ctx); err != nil {
		t.Fatalf("re-seed after edit: %v", err)
	}
	edited, err := svc.GetProfile(ctx, seededMovieHDID(t))
	if err != nil {
		t.Fatalf("get edited profile: %v", err)
	}
	if edited.Name != "HD (edited)" {
		t.Errorf("edited preset name = %q, want %q to survive re-seed", edited.Name, "HD (edited)")
	}
}

// TestQualityProfile_CRUD round-trips a custom format and a profile through the
// service's mutating methods: create, update (replacing the format set), and
// delete.
func TestQualityProfile_CRUD(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	svc := service.NewQualityProfileService(r)
	ctx := context.Background()

	bluray1080 := bk(parsing.SourceBluRay, parsing.Res1080p, parsing.ModNone)
	webdl1080 := bk(parsing.SourceWEBDL, parsing.Res1080p, parsing.ModNone)

	cf, err := svc.CreateCustomFormat(ctx, service.CustomFormatInput{
		Name:       "Repack/Proper",
		Domain:     "movie",
		Conditions: repackTree(t),
	})
	if err != nil {
		t.Fatalf("create custom format: %v", err)
	}

	created, err := svc.CreateProfile(ctx, service.QualityProfileInput{
		Name:       "HD",
		Domain:     "movie",
		Bins:       []parsing.BinKey{bluray1080, webdl1080},
		Cutoff:     bluray1080,
		MinSeeders: 5,
		Formats:    []model.ProfileFormat{{CustomFormatID: cf.ID, Weight: 7}},
	})
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	if len(created.Formats) != 1 || created.Formats[0].Weight != 7 {
		t.Errorf("created formats = %+v, want one weight-7 assignment", created.Formats)
	}

	// Update replaces the format set: drop the assignment.
	updated, err := svc.UpdateProfile(ctx, created.ID, service.QualityProfileInput{
		Name:       "HD renamed",
		Domain:     "movie",
		Bins:       []parsing.BinKey{bluray1080, webdl1080},
		Cutoff:     webdl1080,
		MinSeeders: 0,
	})
	if err != nil {
		t.Fatalf("update profile: %v", err)
	}
	if updated.Name != "HD renamed" || updated.Cutoff != webdl1080 {
		t.Errorf("updated profile = %+v, want name 'HD renamed' cutoff webdl1080", updated.QualityProfile)
	}
	if len(updated.Formats) != 0 {
		t.Errorf("updated formats = %+v, want empty after replace", updated.Formats)
	}

	// Update the custom format, then delete both.
	if _, err := svc.UpdateCustomFormat(ctx, cf.ID, service.CustomFormatInput{
		Name:       "Repack only",
		Domain:     "movie",
		Conditions: repackTree(t),
	}); err != nil {
		t.Fatalf("update custom format: %v", err)
	}

	if err := svc.DeleteProfile(ctx, created.ID); err != nil {
		t.Fatalf("delete profile: %v", err)
	}
	if _, err := svc.GetProfile(ctx, created.ID); !apperrors.IsNotFound(err) {
		t.Errorf("get after delete = %v, want NotFound", err)
	}
	if err := svc.DeleteCustomFormat(ctx, cf.ID); err != nil {
		t.Fatalf("delete custom format: %v", err)
	}
}

// TestQualityProfile_DomainMismatch asserts a profile rejecting a format whose
// domain differs from the profile's — a write-time 422.
func TestQualityProfile_DomainMismatch(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	svc := service.NewQualityProfileService(r)
	ctx := context.Background()

	bluray1080 := bk(parsing.SourceBluRay, parsing.Res1080p, parsing.ModNone)

	seriesFormat, err := svc.CreateCustomFormat(ctx, service.CustomFormatInput{
		Name:       "Repack/Proper",
		Domain:     "series",
		Conditions: repackTree(t),
	})
	if err != nil {
		t.Fatalf("create series custom format: %v", err)
	}

	_, err = svc.CreateProfile(ctx, service.QualityProfileInput{
		Name:    "HD",
		Domain:  "movie",
		Bins:    []parsing.BinKey{bluray1080},
		Cutoff:  bluray1080,
		Formats: []model.ProfileFormat{{CustomFormatID: seriesFormat.ID, Weight: 1}},
	})
	if !apperrors.IsValidation(err) {
		t.Fatalf("create with cross-domain format = %v, want a validation error", err)
	}
}

// TestQualityProfile_Test seeds the presets and exercises the live engine via
// the test endpoint's service method: a BluRay-1080p movie release passes the
// HD movie preset and lands in the Bluray-1080p bin.
func TestQualityProfile_Test(t *testing.T) {
	t.Parallel()
	pool := dbtest.New(t)
	r := repo.New(pool)
	svc := service.NewQualityProfileService(r)
	ctx := context.Background()

	if err := svc.SeedDefaults(ctx); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}

	eval, err := svc.Test(ctx, seededMovieHDID(t), "Movie 2020 1080p BluRay x264-GRP")
	if err != nil {
		t.Fatalf("test profile: %v", err)
	}
	if !eval.Passed {
		t.Errorf("evaluation passed = false, want true (reason: %+v)", eval.RejectReason)
	}
	if want := bk(parsing.SourceBluRay, parsing.Res1080p, parsing.ModNone); eval.Bin != want {
		t.Errorf("evaluation bin = %+v, want bluray1080", eval.Bin)
	}
}

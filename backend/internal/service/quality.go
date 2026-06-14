package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/qualityprofile"
	"github.com/kyleaupton/arrflix/internal/repo"
	"github.com/kyleaupton/arrflix/internal/rules"
)

// QualityProfileService orchestrates the quality-profile domain: profile and
// custom-format CRUD with write-time tree validation against the field
// registry, tier bindings, preset seeding, and the test-against-a-title engine
// exercise. It also assembles a persisted profile into the in-memory
// qualityprofile.Profile the engine consumes. Decoding the stored condition
// trees is a service concern (model cannot name *rules.Condition), exactly like
// RoutingService.
type QualityProfileService struct {
	repo *repo.Repository
	reg  *rules.Registry
}

func NewQualityProfileService(r *repo.Repository) *QualityProfileService {
	return &QualityProfileService{repo: r, reg: rules.NewRegistry()}
}

// gateJSON is the persisted shape of one gate: a name plus the canonical
// rules.Condition tree, carried raw until the registry decodes it.
type gateJSON struct {
	Name string          `json:"name"`
	Tree json.RawMessage `json:"tree"`
}

// Resolve assembles a persisted profile into the in-memory engine profile:
// decoding gate and custom-format trees, joining assigned formats with their
// weights, and merging global size defaults with the profile's overrides.
func (s *QualityProfileService) Resolve(ctx context.Context, id uuid.UUID) (qualityprofile.Profile, error) {
	const op = "QualityProfileService.Resolve"

	profile, err := s.repo.GetQualityProfile(ctx, id)
	if err != nil {
		return qualityprofile.Profile{}, err
	}

	gates, err := s.decodeGates(profile.ID, profile.Gates, op)
	if err != nil {
		return qualityprofile.Profile{}, err
	}

	formats, err := s.resolveFormats(ctx, id, op)
	if err != nil {
		return qualityprofile.Profile{}, err
	}

	sizes, err := s.mergeSizes(ctx, profile, op)
	if err != nil {
		return qualityprofile.Profile{}, err
	}

	return qualityprofile.Profile{
		ID:         profile.ID,
		Name:       profile.Name,
		Domain:     parsing.Domain(profile.Domain),
		Bins:       profile.Bins,
		Cutoff:     profile.Cutoff,
		Sizes:      sizes,
		MinSeeders: profile.MinSeeders,
		Indexers:   profile.Indexers,
		Gates:      gates,
		Formats:    formats,
	}, nil
}

// ResolveByTier resolves the profile bound to a (tier, domain) pair. A missing
// binding is a NotFound — the requester picked a tier with no profile wired for
// that media type.
func (s *QualityProfileService) ResolveByTier(ctx context.Context, tier qualityprofile.Tier, domain parsing.Domain) (qualityprofile.Profile, error) {
	const op = "QualityProfileService.ResolveByTier"

	rows, err := s.repo.ListTierBindings(ctx)
	if err != nil {
		return qualityprofile.Profile{}, err
	}
	bindings := make([]qualityprofile.TierBinding, 0, len(rows))
	for _, b := range rows {
		bindings = append(bindings, qualityprofile.TierBinding{
			Tier:      qualityprofile.Tier(b.Tier),
			Domain:    parsing.Domain(b.Domain),
			ProfileID: b.ProfileID,
		})
	}

	id, ok := qualityprofile.ResolveTier(tier, domain, bindings)
	if !ok {
		return qualityprofile.Profile{}, apperrors.NotFoundf("no quality profile bound to tier %s/%s", tier, domain).Op(op)
	}
	return s.Resolve(ctx, id)
}

// decodeGates turns the persisted [{name, tree}] array into engine gates,
// decoding each tree to a *rules.Condition. Undecodable stored JSON is corrupt
// data — non-retryable Internal, like RoutingService.Dispatch.
func (s *QualityProfileService) decodeGates(profileID uuid.UUID, raw json.RawMessage, op string) ([]qualityprofile.Gate, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var dtos []gateJSON
	if err := json.Unmarshal(raw, &dtos); err != nil {
		return nil, apperrors.Internalf("quality profile %s has undecodable gates: %v", profileID, err).Op(op).NotRetryable()
	}
	gates := make([]qualityprofile.Gate, 0, len(dtos))
	for _, d := range dtos {
		var cond rules.Condition
		if err := json.Unmarshal(d.Tree, &cond); err != nil {
			return nil, apperrors.Internalf("quality profile %s gate %q has an undecodable tree: %v", profileID, d.Name, err).Op(op).NotRetryable()
		}
		gates = append(gates, qualityprofile.Gate{Name: d.Name, Tree: &cond})
	}
	return gates, nil
}

// resolveFormats joins a profile's assigned custom formats with their weights,
// decoding each format's condition tree to a scorer.
func (s *QualityProfileService) resolveFormats(ctx context.Context, profileID uuid.UUID, op string) ([]qualityprofile.ScoredFormat, error) {
	assignments, err := s.repo.ListProfileFormats(ctx, profileID)
	if err != nil {
		return nil, err
	}
	formats := make([]qualityprofile.ScoredFormat, 0, len(assignments))
	for _, a := range assignments {
		cf, err := s.repo.GetCustomFormat(ctx, a.CustomFormatID)
		if err != nil {
			return nil, err
		}
		var cond rules.Condition
		if err := json.Unmarshal(cf.Conditions, &cond); err != nil {
			return nil, apperrors.Internalf("custom format %s has an undecodable tree: %v", cf.ID, err).Op(op).NotRetryable()
		}
		formats = append(formats, qualityprofile.ScoredFormat{Name: cf.Name, Tree: &cond, Weight: a.Weight})
	}
	return formats, nil
}

// mergeSizes builds the per-bin size bands: the global per-domain defaults with
// the profile's overrides layered on top (an override replaces the band for its
// bin). nil when no bin has a band.
func (s *QualityProfileService) mergeSizes(ctx context.Context, profile model.QualityProfile, op string) (map[parsing.BinKey]qualityprofile.SizeBand, error) {
	defaults, err := s.repo.ListSizeDefaultsByDomain(ctx, profile.Domain)
	if err != nil {
		return nil, err
	}
	if len(defaults) == 0 && len(profile.SizeOverrides) == 0 {
		return nil, nil
	}
	sizes := make(map[parsing.BinKey]qualityprofile.SizeBand, len(defaults)+len(profile.SizeOverrides))
	for _, d := range defaults {
		sizes[d.Bin] = qualityprofile.SizeBand{
			MinBytesPerMin:       d.Min,
			PreferredBytesPerMin: d.Preferred,
			MaxBytesPerMin:       d.Max,
		}
	}
	for _, o := range profile.SizeOverrides {
		sizes[o.Bin] = qualityprofile.SizeBand{
			MinBytesPerMin:       o.Min,
			PreferredBytesPerMin: o.Preferred,
			MaxBytesPerMin:       o.Max,
		}
	}
	return sizes, nil
}

// --- Mutating CRUD -----------------------------------------------------------

// QualityProfileInput is the writeable shape for profile create/update. Gates
// rides as the canonical [{name, tree}] JSON; each tree is decoded, type-checked
// against the registry, and re-encoded canonically before storage. Formats are
// the custom-format assignments persisted via SetProfileFormats.
type QualityProfileInput struct {
	Name          string
	Domain        string
	Bins          []parsing.BinKey
	Cutoff        parsing.BinKey
	MinSeeders    int
	Indexers      []uuid.UUID
	Gates         json.RawMessage
	SizeOverrides []model.QualityBinSize
	Formats       []model.ProfileFormat
}

// CustomFormatInput is the writeable shape for custom-format create/update.
// Conditions is the canonical rules.Condition tree, validated and re-encoded on
// write exactly like a routing rule's conditions.
type CustomFormatInput struct {
	Name       string
	Domain     string
	Conditions json.RawMessage
}

// QualityProfileDetail is a profile plus its custom-format assignments — the
// composite the admin API returns so the editor sees the scored formats with
// the profile in one fetch.
type QualityProfileDetail struct {
	model.QualityProfile
	Formats []model.ProfileFormat `json:"formats"`
}

func (s *QualityProfileService) ListProfiles(ctx context.Context) ([]model.QualityProfile, error) {
	return s.repo.ListQualityProfiles(ctx)
}

func (s *QualityProfileService) GetProfile(ctx context.Context, id uuid.UUID) (QualityProfileDetail, error) {
	profile, err := s.repo.GetQualityProfile(ctx, id)
	if err != nil {
		return QualityProfileDetail{}, err
	}
	formats, err := s.repo.ListProfileFormats(ctx, id)
	if err != nil {
		return QualityProfileDetail{}, err
	}
	return QualityProfileDetail{QualityProfile: profile, Formats: formats}, nil
}

func (s *QualityProfileService) CreateProfile(ctx context.Context, in QualityProfileInput) (QualityProfileDetail, error) {
	const op = "QualityProfileService.CreateProfile"
	gates, err := s.validateProfileInput(ctx, in, op)
	if err != nil {
		return QualityProfileDetail{}, err
	}
	profile, err := s.repo.CreateQualityProfile(ctx, repo.CreateQualityProfileParams{
		Name:          in.Name,
		Domain:        in.Domain,
		Bins:          in.Bins,
		Cutoff:        in.Cutoff,
		MinSeeders:    in.MinSeeders,
		Indexers:      in.Indexers,
		Gates:         gates,
		SizeOverrides: in.SizeOverrides,
	})
	if err != nil {
		return QualityProfileDetail{}, err
	}
	if err := s.repo.SetProfileFormats(ctx, profile.ID, in.Formats); err != nil {
		return QualityProfileDetail{}, err
	}
	return s.GetProfile(ctx, profile.ID)
}

func (s *QualityProfileService) UpdateProfile(ctx context.Context, id uuid.UUID, in QualityProfileInput) (QualityProfileDetail, error) {
	const op = "QualityProfileService.UpdateProfile"
	gates, err := s.validateProfileInput(ctx, in, op)
	if err != nil {
		return QualityProfileDetail{}, err
	}
	profile, err := s.repo.UpdateQualityProfile(ctx, repo.UpdateQualityProfileParams{
		ID:            id,
		Name:          in.Name,
		Domain:        in.Domain,
		Bins:          in.Bins,
		Cutoff:        in.Cutoff,
		MinSeeders:    in.MinSeeders,
		Indexers:      in.Indexers,
		Gates:         gates,
		SizeOverrides: in.SizeOverrides,
	})
	if err != nil {
		return QualityProfileDetail{}, err
	}
	// SetProfileFormats replaces the whole assignment set, so an update with a
	// trimmed Formats list drops the removed assignments.
	if err := s.repo.SetProfileFormats(ctx, id, in.Formats); err != nil {
		return QualityProfileDetail{}, err
	}
	return s.GetProfile(ctx, profile.ID)
}

func (s *QualityProfileService) DeleteProfile(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteQualityProfile(ctx, id)
}

func (s *QualityProfileService) ListCustomFormats(ctx context.Context) ([]model.CustomFormat, error) {
	return s.repo.ListCustomFormats(ctx)
}

func (s *QualityProfileService) GetCustomFormat(ctx context.Context, id uuid.UUID) (model.CustomFormat, error) {
	return s.repo.GetCustomFormat(ctx, id)
}

func (s *QualityProfileService) CreateCustomFormat(ctx context.Context, in CustomFormatInput) (model.CustomFormat, error) {
	const op = "QualityProfileService.CreateCustomFormat"
	conditions, err := s.validateCustomFormatInput(in, op)
	if err != nil {
		return model.CustomFormat{}, err
	}
	return s.repo.CreateCustomFormat(ctx, repo.CreateCustomFormatParams{
		Name:       in.Name,
		Domain:     in.Domain,
		Conditions: conditions,
	})
}

func (s *QualityProfileService) UpdateCustomFormat(ctx context.Context, id uuid.UUID, in CustomFormatInput) (model.CustomFormat, error) {
	const op = "QualityProfileService.UpdateCustomFormat"
	conditions, err := s.validateCustomFormatInput(in, op)
	if err != nil {
		return model.CustomFormat{}, err
	}
	return s.repo.UpdateCustomFormat(ctx, repo.UpdateCustomFormatParams{
		ID:         id,
		Name:       in.Name,
		Domain:     in.Domain,
		Conditions: conditions,
	})
}

func (s *QualityProfileService) DeleteCustomFormat(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteCustomFormat(ctx, id)
}

func (s *QualityProfileService) ListTierBindings(ctx context.Context) ([]model.TierBinding, error) {
	return s.repo.ListTierBindings(ctx)
}

// BindTier binds a (tier, domain) selection to a concrete profile, replacing any
// existing binding for that pair.
func (s *QualityProfileService) BindTier(ctx context.Context, tier, domain string, profileID uuid.UUID) (model.TierBinding, error) {
	return s.repo.UpsertTierBinding(ctx, tier, domain, profileID)
}

func (s *QualityProfileService) ListSizeDefaults(ctx context.Context, domain string) ([]model.QualitySizeDefault, error) {
	return s.repo.ListSizeDefaultsByDomain(ctx, domain)
}

// --- Write-time validation ---------------------------------------------------

// validateProfileInput runs every authoring-time check and returns the canonical
// gates bytes to store. All field problems are collected so the editor can flag
// everything at once.
func (s *QualityProfileService) validateProfileInput(ctx context.Context, in QualityProfileInput, op string) (json.RawMessage, error) {
	var fields []apperrors.FieldError
	if in.Name == "" {
		fields = append(fields, apperrors.Field("body.name", "required"))
	}
	if in.Domain != "movie" && in.Domain != "series" {
		fields = append(fields, apperrors.Field("body.domain", "must be 'movie' or 'series'"))
	}

	// Bins non-empty; the cutoff must be one of the bins (the engine ranks the
	// cutoff against Bins, so a cutoff outside the list is meaningless).
	if len(in.Bins) == 0 {
		fields = append(fields, apperrors.Field("body.bins", "at least one bin is required"))
	} else {
		inBins := false
		for _, b := range in.Bins {
			if b == in.Cutoff {
				inBins = true
				break
			}
		}
		if !inBins {
			fields = append(fields, apperrors.Field("body.cutoff", "must be one of the profile's bins"))
		}
	}

	// Gates: decode the [{name, tree}] array, type-check each tree, re-encode
	// canonical.
	canonicalGates, gateFields := s.validateGates(in.Gates)
	fields = append(fields, gateFields...)

	// Each assigned custom format must exist and match the profile's domain.
	for i, f := range in.Formats {
		cf, err := s.repo.GetCustomFormat(ctx, f.CustomFormatID)
		if apperrors.IsNotFound(err) {
			fields = append(fields, apperrors.Field(fmt.Sprintf("body.formats[%d].customFormatId", i), "custom format not found"))
			continue
		} else if err != nil {
			return nil, err
		}
		if cf.Domain != in.Domain {
			fields = append(fields, apperrors.Field(fmt.Sprintf("body.formats[%d].customFormatId", i),
				fmt.Sprintf("custom format domain %q does not match profile domain %q", cf.Domain, in.Domain)))
		}
	}

	if len(fields) > 0 {
		return nil, apperrors.Validation("invalid quality profile", fields...).Op(op)
	}
	return canonicalGates, nil
}

// validateGates decodes the [{name, tree}] array, type-checks each tree against
// the registry, and returns the canonical re-encoding. nil/empty input yields a
// nil canonical (the repo normalizes that to []). All problems are collected
// into field errors keyed by gate index.
func (s *QualityProfileService) validateGates(raw json.RawMessage) (json.RawMessage, []apperrors.FieldError) {
	if len(raw) == 0 {
		return nil, nil
	}
	var dtos []gateJSON
	if err := json.Unmarshal(raw, &dtos); err != nil {
		return nil, []apperrors.FieldError{apperrors.Field("body.gates", "not a valid gate list")}
	}
	var fields []apperrors.FieldError
	canonical := make([]gateJSON, 0, len(dtos))
	for i, d := range dtos {
		if d.Name == "" {
			fields = append(fields, apperrors.Field(fmt.Sprintf("body.gates[%d].name", i), "required"))
		}
		treeLoc := fmt.Sprintf("body.gates[%d].tree", i)
		var cond rules.Condition
		if err := json.Unmarshal(d.Tree, &cond); err != nil {
			fields = append(fields, apperrors.Field(treeLoc, "not a valid condition tree"))
			continue
		}
		if err := s.reg.Validate(&cond); err != nil {
			var verr *rules.ValidationError
			if errors.As(err, &verr) {
				for _, p := range verr.Problems {
					loc := treeLoc
					if p.Loc != "" {
						loc += "." + p.Loc
					}
					fields = append(fields, apperrors.Field(loc, p.Msg))
				}
			} else {
				fields = append(fields, apperrors.Field(treeLoc, err.Error()))
			}
			continue
		}
		tree, _ := json.Marshal(&cond)
		canonical = append(canonical, gateJSON{Name: d.Name, Tree: tree})
	}
	if len(fields) > 0 {
		return nil, fields
	}
	out, _ := json.Marshal(canonical)
	return out, nil
}

// validateCustomFormatInput checks name/domain and type-checks the condition
// tree against the registry, returning the canonical re-encoding. Identical in
// spirit to RoutingService's condition handling.
func (s *QualityProfileService) validateCustomFormatInput(in CustomFormatInput, op string) (json.RawMessage, error) {
	var fields []apperrors.FieldError
	if in.Name == "" {
		fields = append(fields, apperrors.Field("body.name", "required"))
	}
	if in.Domain != "movie" && in.Domain != "series" {
		fields = append(fields, apperrors.Field("body.domain", "must be 'movie' or 'series'"))
	}

	var canonical json.RawMessage
	if len(in.Conditions) == 0 {
		fields = append(fields, apperrors.Field("body.conditions", "required"))
	} else {
		var cond rules.Condition
		if err := json.Unmarshal(in.Conditions, &cond); err != nil {
			fields = append(fields, apperrors.Field("body.conditions", "not a valid condition tree"))
		} else if err := s.reg.Validate(&cond); err != nil {
			var verr *rules.ValidationError
			if errors.As(err, &verr) {
				for _, p := range verr.Problems {
					loc := "body.conditions"
					if p.Loc != "" {
						loc += "." + p.Loc
					}
					fields = append(fields, apperrors.Field(loc, p.Msg))
				}
			} else {
				fields = append(fields, apperrors.Field("body.conditions", err.Error()))
			}
		} else {
			canonical, _ = json.Marshal(&cond)
		}
	}

	if len(fields) > 0 {
		return nil, apperrors.Validation("invalid custom format", fields...).Op(op)
	}
	return canonical, nil
}

// --- Preset seeding ----------------------------------------------------------

// SeedDefaults installs the HD/4K preset profiles, their repack/proper custom
// formats, and the tier bindings on startup. It is idempotent and
// non-clobbering: a preset (or tier binding) the user has since edited is left
// untouched, so seeding writes only what is genuinely absent.
func (s *QualityProfileService) SeedDefaults(ctx context.Context) error {
	const op = "QualityProfileService.SeedDefaults"

	for _, p := range qualityprofile.DefaultProfiles() {
		if _, err := s.repo.GetQualityProfile(ctx, p.ID); err == nil {
			continue // already present — never overwrite user edits
		} else if !apperrors.IsNotFound(err) {
			return err
		}

		assignments := make([]model.ProfileFormat, 0, len(p.Formats))
		for _, f := range p.Formats {
			cfID, ok := qualityprofile.SeedCustomFormatID(p.Domain, f.Name)
			if !ok {
				return apperrors.Internalf("preset profile %s references unseeded custom format %q", p.ID, f.Name).Op(op).NotRetryable()
			}
			conditions, err := json.Marshal(f.Tree)
			if err != nil {
				return apperrors.Internalf("encode preset custom format %q: %v", f.Name, err).Op(op).NotRetryable()
			}
			if err := s.repo.SeedCustomFormat(ctx, cfID, f.Name, string(p.Domain), conditions); err != nil {
				return err
			}
			assignments = append(assignments, model.ProfileFormat{CustomFormatID: cfID, Weight: f.Weight})
		}

		if err := s.repo.SeedQualityProfile(ctx, repo.SeedQualityProfileParams{
			ID:     p.ID,
			Name:   p.Name,
			Domain: string(p.Domain),
			Bins:   p.Bins,
			Cutoff: p.Cutoff,
		}); err != nil {
			return err
		}
		if err := s.repo.SetProfileFormats(ctx, p.ID, assignments); err != nil {
			return err
		}
	}

	for _, b := range qualityprofile.DefaultTierBindings() {
		if _, err := s.repo.GetTierBinding(ctx, string(b.Tier), string(b.Domain)); err == nil {
			continue
		} else if !apperrors.IsNotFound(err) {
			return err
		}
		if _, err := s.repo.UpsertTierBinding(ctx, string(b.Tier), string(b.Domain), b.ProfileID); err != nil {
			return err
		}
	}
	return nil
}

// --- Test --------------------------------------------------------------------

// Test evaluates a single release title against a resolved profile and returns
// the engine's verdict: the bin it landed in, whether it passed the gate, its
// custom-format score, and the rules traces. The profile carries its own domain,
// so the caller supplies only the title. A gated-out release still produces an
// Evaluation (the lone candidate's), carrying its reject reason.
func (s *QualityProfileService) Test(ctx context.Context, id uuid.UUID, title string) (qualityprofile.Evaluation, error) {
	prof, err := s.Resolve(ctx, id)
	if err != nil {
		return qualityprofile.Evaluation{}, err
	}
	q := parsing.Parse(title, prof.Domain)
	subject := model.NewSubject(model.DownloadCandidate{Title: title}, q)
	sel := prof.Pick(s.reg, []model.Subject{subject})
	if sel.Picked != nil {
		return *sel.Picked, nil
	}
	return sel.All[0], nil
}

// Pick gates, scores, and ranks the given subjects against the profile, returning
// the winner (Selection.Picked, nil if all gated out) plus every release's verdict.
func (s *QualityProfileService) Pick(ctx context.Context, profileID uuid.UUID, subjects []model.Subject) (qualityprofile.Selection, error) {
	prof, err := s.Resolve(ctx, profileID)
	if err != nil {
		return qualityprofile.Selection{}, err
	}
	return prof.Pick(s.reg, subjects), nil
}

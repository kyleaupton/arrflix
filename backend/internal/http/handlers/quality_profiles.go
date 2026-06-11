package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
	"github.com/kyleaupton/arrflix/internal/qualityprofile"
	"github.com/kyleaupton/arrflix/internal/service"
)

// ----- Handler -----

type QualityProfiles struct{ svc *service.Services }

func NewQualityProfiles(s *service.Services) *QualityProfiles { return &QualityProfiles{svc: s} }

// ----- Shared body shapes -----

// qualityProfileWriteBody is the profile create/update shape. Gates rides as the
// canonical [{name, tree}] JSON — carried opaquely here (the tree type is
// recursive) and type-checked against the field registry in the service, which
// rejects ill-typed trees as 422s. Bins/Cutoff/SizeOverrides are structured bin
// identity typed straight through.
type qualityProfileWriteBody struct {
	Name          string                 `json:"name" required:"true" minLength:"1" doc:"Display name"`
	Domain        string                 `json:"domain" required:"true" enum:"movie,series" doc:"Media domain"`
	Bins          []parsing.BinKey       `json:"bins" doc:"Allowed bins ranked best-first (index 0 is best)"`
	Cutoff        parsing.BinKey         `json:"cutoff" doc:"Bin at which upgrading stops; must be one of bins"`
	MinSeeders    int                    `json:"minSeeders" doc:"Reject torrents with fewer seeders"`
	Indexers      []uuid.UUID            `json:"indexers,omitempty" doc:"Indexers a search fans out to (empty = all)"`
	Gates         json.RawMessage        `json:"gates,omitempty" doc:"Hard gates as [{name, tree}] where tree is canonical rules JSON"`
	SizeOverrides []model.QualityBinSize `json:"sizeOverrides,omitempty" doc:"Per-bin size band overrides in bytes/minute"`
	Formats       []model.ProfileFormat  `json:"formats,omitempty" doc:"Scored custom-format assignments (id + weight)"`
}

func (b qualityProfileWriteBody) toInput() service.QualityProfileInput {
	return service.QualityProfileInput{
		Name:          b.Name,
		Domain:        b.Domain,
		Bins:          b.Bins,
		Cutoff:        b.Cutoff,
		MinSeeders:    b.MinSeeders,
		Indexers:      b.Indexers,
		Gates:         b.Gates,
		SizeOverrides: b.SizeOverrides,
		Formats:       b.Formats,
	}
}

// customFormatWriteBody is the custom-format create/update shape. Conditions
// rides as the canonical rules JSON, type-checked in the service.
type customFormatWriteBody struct {
	Name       string          `json:"name" required:"true" minLength:"1" doc:"Display name"`
	Domain     string          `json:"domain" required:"true" enum:"movie,series" doc:"Media domain"`
	Conditions json.RawMessage `json:"conditions" required:"true" doc:"Condition tree (canonical rules JSON)"`
}

func (b customFormatWriteBody) toInput() service.CustomFormatInput {
	return service.CustomFormatInput{
		Name:       b.Name,
		Domain:     b.Domain,
		Conditions: b.Conditions,
	}
}

// ----- List profiles -----

type QualityListProfilesInput struct{}

type QualityListProfilesOutput struct {
	Body []model.QualityProfile
}

func (h *QualityProfiles) ListProfiles(ctx context.Context, _ *QualityListProfilesInput) (*QualityListProfilesOutput, error) {
	out, err := h.svc.QualityProfiles.ListProfiles(ctx)
	if err != nil {
		return nil, err
	}
	return &QualityListProfilesOutput{Body: out}, nil
}

// ----- Get profile -----

type QualityGetProfileInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Quality profile ID"`
}

type QualityProfileOutput struct {
	Body service.QualityProfileDetail
}

func (h *QualityProfiles) GetProfile(ctx context.Context, input *QualityGetProfileInput) (*QualityProfileOutput, error) {
	out, err := h.svc.QualityProfiles.GetProfile(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &QualityProfileOutput{Body: out}, nil
}

// ----- Create profile -----

type QualityCreateProfileInput struct {
	Body qualityProfileWriteBody
}

func (h *QualityProfiles) CreateProfile(ctx context.Context, input *QualityCreateProfileInput) (*QualityProfileOutput, error) {
	out, err := h.svc.QualityProfiles.CreateProfile(ctx, input.Body.toInput())
	if err != nil {
		return nil, err
	}
	return &QualityProfileOutput{Body: out}, nil
}

// ----- Update profile -----

type QualityUpdateProfileInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"Quality profile ID"`
	Body qualityProfileWriteBody
}

func (h *QualityProfiles) UpdateProfile(ctx context.Context, input *QualityUpdateProfileInput) (*QualityProfileOutput, error) {
	out, err := h.svc.QualityProfiles.UpdateProfile(ctx, input.ID, input.Body.toInput())
	if err != nil {
		return nil, err
	}
	return &QualityProfileOutput{Body: out}, nil
}

// ----- Delete profile -----

type QualityDeleteProfileInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Quality profile ID"`
}

type QualityDeleteProfileOutput struct{}

func (h *QualityProfiles) DeleteProfile(ctx context.Context, input *QualityDeleteProfileInput) (*QualityDeleteProfileOutput, error) {
	if err := h.svc.QualityProfiles.DeleteProfile(ctx, input.ID); err != nil {
		return nil, err
	}
	return &QualityDeleteProfileOutput{}, nil
}

// ----- List custom formats -----

type QualityListCustomFormatsInput struct{}

type QualityListCustomFormatsOutput struct {
	Body []model.CustomFormat
}

func (h *QualityProfiles) ListCustomFormats(ctx context.Context, _ *QualityListCustomFormatsInput) (*QualityListCustomFormatsOutput, error) {
	out, err := h.svc.QualityProfiles.ListCustomFormats(ctx)
	if err != nil {
		return nil, err
	}
	return &QualityListCustomFormatsOutput{Body: out}, nil
}

// ----- Get custom format -----

type QualityGetCustomFormatInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Custom format ID"`
}

type QualityCustomFormatOutput struct {
	Body model.CustomFormat
}

func (h *QualityProfiles) GetCustomFormat(ctx context.Context, input *QualityGetCustomFormatInput) (*QualityCustomFormatOutput, error) {
	out, err := h.svc.QualityProfiles.GetCustomFormat(ctx, input.ID)
	if err != nil {
		return nil, err
	}
	return &QualityCustomFormatOutput{Body: out}, nil
}

// ----- Create custom format -----

type QualityCreateCustomFormatInput struct {
	Body customFormatWriteBody
}

func (h *QualityProfiles) CreateCustomFormat(ctx context.Context, input *QualityCreateCustomFormatInput) (*QualityCustomFormatOutput, error) {
	out, err := h.svc.QualityProfiles.CreateCustomFormat(ctx, input.Body.toInput())
	if err != nil {
		return nil, err
	}
	return &QualityCustomFormatOutput{Body: out}, nil
}

// ----- Update custom format -----

type QualityUpdateCustomFormatInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"Custom format ID"`
	Body customFormatWriteBody
}

func (h *QualityProfiles) UpdateCustomFormat(ctx context.Context, input *QualityUpdateCustomFormatInput) (*QualityCustomFormatOutput, error) {
	out, err := h.svc.QualityProfiles.UpdateCustomFormat(ctx, input.ID, input.Body.toInput())
	if err != nil {
		return nil, err
	}
	return &QualityCustomFormatOutput{Body: out}, nil
}

// ----- Delete custom format -----

type QualityDeleteCustomFormatInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"Custom format ID"`
}

type QualityDeleteCustomFormatOutput struct{}

func (h *QualityProfiles) DeleteCustomFormat(ctx context.Context, input *QualityDeleteCustomFormatInput) (*QualityDeleteCustomFormatOutput, error) {
	if err := h.svc.QualityProfiles.DeleteCustomFormat(ctx, input.ID); err != nil {
		return nil, err
	}
	return &QualityDeleteCustomFormatOutput{}, nil
}

// ----- List tier bindings -----

type QualityListTiersInput struct{}

type QualityListTiersOutput struct {
	Body []model.TierBinding
}

func (h *QualityProfiles) ListTiers(ctx context.Context, _ *QualityListTiersInput) (*QualityListTiersOutput, error) {
	out, err := h.svc.QualityProfiles.ListTierBindings(ctx)
	if err != nil {
		return nil, err
	}
	return &QualityListTiersOutput{Body: out}, nil
}

// ----- Bind tier -----

type qualityBindTierBody struct {
	Tier      string    `json:"tier" required:"true" enum:"HD,4K" doc:"Coarse quality tier"`
	Domain    string    `json:"domain" required:"true" enum:"movie,series" doc:"Media domain"`
	ProfileID uuid.UUID `json:"profileId" required:"true" format:"uuid" doc:"Profile the tier resolves to"`
}

type QualityBindTierInput struct {
	Body qualityBindTierBody
}

type QualityBindTierOutput struct {
	Body model.TierBinding
}

func (h *QualityProfiles) BindTier(ctx context.Context, input *QualityBindTierInput) (*QualityBindTierOutput, error) {
	out, err := h.svc.QualityProfiles.BindTier(ctx, input.Body.Tier, input.Body.Domain, input.Body.ProfileID)
	if err != nil {
		return nil, err
	}
	return &QualityBindTierOutput{Body: out}, nil
}

// ----- List size defaults -----

type QualityListSizeDefaultsInput struct {
	Domain string `query:"domain" required:"true" enum:"movie,series" doc:"Media domain"`
}

type QualityListSizeDefaultsOutput struct {
	Body []model.QualitySizeDefault
}

func (h *QualityProfiles) ListSizeDefaults(ctx context.Context, input *QualityListSizeDefaultsInput) (*QualityListSizeDefaultsOutput, error) {
	out, err := h.svc.QualityProfiles.ListSizeDefaults(ctx, input.Domain)
	if err != nil {
		return nil, err
	}
	return &QualityListSizeDefaultsOutput{Body: out}, nil
}

// ----- List bins -----

// qualityBinOption is one entry of a domain's bin vocabulary: the identity
// (BinKey, so it compares equal to a profile's bins/cutoff entries) plus the
// rendered display name the editor labels and picks bins with.
type qualityBinOption struct {
	Bin  parsing.BinKey `json:"bin"`
	Name string         `json:"name" doc:"Rendered display name"`
}

type QualityListBinsInput struct {
	Domain string `query:"domain" required:"true" enum:"movie,series" doc:"Media domain"`
}

type QualityListBinsOutput struct {
	Body []qualityBinOption
}

func (h *QualityProfiles) ListBins(ctx context.Context, input *QualityListBinsInput) (*QualityListBinsOutput, error) {
	var domain parsing.Domain
	switch input.Domain {
	case "movie":
		domain = parsing.DomainMovie
	case "series":
		domain = parsing.DomainSeries
	}

	bins := parsing.Vocabulary(domain)
	opts := make([]qualityBinOption, 0, len(bins))
	for _, b := range bins {
		opts = append(opts, qualityBinOption{Bin: b.Key(), Name: b.Name})
	}
	return &QualityListBinsOutput{Body: opts}, nil
}

// ----- Test -----

// qualityTestBody is the test-against-a-title request. The profile knows its own
// domain, so only the release title is needed.
type qualityTestBody struct {
	Title string `json:"title" required:"true" minLength:"1" doc:"Release title to evaluate against the profile"`
}

type QualityTestInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"Quality profile ID"`
	Body qualityTestBody
}

// QualityEvaluation is a defined type over qualityprofile.Evaluation so its
// OpenAPI schema name doesn't collide with routing.Evaluation — huma derives
// schema names from the Go type name, and both engines call their verdict an
// "Evaluation". The underlying struct (and its JSON tags) is identical.
type QualityEvaluation qualityprofile.Evaluation

type QualityTestOutput struct {
	Body QualityEvaluation
}

func (h *QualityProfiles) Test(ctx context.Context, input *QualityTestInput) (*QualityTestOutput, error) {
	out, err := h.svc.QualityProfiles.Test(ctx, input.ID, input.Body.Title)
	if err != nil {
		return nil, err
	}
	return &QualityTestOutput{Body: QualityEvaluation(out)}, nil
}

// ----- Register -----

func (h *QualityProfiles) RegisterHumachi(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "quality-list-profiles",
		Method:      http.MethodGet,
		Path:        "/api/v1/quality-profiles",
		Summary:     "List quality profiles",
		Tags:        []string{"quality"},
	}, h.ListProfiles)

	huma.Register(api, huma.Operation{
		OperationID:   "quality-create-profile",
		Method:        http.MethodPost,
		Path:          "/api/v1/quality-profiles",
		Summary:       "Create quality profile",
		Tags:          []string{"quality"},
		DefaultStatus: http.StatusCreated,
		Errors:        errs(errsWrite, errsForbidden),
	}, h.CreateProfile)

	huma.Register(api, huma.Operation{
		OperationID: "quality-get-profile",
		Method:      http.MethodGet,
		Path:        "/api/v1/quality-profiles/{id}",
		Summary:     "Get quality profile",
		Tags:        []string{"quality"},
		Errors:      errsRead,
	}, h.GetProfile)

	huma.Register(api, huma.Operation{
		OperationID: "quality-update-profile",
		Method:      http.MethodPut,
		Path:        "/api/v1/quality-profiles/{id}",
		Summary:     "Update quality profile",
		Tags:        []string{"quality"},
		Errors:      errs(errsUpsert, errsForbidden),
	}, h.UpdateProfile)

	huma.Register(api, huma.Operation{
		OperationID:   "quality-delete-profile",
		Method:        http.MethodDelete,
		Path:          "/api/v1/quality-profiles/{id}",
		Summary:       "Delete quality profile",
		Tags:          []string{"quality"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errs(errsDelete, errsForbidden),
	}, h.DeleteProfile)

	huma.Register(api, huma.Operation{
		OperationID: "quality-test-profile",
		Method:      http.MethodPost,
		Path:        "/api/v1/quality-profiles/{id}/test",
		Summary:     "Test a quality profile against a release title",
		Description: "Parses the title in the profile's domain and returns the engine's verdict: bin, pass/reject, score, and rules traces.",
		Tags:        []string{"quality"},
		Errors:      errs(errsRead, errsWrite),
	}, h.Test)

	huma.Register(api, huma.Operation{
		OperationID: "quality-list-custom-formats",
		Method:      http.MethodGet,
		Path:        "/api/v1/custom-formats",
		Summary:     "List custom formats",
		Tags:        []string{"quality"},
	}, h.ListCustomFormats)

	huma.Register(api, huma.Operation{
		OperationID:   "quality-create-custom-format",
		Method:        http.MethodPost,
		Path:          "/api/v1/custom-formats",
		Summary:       "Create custom format",
		Tags:          []string{"quality"},
		DefaultStatus: http.StatusCreated,
		Errors:        errs(errsWrite, errsForbidden),
	}, h.CreateCustomFormat)

	huma.Register(api, huma.Operation{
		OperationID: "quality-get-custom-format",
		Method:      http.MethodGet,
		Path:        "/api/v1/custom-formats/{id}",
		Summary:     "Get custom format",
		Tags:        []string{"quality"},
		Errors:      errsRead,
	}, h.GetCustomFormat)

	huma.Register(api, huma.Operation{
		OperationID: "quality-update-custom-format",
		Method:      http.MethodPut,
		Path:        "/api/v1/custom-formats/{id}",
		Summary:     "Update custom format",
		Tags:        []string{"quality"},
		Errors:      errs(errsUpsert, errsForbidden),
	}, h.UpdateCustomFormat)

	huma.Register(api, huma.Operation{
		OperationID:   "quality-delete-custom-format",
		Method:        http.MethodDelete,
		Path:          "/api/v1/custom-formats/{id}",
		Summary:       "Delete custom format",
		Tags:          []string{"quality"},
		DefaultStatus: http.StatusNoContent,
		Errors:        errs(errsDelete, errsForbidden),
	}, h.DeleteCustomFormat)

	huma.Register(api, huma.Operation{
		OperationID: "quality-list-tiers",
		Method:      http.MethodGet,
		Path:        "/api/v1/quality-tiers",
		Summary:     "List tier bindings",
		Tags:        []string{"quality"},
	}, h.ListTiers)

	huma.Register(api, huma.Operation{
		OperationID: "quality-bind-tier",
		Method:      http.MethodPut,
		Path:        "/api/v1/quality-tiers",
		Summary:     "Bind a tier to a quality profile",
		Tags:        []string{"quality"},
		Errors:      errs(errsUpsert, errsForbidden),
	}, h.BindTier)

	huma.Register(api, huma.Operation{
		OperationID: "quality-list-bins",
		Method:      http.MethodGet,
		Path:        "/api/v1/quality-bins",
		Summary:     "List the bin vocabulary for a domain",
		Description: "Returns the canonical quality bins for the domain with their rendered display names — the vocabulary editors label, pick, and order profile bins against.",
		Tags:        []string{"quality"},
	}, h.ListBins)

	huma.Register(api, huma.Operation{
		OperationID: "quality-list-size-defaults",
		Method:      http.MethodGet,
		Path:        "/api/v1/quality-size-defaults",
		Summary:     "List global per-bin size defaults for a domain",
		Tags:        []string{"quality"},
	}, h.ListSizeDefaults)
}

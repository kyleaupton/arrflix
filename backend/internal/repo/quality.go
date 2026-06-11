package repo

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	dbgen "github.com/kyleaupton/arrflix/internal/db/sqlc"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/model"
	"github.com/kyleaupton/arrflix/internal/parsing"
)

// Repo-local JSON DTOs own the on-the-wire shape of the structured JSONB
// columns, so parsing.BinKey stays persistence-agnostic (no JSON tags on the
// domain type). bins/cutoff/size_overrides are loaded and stored whole; gates
// passes through as raw bytes (the service decodes the [{name, tree}] array,
// since model cannot name *rules.Condition).

type binJSON struct {
	Source     string `json:"source"`
	Resolution string `json:"resolution"`
	Modifier   string `json:"modifier"`
}

type sizeBinJSON struct {
	Bin       binJSON `json:"bin"`
	Min       int64   `json:"min"`
	Preferred int64   `json:"preferred"`
	Max       int64   `json:"max"`
}

func toBinJSON(b parsing.BinKey) binJSON {
	return binJSON{Source: string(b.Source), Resolution: string(b.Resolution), Modifier: string(b.Modifier)}
}

func (b binJSON) toModel() parsing.BinKey {
	return parsing.BinKey{
		Source:     parsing.Source(b.Source),
		Resolution: parsing.Resolution(b.Resolution),
		Modifier:   parsing.Modifier(b.Modifier),
	}
}

// encodeBins marshals a ranked bin list; an empty list emits [] (never null) so
// the JSONB column stays an array.
func encodeBins(bins []parsing.BinKey) ([]byte, error) {
	dtos := make([]binJSON, 0, len(bins))
	for _, b := range bins {
		dtos = append(dtos, toBinJSON(b))
	}
	return json.Marshal(dtos)
}

func encodeIndexers(ids []uuid.UUID) ([]byte, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return json.Marshal(out)
}

func encodeSizeOverrides(overrides []model.QualityBinSize) ([]byte, error) {
	dtos := make([]sizeBinJSON, 0, len(overrides))
	for _, o := range overrides {
		dtos = append(dtos, sizeBinJSON{Bin: toBinJSON(o.Bin), Min: o.Min, Preferred: o.Preferred, Max: o.Max})
	}
	return json.Marshal(dtos)
}

// jsonArrayOrEmpty normalizes a raw JSONB payload destined for an array column:
// nil/empty bytes become [], anything else passes through verbatim.
func jsonArrayOrEmpty(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("[]")
	}
	return raw
}

// CreateQualityProfileParams is the domain-shaped input for CreateQualityProfile.
// Mirrors the writeable subset of model.QualityProfile (server-managed ID and
// timestamps excluded). Gates is the canonical [{name, tree}] JSON the service
// produces.
type CreateQualityProfileParams struct {
	Name          string
	Domain        string
	Bins          []parsing.BinKey
	Cutoff        parsing.BinKey
	MinSeeders    int
	Indexers      []uuid.UUID
	Gates         json.RawMessage
	SizeOverrides []model.QualityBinSize
}

// UpdateQualityProfileParams mirrors CreateQualityProfileParams plus the target
// ID.
type UpdateQualityProfileParams struct {
	ID            uuid.UUID
	Name          string
	Domain        string
	Bins          []parsing.BinKey
	Cutoff        parsing.BinKey
	MinSeeders    int
	Indexers      []uuid.UUID
	Gates         json.RawMessage
	SizeOverrides []model.QualityBinSize
}

// toModelQualityProfile translates a persistence-shaped row into the domain
// type, decoding the structured JSONB columns into typed fields and passing
// gates through as raw bytes. A decode failure is corrupt stored data —
// surfaced as a non-retryable Internal error.
func toModelQualityProfile(row dbgen.QualityProfile) (model.QualityProfile, error) {
	var binDTOs []binJSON
	if err := json.Unmarshal(row.Bins, &binDTOs); err != nil {
		return model.QualityProfile{}, apperrors.Internalf("quality profile %s has undecodable bins: %v", uuidFromPgtype(row.ID), err).NotRetryable()
	}
	bins := make([]parsing.BinKey, 0, len(binDTOs))
	for _, b := range binDTOs {
		bins = append(bins, b.toModel())
	}

	var cutoffDTO binJSON
	if err := json.Unmarshal(row.Cutoff, &cutoffDTO); err != nil {
		return model.QualityProfile{}, apperrors.Internalf("quality profile %s has undecodable cutoff: %v", uuidFromPgtype(row.ID), err).NotRetryable()
	}

	var sizeDTOs []sizeBinJSON
	if err := json.Unmarshal(row.SizeOverrides, &sizeDTOs); err != nil {
		return model.QualityProfile{}, apperrors.Internalf("quality profile %s has undecodable size overrides: %v", uuidFromPgtype(row.ID), err).NotRetryable()
	}
	overrides := make([]model.QualityBinSize, 0, len(sizeDTOs))
	for _, s := range sizeDTOs {
		overrides = append(overrides, model.QualityBinSize{Bin: s.Bin.toModel(), Min: s.Min, Preferred: s.Preferred, Max: s.Max})
	}

	var indexerStrs []string
	if err := json.Unmarshal(row.Indexers, &indexerStrs); err != nil {
		return model.QualityProfile{}, apperrors.Internalf("quality profile %s has undecodable indexers: %v", uuidFromPgtype(row.ID), err).NotRetryable()
	}
	indexers := make([]uuid.UUID, 0, len(indexerStrs))
	for _, s := range indexerStrs {
		id, err := uuid.Parse(s)
		if err != nil {
			return model.QualityProfile{}, apperrors.Internalf("quality profile %s has a malformed indexer id %q: %v", uuidFromPgtype(row.ID), s, err).NotRetryable()
		}
		indexers = append(indexers, id)
	}

	return model.QualityProfile{
		ID:            uuidFromPgtype(row.ID),
		Name:          row.Name,
		Domain:        row.Domain,
		Bins:          bins,
		Cutoff:        cutoffDTO.toModel(),
		MinSeeders:    int(row.MinSeeders),
		Indexers:      indexers,
		Gates:         row.Gates,
		SizeOverrides: overrides,
		CreatedAt:     row.CreatedAt,
		UpdatedAt:     row.UpdatedAt,
	}, nil
}

// encodeProfileColumns marshals the structured columns a create/update writes,
// short-circuiting on the first encode failure.
func encodeProfileColumns(bins []parsing.BinKey, cutoff parsing.BinKey, indexers []uuid.UUID, gates json.RawMessage, overrides []model.QualityBinSize) (binBytes, cutoffBytes, indexerBytes, sizeBytes []byte, err error) {
	if binBytes, err = encodeBins(bins); err != nil {
		return
	}
	if cutoffBytes, err = json.Marshal(toBinJSON(cutoff)); err != nil {
		return
	}
	if indexerBytes, err = encodeIndexers(indexers); err != nil {
		return
	}
	if sizeBytes, err = encodeSizeOverrides(overrides); err != nil {
		return
	}
	return
}

func (r *Repository) ListQualityProfiles(ctx context.Context) ([]model.QualityProfile, error) {
	rows, err := r.Q.ListQualityProfiles(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list quality profiles")
	}
	return mapQualityProfiles(rows)
}

func (r *Repository) ListQualityProfilesByDomain(ctx context.Context, domain string) ([]model.QualityProfile, error) {
	rows, err := r.Q.ListQualityProfilesByDomain(ctx, domain)
	if err != nil {
		return nil, apperrors.FromPg(err, "list quality profiles for %s", domain)
	}
	return mapQualityProfiles(rows)
}

func mapQualityProfiles(rows []dbgen.QualityProfile) ([]model.QualityProfile, error) {
	out := make([]model.QualityProfile, 0, len(rows))
	for _, row := range rows {
		m, err := toModelQualityProfile(row)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func (r *Repository) GetQualityProfile(ctx context.Context, id uuid.UUID) (model.QualityProfile, error) {
	row, err := r.Q.GetQualityProfile(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.QualityProfile{}, apperrors.FromPg(err, "quality profile %s not found", id)
	}
	return toModelQualityProfile(row)
}

func (r *Repository) CreateQualityProfile(ctx context.Context, params CreateQualityProfileParams) (model.QualityProfile, error) {
	bins, cutoff, indexers, sizes, err := encodeProfileColumns(params.Bins, params.Cutoff, params.Indexers, params.Gates, params.SizeOverrides)
	if err != nil {
		return model.QualityProfile{}, apperrors.Internalf("encode quality profile %q: %v", params.Name, err).NotRetryable()
	}
	row, err := r.Q.CreateQualityProfile(ctx, dbgen.CreateQualityProfileParams{
		Name:          params.Name,
		Domain:        params.Domain,
		Bins:          bins,
		Cutoff:        cutoff,
		MinSeeders:    int32(params.MinSeeders),
		Indexers:      indexers,
		Gates:         jsonArrayOrEmpty(params.Gates),
		SizeOverrides: sizes,
	})
	if err != nil {
		return model.QualityProfile{}, apperrors.FromPg(err, "create quality profile %q", params.Name)
	}
	return toModelQualityProfile(row)
}

func (r *Repository) UpdateQualityProfile(ctx context.Context, params UpdateQualityProfileParams) (model.QualityProfile, error) {
	bins, cutoff, indexers, sizes, err := encodeProfileColumns(params.Bins, params.Cutoff, params.Indexers, params.Gates, params.SizeOverrides)
	if err != nil {
		return model.QualityProfile{}, apperrors.Internalf("encode quality profile %s: %v", params.ID, err).NotRetryable()
	}
	row, err := r.Q.UpdateQualityProfile(ctx, dbgen.UpdateQualityProfileParams{
		ID:            pgtypeFromUUID(params.ID),
		Name:          params.Name,
		Domain:        params.Domain,
		Bins:          bins,
		Cutoff:        cutoff,
		MinSeeders:    int32(params.MinSeeders),
		Indexers:      indexers,
		Gates:         jsonArrayOrEmpty(params.Gates),
		SizeOverrides: sizes,
	})
	if err != nil {
		return model.QualityProfile{}, apperrors.FromPg(err, "update quality profile %s", params.ID)
	}
	return toModelQualityProfile(row)
}

func (r *Repository) DeleteQualityProfile(ctx context.Context, id uuid.UUID) error {
	return apperrors.FromPg(r.Q.DeleteQualityProfile(ctx, pgtypeFromUUID(id)), "delete quality profile %s", id)
}

// SeedQualityProfileParams is the domain-shaped input for SeedQualityProfile: a
// preset profile written under its explicit fixed ID. The remaining columns
// (min_seeders, indexers, gates, size_overrides) seed empty — global size
// defaults live in quality_size_default (migration 0015).
type SeedQualityProfileParams struct {
	ID     uuid.UUID
	Name   string
	Domain string
	Bins   []parsing.BinKey
	Cutoff parsing.BinKey
}

// SeedQualityProfile inserts a preset profile under its explicit ID, doing
// nothing if a row already exists — so a user's edits to a preset survive a
// restart. Idempotent and non-clobbering.
func (r *Repository) SeedQualityProfile(ctx context.Context, params SeedQualityProfileParams) error {
	bins, err := encodeBins(params.Bins)
	if err != nil {
		return apperrors.Internalf("encode preset profile %q bins: %v", params.Name, err).NotRetryable()
	}
	cutoff, err := json.Marshal(toBinJSON(params.Cutoff))
	if err != nil {
		return apperrors.Internalf("encode preset profile %q cutoff: %v", params.Name, err).NotRetryable()
	}
	return apperrors.FromPg(r.Q.SeedQualityProfile(ctx, dbgen.SeedQualityProfileParams{
		ID:            pgtypeFromUUID(params.ID),
		Name:          params.Name,
		Domain:        params.Domain,
		Bins:          bins,
		Cutoff:        cutoff,
		MinSeeders:    0,
		Indexers:      []byte("[]"),
		Gates:         []byte("[]"),
		SizeOverrides: []byte("[]"),
	}), "seed quality profile %q", params.Name)
}

// --- Custom formats ----------------------------------------------------------

// CreateCustomFormatParams is the domain-shaped input for CreateCustomFormat.
// Conditions is the canonical rules.Condition tree (the service validates and
// re-encodes it on write in P4).
type CreateCustomFormatParams struct {
	Name       string
	Domain     string
	Conditions json.RawMessage
}

// UpdateCustomFormatParams mirrors CreateCustomFormatParams plus the target ID.
type UpdateCustomFormatParams struct {
	ID         uuid.UUID
	Name       string
	Domain     string
	Conditions json.RawMessage
}

func toModelCustomFormat(row dbgen.CustomFormat) model.CustomFormat {
	return model.CustomFormat{
		ID:         uuidFromPgtype(row.ID),
		Name:       row.Name,
		Domain:     row.Domain,
		Conditions: row.Conditions,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
	}
}

func (r *Repository) ListCustomFormats(ctx context.Context) ([]model.CustomFormat, error) {
	rows, err := r.Q.ListCustomFormats(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list custom formats")
	}
	out := make([]model.CustomFormat, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelCustomFormat(row))
	}
	return out, nil
}

func (r *Repository) ListCustomFormatsByDomain(ctx context.Context, domain string) ([]model.CustomFormat, error) {
	rows, err := r.Q.ListCustomFormatsByDomain(ctx, domain)
	if err != nil {
		return nil, apperrors.FromPg(err, "list custom formats for %s", domain)
	}
	out := make([]model.CustomFormat, 0, len(rows))
	for _, row := range rows {
		out = append(out, toModelCustomFormat(row))
	}
	return out, nil
}

func (r *Repository) GetCustomFormat(ctx context.Context, id uuid.UUID) (model.CustomFormat, error) {
	row, err := r.Q.GetCustomFormat(ctx, pgtypeFromUUID(id))
	if err != nil {
		return model.CustomFormat{}, apperrors.FromPg(err, "custom format %s not found", id)
	}
	return toModelCustomFormat(row), nil
}

func (r *Repository) CreateCustomFormat(ctx context.Context, params CreateCustomFormatParams) (model.CustomFormat, error) {
	row, err := r.Q.CreateCustomFormat(ctx, dbgen.CreateCustomFormatParams{
		Name:       params.Name,
		Domain:     params.Domain,
		Conditions: params.Conditions,
	})
	if err != nil {
		return model.CustomFormat{}, apperrors.FromPg(err, "create custom format %q", params.Name)
	}
	return toModelCustomFormat(row), nil
}

func (r *Repository) UpdateCustomFormat(ctx context.Context, params UpdateCustomFormatParams) (model.CustomFormat, error) {
	row, err := r.Q.UpdateCustomFormat(ctx, dbgen.UpdateCustomFormatParams{
		ID:         pgtypeFromUUID(params.ID),
		Name:       params.Name,
		Domain:     params.Domain,
		Conditions: params.Conditions,
	})
	if err != nil {
		return model.CustomFormat{}, apperrors.FromPg(err, "update custom format %s", params.ID)
	}
	return toModelCustomFormat(row), nil
}

func (r *Repository) DeleteCustomFormat(ctx context.Context, id uuid.UUID) error {
	return apperrors.FromPg(r.Q.DeleteCustomFormat(ctx, pgtypeFromUUID(id)), "delete custom format %s", id)
}

// SeedCustomFormat inserts a preset custom format under its explicit ID, doing
// nothing if a row already exists. Idempotent and non-clobbering.
func (r *Repository) SeedCustomFormat(ctx context.Context, id uuid.UUID, name, domain string, conditions json.RawMessage) error {
	return apperrors.FromPg(r.Q.SeedCustomFormat(ctx, dbgen.SeedCustomFormatParams{
		ID:         pgtypeFromUUID(id),
		Name:       name,
		Domain:     domain,
		Conditions: conditions,
	}), "seed custom format %q", name)
}

// --- Profile/format join -----------------------------------------------------

// ListProfileFormats returns the custom-format assignments for a profile (the
// format id + its scoring weight).
func (r *Repository) ListProfileFormats(ctx context.Context, profileID uuid.UUID) ([]model.ProfileFormat, error) {
	rows, err := r.Q.ListProfileFormats(ctx, pgtypeFromUUID(profileID))
	if err != nil {
		return nil, apperrors.FromPg(err, "list formats for quality profile %s", profileID)
	}
	out := make([]model.ProfileFormat, 0, len(rows))
	for _, row := range rows {
		out = append(out, model.ProfileFormat{CustomFormatID: uuidFromPgtype(row.CustomFormatID), Weight: int(row.Weight)})
	}
	return out, nil
}

// SetProfileFormats replaces a profile's entire format assignment set in one
// transaction (delete-all then insert), so a partial failure leaves the prior
// set intact.
func (r *Repository) SetProfileFormats(ctx context.Context, profileID uuid.UUID, formats []model.ProfileFormat) error {
	return r.InTx(ctx, func(tx *Repository) error {
		if err := tx.Q.DeleteProfileFormats(ctx, pgtypeFromUUID(profileID)); err != nil {
			return apperrors.FromPg(err, "clear formats for quality profile %s", profileID)
		}
		for _, f := range formats {
			if err := tx.Q.AddProfileFormat(ctx, dbgen.AddProfileFormatParams{
				ProfileID:      pgtypeFromUUID(profileID),
				CustomFormatID: pgtypeFromUUID(f.CustomFormatID),
				Weight:         int32(f.Weight),
			}); err != nil {
				return apperrors.FromPg(err, "assign format %s to quality profile %s", f.CustomFormatID, profileID)
			}
		}
		return nil
	})
}

func (r *Repository) DeleteProfileFormat(ctx context.Context, profileID, customFormatID uuid.UUID) error {
	return apperrors.FromPg(r.Q.DeleteProfileFormat(ctx, dbgen.DeleteProfileFormatParams{
		ProfileID:      pgtypeFromUUID(profileID),
		CustomFormatID: pgtypeFromUUID(customFormatID),
	}), "remove format %s from quality profile %s", customFormatID, profileID)
}

// --- Tier bindings -----------------------------------------------------------

func (r *Repository) ListTierBindings(ctx context.Context) ([]model.TierBinding, error) {
	rows, err := r.Q.ListTierBindings(ctx)
	if err != nil {
		return nil, apperrors.FromPg(err, "list tier bindings")
	}
	out := make([]model.TierBinding, 0, len(rows))
	for _, row := range rows {
		out = append(out, model.TierBinding{Tier: row.Tier, Domain: row.Domain, ProfileID: uuidFromPgtype(row.ProfileID)})
	}
	return out, nil
}

func (r *Repository) GetTierBinding(ctx context.Context, tier, domain string) (model.TierBinding, error) {
	row, err := r.Q.GetTierBinding(ctx, dbgen.GetTierBindingParams{Tier: tier, Domain: domain})
	if err != nil {
		return model.TierBinding{}, apperrors.FromPg(err, "tier binding %s/%s not found", tier, domain)
	}
	return model.TierBinding{Tier: row.Tier, Domain: row.Domain, ProfileID: uuidFromPgtype(row.ProfileID)}, nil
}

func (r *Repository) UpsertTierBinding(ctx context.Context, tier, domain string, profileID uuid.UUID) (model.TierBinding, error) {
	row, err := r.Q.UpsertTierBinding(ctx, dbgen.UpsertTierBindingParams{Tier: tier, Domain: domain, ProfileID: pgtypeFromUUID(profileID)})
	if err != nil {
		return model.TierBinding{}, apperrors.FromPg(err, "bind tier %s/%s", tier, domain)
	}
	return model.TierBinding{Tier: row.Tier, Domain: row.Domain, ProfileID: uuidFromPgtype(row.ProfileID)}, nil
}

// --- Size defaults -----------------------------------------------------------

// ListSizeDefaultsByDomain returns the global per-bin size defaults for a
// domain. Bins absent from the result are unbounded.
func (r *Repository) ListSizeDefaultsByDomain(ctx context.Context, domain string) ([]model.QualitySizeDefault, error) {
	rows, err := r.Q.ListSizeDefaultsByDomain(ctx, domain)
	if err != nil {
		return nil, apperrors.FromPg(err, "list size defaults for %s", domain)
	}
	out := make([]model.QualitySizeDefault, 0, len(rows))
	for _, row := range rows {
		out = append(out, model.QualitySizeDefault{
			Domain: row.Domain,
			Bin: parsing.BinKey{
				Source:     parsing.Source(row.Source),
				Resolution: parsing.Resolution(row.Resolution),
				Modifier:   parsing.Modifier(row.Modifier),
			},
			Min:       row.MinBytesPerMin,
			Preferred: row.PreferredBytesPerMin,
			Max:       row.MaxBytesPerMin,
		})
	}
	return out, nil
}

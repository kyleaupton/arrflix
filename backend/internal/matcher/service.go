package matcher

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/metadata"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// MatcherRepo is the persistence seam the matcher needs. Keeping it as
// an interface (rather than depending on `*repo.Repository` directly)
// keeps MatcherService unit-testable with a hand-rolled fake. The
// production implementation is *repo.Repository via the small adapter
// at the bottom of this file (RepoAdapter).
type MatcherRepo interface {
	// InsertMatchDecision writes a row, returns the assigned ID. The
	// auto-incremented match_decision.id is what the supersede chain
	// keys on; the in-record FileID is the (uuid) file the decision is
	// about.
	InsertMatchDecision(ctx context.Context, rec MatchOutcomeRecord) (int64, error)
	// SupersedeMatchDecision marks the prior current decision for the
	// given file as superseded by the new decision. Called as part of
	// re-match flows in phase 4; phase 1 calls it as part of every write
	// so the "latest non-superseded" invariant holds even when scan
	// re-processes the same file (e.g. during development).
	SupersedeMatchDecision(ctx context.Context, fileID uuid.UUID, supersedingID int64) error
}

// MatcherService is the public surface scan, drop-in flows, and the
// future match-decision handlers call. Phase 1: implemented end-to-end
// (the aggregator runs, the repo writes), but no resolvers are
// registered out of the box, so every call returns no_match. Phase 2
// wires the real resolvers (pathembed, nameparse) into the registry at
// construction.
type MatcherService struct {
	log      *logger.Logger
	repo     MatcherRepo
	provider metadata.MetadataProvider
	registry *Registry
	cfg      Config
	agg      *Aggregator
}

// NewMatcherService wires the matcher's collaborators. The registry is
// the catalog of identity resolvers; the provider is the metadata seam
// used for Tier-1 validation; the repo is the match_decision persistence
// boundary.
//
// TODO(phase-1): Config currently arrives wired up by service.New (the
// caller), which hardcodes DefaultConfig(). Wire the settings-table
// override (the per-installation Strict/Recommended/Relaxed preset)
// when the metadata-module settings work lands; matching spec § Threshold
// presets calls out the data shape.
func NewMatcherService(
	log *logger.Logger,
	r MatcherRepo,
	provider metadata.MetadataProvider,
	registry *Registry,
	cfg Config,
) *MatcherService {
	if registry == nil {
		registry = NewRegistry()
	}
	return &MatcherService{
		log:      log,
		repo:     r,
		provider: provider,
		registry: registry,
		cfg:      cfg,
		agg:      NewAggregator(log, provider, registry, cfg),
	}
}

// MatchBatch is the public surface scan.go (phase 3) calls with a batch
// of FileRefs. It runs the aggregator per file, writes a match_decision
// row per outcome, and returns the records in the same order as the
// input. A repo write failure surfaces; aggregator-level resolver
// errors are absorbed (see Aggregator.runTier).
//
// TODO(phase-4): want fulfillment lives downstream of a confident match.
// When the tracking module lands, this is where a confident match
// triggers want closure + Story-1 notifications (matching spec § Drop-in
// fulfills wants).
func (s *MatcherService) MatchBatch(ctx context.Context, files []FileRef) ([]MatchOutcomeRecord, error) {
	if len(files) == 0 {
		return nil, nil
	}
	out := make([]MatchOutcomeRecord, 0, len(files))
	for _, f := range files {
		rec := s.agg.Aggregate(ctx, f)
		if s.repo != nil {
			id, err := s.repo.InsertMatchDecision(ctx, rec)
			if err != nil {
				return nil, err
			}
			// Phase 1: scan loops over a fresh batch each time, so the
			// supersede chain is degenerate — there's no prior current
			// decision to displace. The call is here so phase-3
			// re-scan and phase-4 user re-match share the same shape.
			if err := s.repo.SupersedeMatchDecision(ctx, f.ID, id); err != nil {
				return nil, err
			}
		}
		out = append(out, rec)
	}
	return out, nil
}

// Registry exposes the resolver catalog for inspection (e.g. debug
// pages, the matcher inbox's "why didn't this match" explanation, and
// per-library toggle UI in v2). Read-only from the caller's perspective.
func (s *MatcherService) Registry() *Registry {
	return s.registry
}

// RepoAdapter wires a *repo.Repository up to the MatcherRepo interface
// by translating MatchOutcomeRecord into repo.InsertMatchDecisionParams.
// The translation lives here, not in the repo layer, because the matcher
// owns the in-memory shape and the repo doesn't import upward.
type RepoAdapter struct {
	Repo *repo.Repository
}

// NewRepoAdapter returns a MatcherRepo backed by the given repository.
func NewRepoAdapter(r *repo.Repository) *RepoAdapter {
	return &RepoAdapter{Repo: r}
}

// InsertMatchDecision marshals the audit slice and forwards to the
// repo, populating chosen_* from the in-memory record's optional fields.
func (a *RepoAdapter) InsertMatchDecision(ctx context.Context, rec MatchOutcomeRecord) (int64, error) {
	auditJSON, err := json.Marshal(rec.ResolversConsulted)
	if err != nil {
		return 0, apperrors.Internalf("marshal resolvers_consulted: %v", err).
			Op("RepoAdapter.InsertMatchDecision").
			NotRetryable()
	}

	params := repo.InsertMatchDecisionParams{
		FileID:             rec.FileID,
		Outcome:            string(rec.Outcome),
		Confidence:         rec.Confidence,
		ResolversConsulted: auditJSON,
		Evidence:           rec.Evidence,
		EvidenceTruncated:  rec.Truncated,
		DecidedBy:          rec.DecidedBy,
	}
	if rec.ChosenRef != nil {
		src := string(rec.ChosenRef.Source)
		id := rec.ChosenRef.ExternalID
		params.ChosenSource = &src
		params.ChosenExternalID = &id
	}
	if rec.ChosenEpisode != nil {
		s := int32(rec.ChosenEpisode.Season)
		e := int32(rec.ChosenEpisode.Episode)
		params.ChosenSeason = &s
		params.ChosenEpisode = &e
	}
	if rec.ChosenEdition != nil {
		ed := *rec.ChosenEdition
		params.ChosenEdition = &ed
	}

	return a.Repo.InsertMatchDecision(ctx, params)
}

// SupersedeMatchDecision is a straight forward to the repo method.
func (a *RepoAdapter) SupersedeMatchDecision(ctx context.Context, fileID uuid.UUID, supersedingID int64) error {
	return a.Repo.SupersedeMatchDecision(ctx, fileID, supersedingID)
}

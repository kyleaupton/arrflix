package service

import (
	"context"
	"encoding/json"

	apperrors "github.com/kyleaupton/arrflix/internal/errors"
	"github.com/kyleaupton/arrflix/internal/logger"
	"github.com/kyleaupton/arrflix/internal/matcher"
	"github.com/kyleaupton/arrflix/internal/metadata"
	"github.com/kyleaupton/arrflix/internal/repo"
)

// MatcherService is the public surface scan, drop-in flows, and the
// match-decision handlers call. It wraps the matcher domain module (the
// aggregator + resolver registry, both pure) and owns the match_decision
// persistence boundary. The matcher package stays repo-free; this service
// is where the domain pipeline meets the database.
type MatcherService struct {
	log      *logger.Logger
	repo     *repo.Repository
	provider metadata.MetadataProvider
	registry *matcher.Registry
	cfg      matcher.Config
	agg      *matcher.Aggregator
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
	r *repo.Repository,
	provider metadata.MetadataProvider,
	registry *matcher.Registry,
	cfg matcher.Config,
) *MatcherService {
	if registry == nil {
		registry = matcher.NewRegistry()
	}
	return &MatcherService{
		log:      log,
		repo:     r,
		provider: provider,
		registry: registry,
		cfg:      cfg,
		agg:      matcher.NewAggregator(log, provider, registry, cfg),
	}
}

// MatchBatch is the public surface scan.go calls with a batch of
// FileRefs. It runs the aggregator per file, writes a match_decision row
// per outcome, and returns the records in the same order as the input. A
// repo write failure surfaces; aggregator-level resolver errors are
// absorbed (see Aggregator.runTier).
//
// A nil repo skips persistence — the parity harness drives the aggregator
// without a database.
//
// TODO(phase-4): want fulfillment lives downstream of a confident match.
// When the tracking module lands, this is where a confident match
// triggers want closure + Story-1 notifications (matching spec § Drop-in
// fulfills wants).
func (s *MatcherService) MatchBatch(ctx context.Context, files []matcher.FileRef) ([]matcher.MatchOutcomeRecord, error) {
	if len(files) == 0 {
		return nil, nil
	}
	out := make([]matcher.MatchOutcomeRecord, 0, len(files))
	for _, f := range files {
		rec := s.agg.Aggregate(ctx, f)
		if s.repo != nil {
			id, err := insertMatchOutcome(ctx, s.repo, rec)
			if err != nil {
				return nil, err
			}
			// Supersede the prior current decision for this file so the
			// "latest non-superseded" invariant holds even when scan
			// re-processes the same file. No-op on first match.
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
func (s *MatcherService) Registry() *matcher.Registry {
	return s.registry
}

// insertMatchOutcome translates a matcher.MatchOutcomeRecord (the
// aggregator's in-memory shape) into the repo's params struct and writes
// the row. The translation lives in the service layer — the only layer
// allowed to know both the matcher's domain record and the repo's params
// — so the matcher package doesn't import upward and the repo doesn't
// depend on the matcher's enums.
//
// Shared by MatcherService (auto-match) and MatchDecisionsService
// (user-driven re-match / un-match / detach), which both build a
// MatchOutcomeRecord and persist it through the same boundary.
func insertMatchOutcome(ctx context.Context, r *repo.Repository, rec matcher.MatchOutcomeRecord) (int64, error) {
	auditJSON, err := json.Marshal(rec.ResolversConsulted)
	if err != nil {
		return 0, apperrors.Internalf("marshal resolvers_consulted: %v", err).
			Op("insertMatchOutcome").
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
		sn := int32(rec.ChosenEpisode.Season)
		en := int32(rec.ChosenEpisode.Episode)
		params.ChosenSeason = &sn
		params.ChosenEpisode = &en
	}
	if rec.ChosenEdition != nil {
		ed := *rec.ChosenEdition
		params.ChosenEdition = &ed
	}

	return r.InsertMatchDecision(ctx, params)
}
